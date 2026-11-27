package webserver

import (
	"errors"
	"fmt"
	"html"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/identity"
)

// images.go serves the Image screen re-keyed onto the toolchain-sets
// world (spec toolchain-sets §9 WP-7/#209): family rows = base + one
// component row per discovered recipe and per tagged component image
// (invalid recipe → row with reason, Build/Rebuild disabled; an orphan
// image → "recipe missing"; a user/shipped collision renders its
// warning line on the shipped row), set rows enumerated by label
// (Delete only), the shared infra rows (oc-searxng/oc-stt on-screen,
// oc-proxy off-screen as today) and the §10 Legacy group (old-world
// opencode-sandbox-* refs, delete-only, hidden when empty). Cached
// markers (the derived content-addressed tag exists) are computed fresh
// per render. Build all runs build-images.sh --all; row builds ride
// --tc <id>/--base; the infra rows build as today. Deletes are honest:
// base/components are never in-use-pinned (the danger confirm carries
// the one-rebuild cost), set/Legacy rows pin by container refs, and
// parent-having rows are refusal-first — podman rmi's exit-2 surfaces
// with the honest reason, no children preflight, no --force. Every
// successful rmi auto-prunes the target's dangling parents (§11, the
// GC engine the later GC passes reuse). Footer: host free space and
// the danger-confirmed host-wide dangling sweep (Clean intermediates)
// with the reclaimable estimate. All ops run sequentially on the
// shared images queue key into the embedded log pane.

// imagesKey is the queue key of the image ops: build, delete-image and
// clean intermediates run one-at-a-time (one sequential op),
// independent of the per-workspace keys.
const imagesKey = "images"

// Row families (imageRow.Kind).
const (
	kindBase      = "base"
	kindComponent = "component"
	kindSet       = "set"
	kindInfra     = "infra"
	kindLegacy    = "legacy"
)

// imageRow is one row of the image screen.
type imageRow struct {
	Kind    string   // row family (kindBase … kindLegacy)
	Name    string   // row title: the unit id / "Base" / the sorted member list / the infra name / the legacy ref
	Tag     string   // the row's image ref (data-tag; delete payload: family ref for base/component, exact tag otherwise)
	Tags    []string // every tagged image the row's delete removes (base/component families)
	Size    string   // human-readable size, "" when missing
	Cached  bool     // the derived content-addressed tag exists (base/component rows)
	Built   bool     // at least one tagged image exists
	InUse   bool     // pinned by a container ref (set/infra/legacy rows — base/components are never pinned)
	Reason  string   // invalid-recipe refusal or "recipe missing" (orphan image)
	Warning string   // user/shipped collision warning line (the shipped row)
	Busy    bool
}

// imageRefs computes the enriched ref list pinning images in use
// (the deleted TUI view's imageRefs parity, #163): every container's image reference
// in any state, plus — while a sandbox-family image is in use — the base
// image the family builds FROM and the infra refs. Errors yield an empty
// list.
func (s *Server) imageRefs() []string {
	refs := discovery.ContainerImageRefs(s.deps.Run)
	if !slices.ContainsFunc(refs, func(ref string) bool {
		return discovery.ParseImageRef(ref).Kind != discovery.RefUnrelated
	}) {
		return refs
	}
	if !slices.Contains(refs, discovery.BaseImageRef) {
		refs = append(refs, discovery.BaseImageRef)
	}
	return append(refs, discovery.ProxyImageTag, discovery.SearxngImageTag, discovery.SttImageTag)
}

// baseRecipePath resolves the toolchains dir's fixed base recipe (§1);
// "" when the seam is unwired (no cached markers, no base row).
func (s *Server) baseRecipePath() string {
	if s.deps.Toolchains == "" {
		return ""
	}
	return filepath.Join(s.deps.Toolchains, "base.Dockerfile")
}

// pinnedRef reports whether any container image reference is the same
// image as tag (exact-ref pin — set/Legacy/infra rows): registry
// prefixes strip, a missing tag reads as :latest.
func pinnedRef(refs []string, tag string) bool {
	return slices.ContainsFunc(refs, func(ref string) bool { return sameImageRef(ref, tag) })
}

// sameImageRef compares two podman image references on their last path
// segment (registry prefixes strip; a missing tag reads as :latest —
// container refs carry no tag).
func sameImageRef(a, b string) bool {
	return lastImageSegment(a) == lastImageSegment(b)
}

func lastImageSegment(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	if j := strings.IndexByte(ref, '@'); j >= 0 {
		return ref // digest refs compare exact
	}
	if j := strings.IndexByte(ref, ':'); j >= 0 {
		return ref[:j] + ":" + ref[j+1:]
	}
	return ref + ":latest"
}

// imageRows builds the image screen rows fresh per request: the base
// row, the component rows (union of discovered recipes and tagged
// component images), the label-enumerated set rows, the shared infra
// rows and the §10 Legacy rows — each with its cached/built markers,
// size and pins.
func (s *Server) imageRows() []imageRow {
	d := s.deps
	sizes := map[string]string{}
	var fam []discovery.SandboxImage // tagged base/tc/set/legacy images
	for _, img := range discovery.SandboxImages(d.Run) {
		sizes[img.Tag] = img.Size
		fam = append(fam, img)
	}
	refs := s.imageRefs()
	busy := s.queue.BusyKey(imagesKey)
	var rows []imageRow

	baseRecipe := s.baseRecipePath()

	// Base row: cached = the derived opencode-sandbox-base:<hash12>
	// exists; built = any tagged base image. Never in-use-pinned.
	var baseTags []string
	baseCachedTag := ""
	if baseRecipe != "" {
		if tag, err := identity.BaseTag(baseRecipe); err == nil {
			baseCachedTag = tag
		}
	}
	for _, img := range fam {
		if discovery.ParseImageRef(img.Tag).Kind == discovery.RefBase {
			baseTags = append(baseTags, img.Tag)
		}
	}
	if baseRecipe != "" {
		row := imageRow{
			Kind: kindBase, Name: "Base", Tag: discovery.BaseImageRef, Tags: baseTags,
			Cached: baseCachedTag != "" && discovery.ImageExistsTag(d.Run, baseCachedTag),
			Built:  len(baseTags) > 0, Busy: busy,
		}
		if row.Cached {
			row.Size = sizes[baseCachedTag]
			row.Tag = baseCachedTag
		} else if len(baseTags) > 0 {
			row.Size = sizes[baseTags[0]]
		}
		rows = append(rows, row)
	}

	// Component rows: the union of discovered units and tagged
	// component images, keyed by id. Invalid recipes render their
	// reason with Build/Rebuild disabled; a user/shipped collision
	// renders its warning line on the shipped row; an orphan image (a
	// tagged id without a recipe) renders "recipe missing".
	var cat discovery.Catalog
	if d.Toolchains != "" {
		cat = discovery.Discover(d.Toolchains)
	}
	tcTags := map[string][]string{}
	for _, img := range fam {
		if r := discovery.ParseImageRef(img.Tag); r.Kind == discovery.RefToolchain {
			tcTags[r.ID] = append(tcTags[r.ID], img.Tag)
		}
	}
	collisionWarn := map[string]string{}
	for _, w := range cat.Warnings {
		if id, ok := strings.CutPrefix(w, "user recipe custom/"); ok {
			if id, _, ok2 := strings.Cut(id, ".Dockerfile"); ok2 {
				collisionWarn[id] = w
			}
		}
	}
	seen := map[string]bool{}
	for _, u := range cat.Units {
		seen[u.ID] = true
		tags := tcTags[u.ID]
		row := imageRow{
			Kind: kindComponent, Name: u.ID,
			Tag:  identity.ComponentRepo + u.ID,
			Tags: tags, Built: len(tags) > 0, Busy: busy,
			Warning: collisionWarn[u.ID],
		}
		if u.Valid {
			if tag, err := identity.ComponentTag(baseRecipe, u.ID, u.Path); err == nil {
				row.Cached = discovery.ImageExistsTag(d.Run, tag)
				if row.Cached {
					row.Size = sizes[tag]
					row.Tag = tag
				}
			}
		} else {
			row.Reason = u.Reason
		}
		if row.Size == "" && len(tags) > 0 {
			row.Size = sizes[tags[0]]
		}
		rows = append(rows, row)
	}
	for _, id := range slices.Sorted(slices.Values(keysOf(tcTags))) {
		if seen[id] {
			continue
		}
		tags := tcTags[id]
		rows = append(rows, imageRow{
			Kind: kindComponent, Name: id, Tag: identity.ComponentRepo + id, Tags: tags,
			Built: true, Size: sizes[tags[0]], Reason: "recipe missing", Busy: busy,
		})
	}

	// Set rows: label-enumerated (never tag parsing), title = the
	// sorted member list, Delete only — derived artifacts rebuilt by
	// their workspace's start path. Pinned by exact container refs.
	for _, set := range discovery.SetImages(d.Run) {
		name := strings.Join(set.Members, ", ")
		if name == "" {
			name = "base only"
		}
		rows = append(rows, imageRow{
			Kind: kindSet, Name: name, Tag: set.Tag, Size: set.Size,
			Built: true, InUse: pinnedRef(refs, set.Tag), Busy: busy,
		})
	}

	// Infra rows: the shared SearXNG / STT images, built and deleted
	// exactly as today; oc-proxy stays off-screen (its build rides the
	// proxy screen's member flow).
	for _, infra := range []struct{ name, tag string }{
		{"SearXNG", discovery.SearxngImageTag},
		{"STT", discovery.SttImageTag},
	} {
		rows = append(rows, imageRow{
			Kind: kindInfra, Name: infra.name, Tag: infra.tag,
			Size:  sizes[infra.tag],
			Built: discovery.ImageExistsTag(d.Run, infra.tag),
			InUse: pinnedRef(refs, infra.tag),
			Busy:  busy,
		})
	}

	// Legacy rows (§10): old-world opencode-sandbox-* refs that are no
	// current base/tc/set tag — delete-only, in-use pinned, the group
	// hides when empty.
	for _, img := range fam {
		if discovery.ParseImageRef(img.Tag).Kind != discovery.RefLegacy {
			continue
		}
		rows = append(rows, imageRow{
			Kind: kindLegacy, Name: img.Tag, Tag: img.Tag, Size: img.Size,
			Built: true, InUse: pinnedRef(refs, img.Tag), Busy: busy,
		})
	}
	return rows
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// renderImages renders the image screen: the Build all confirm button,
// the family/set/infra/Legacy rows, the footer (host free space + Clean
// intermediates) and the embedded log pane the ops stream into (Op.ID
// in data-op; streamed lines ride the SSE `line` events). Rows are
// prototype-style panel rows (icon tile + title/sub + right actions);
// buttons render always — their disabled state carries the refusal
// (busy queue, invalid recipe, nothing to delete, in-use pin). Toast
// (a refused confirm or the last failed op) rides at the top when msg
// is non-empty.
func (s *Server) renderImages(msg string, opID int) string {
	busy := s.queue.BusyKey(imagesKey)
	if msg == "" {
		msg = s.queue.LastError(imagesKey) // the last failed op's honest reason
	}
	var b strings.Builder
	b.WriteString(`<section class="panel plist" data-screen="images">`)
	b.WriteString(`<div class="card-head"><h2>Images</h2>`)
	if busy {
		b.WriteString(`<button class="btn ghost" disabled title="an image operation is queued or running">Build all</button>`)
	} else {
		b.WriteString(`<button class="btn ghost" data-action="build" data-href="images/build" data-confirm="Build all images? This can take a while.">Build all</button>`)
	}
	b.WriteString(`</div>`)
	if msg != "" {
		b.WriteString(`<div class="inline-toast" data-toast>` + html.EscapeString(msg) + `</div>`)
	}
	legacyHead := false
	for _, row := range s.imageRows() {
		if row.Kind == kindLegacy && !legacyHead {
			legacyHead = true
			b.WriteString(`<div class="row"><div class="rl"><div class="t">Legacy</div>` +
				`<div class="s">old-world images superseded by toolchain sets — delete freely</div></div></div>`)
		}
		b.WriteString(`<div class="row" data-tag="` + html.EscapeString(row.Tag) + `"`)
		if row.Busy {
			b.WriteString(` data-busy`)
		}
		b.WriteString(`>`)
		b.WriteString(`<div class="ic">`)
		b.WriteString(string(iconSVG("container")) + `</div>`)
		b.WriteString(`<div class="rl"><div class="t">` + html.EscapeString(row.Name) + `</div><div class="s">`)
		switch {
		case row.Kind == kindBase || row.Kind == kindComponent:
			// Cached markers (the superseded built/missing term):
			// fresh per render, honest about the stale-hash case.
			if row.Cached {
				b.WriteString(`<span class="marker st-running">● cached</span>`)
			} else {
				b.WriteString(`<span class="marker st-off">○ not cached</span>`)
			}
			if row.Reason != "" {
				b.WriteString(`<span class="marker st-off">⚠ ` + html.EscapeString(row.Reason) + `</span>`)
			}
			if row.Warning != "" {
				b.WriteString(`<span class="marker st-off">⚠ ` + html.EscapeString(row.Warning) + `</span>`)
			}
			if row.Built && row.Size != "" {
				b.WriteString(`<span class="unit-sub">` + html.EscapeString(row.Size) + `</span>`)
			}
		case row.Built:
			b.WriteString(`<span class="marker st-running">● built</span>`)
			if row.Size != "" {
				b.WriteString(`<span class="unit-sub">` + html.EscapeString(row.Size) + `</span>`)
			}
			if row.Kind == kindSet {
				b.WriteString(`<span class="unit-sub">` + html.EscapeString(row.Tag) + `</span>`)
			}
			if row.InUse {
				b.WriteString(`<span class="marker st-running">● in use</span>`)
			} else {
				b.WriteString(`<span class="marker st-off">○ unused</span>`)
			}
		default:
			b.WriteString(`<span class="marker st-off">○ missing</span>`)
		}
		if row.Busy {
			b.WriteString(busyMarker())
		}
		b.WriteString(`</div></div><div class="rc">`)
		s.writeBuildButton(&b, row, busy)
		s.writeDeleteButton(&b, row, busy)
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`<pre class="logpane" data-role="log" data-op="` + itoa(opID) + `"></pre>`)
	s.writeImagesFooter(&b, busy)
	b.WriteString(`</section>`)
	return b.String()
}

// writeBuildButton renders the row's Build/Rebuild button: family rows
// build their derived content (--tc <id> / --base — disabled when the
// recipe is invalid or the recipe is missing), infra rows build as
// today, set/Legacy rows carry none (derived artifacts / recipes gone).
func (s *Server) writeBuildButton(b *strings.Builder, row imageRow, busy bool) {
	var tag, label, confirm string
	switch row.Kind {
	case kindBase:
		tag, label = "base", "Build"
		confirm = "Build the base image? This can take a while."
	case kindComponent:
		if row.Reason == "recipe missing" {
			// An orphan image cannot build — no recipe exists.
			b.WriteString(`<button class="btn ghost" disabled title="recipe missing — no recipe to build from">Build</button>`)
			return
		}
		tag, label = row.Name, "Build"
		confirm = "Build toolchain " + row.Name + "? This can take a while."
	case kindInfra:
		tag, label = row.Tag, "Build"
		confirm = "Build image " + row.Tag + "? This can take a while."
	default:
		return
	}
	if row.Cached || (row.Kind == kindInfra && row.Built) {
		label = "Rebuild"
		confirm = "Rebuild " + row.Name + "? Running containers keep the old image until they are replaced."
	}
	if busy {
		b.WriteString(`<button class="btn ghost" disabled title="an image operation is queued or running">` + label + `</button>`)
	} else if row.Reason != "" {
		b.WriteString(`<button class="btn ghost" disabled title="` + html.EscapeString("recipe invalid — "+row.Reason) + `">` + label + `</button>`)
	} else {
		b.WriteString(`<button class="btn ghost" data-action="build" data-href="images/build" data-tag="` +
			html.EscapeString(tag) + `" data-confirm="` + html.EscapeString(confirm) + `">` + label + `</button>`)
	}
}

// writeDeleteButton renders the row's danger Delete. Base/component
// rows are never in-use-pinned — the danger confirm carries the honest
// cost ("one rebuild when next needed"; base: "needed for every
// component/set build"). Set/Legacy/infra rows pin by container refs.
func (s *Server) writeDeleteButton(b *strings.Builder, row imageRow, busy bool) {
	confirm := "Delete image " + row.Tag + "?"
	switch row.Kind {
	case kindBase:
		confirm = "Delete the base image? Needed for every component/set build — one rebuild when next needed."
	case kindComponent:
		confirm = "Delete image " + row.Tag + "? One rebuild when next needed."
	case kindLegacy:
		confirm = "Delete image " + row.Tag + "? Superseded by toolchain sets — delete freely."
	}
	disabled := ""
	if row.Busy {
		disabled = ` disabled title="an image operation is queued or running"`
	} else if !row.Built && len(row.Tags) == 0 {
		disabled = ` disabled title="not built — nothing to delete"`
	} else if row.InUse {
		disabled = ` disabled title="` + errInUseHint(row.Tag).Error() + `"`
	}
	b.WriteString(`<button class="btn danger-ghost" data-action="img-delete" data-href="images/delete" data-tag="` +
		html.EscapeString(row.Tag) + `" data-confirm="` + html.EscapeString(confirm) + `" data-danger` + disabled + `>Delete</button>`)
}

// writeImagesFooter renders the screen footer: the host free space and
// the Clean intermediates button with the fresh reclaimable estimate
// ("~X GB reclaimable — removes dangling (untagged, unused) images
// from all projects on this machine"), danger-confirmed.
func (s *Server) writeImagesFooter(b *strings.Builder, busy bool) {
	est := reclaimableEstimate(s.deps.Run)
	free := hostFreeSpace(s.deps.Run, s.home())
	b.WriteString(`<div class="row" data-role="img-footer"><div class="rl"><div class="s">`)
	if free != "" {
		b.WriteString(`host free space: ` + html.EscapeString(free) + ` `)
	}
	if est != "" {
		b.WriteString(`<span class="unit-sub">` + html.EscapeString(est) + ` reclaimable in dangling images</span>`)
	}
	b.WriteString(`</div></div><div class="rc">`)
	if busy {
		b.WriteString(`<button class="btn ghost" disabled title="an image operation is queued or running">Clean intermediates</button>`)
	} else {
		confirm := "removes dangling (untagged, unused) images from all projects on this machine."
		if est != "" {
			confirm = "~" + est + " reclaimable — " + confirm
		}
		b.WriteString(`<button class="btn ghost" data-action="build" data-href="images/clean" data-confirm="` +
			html.EscapeString(confirm) + `" data-danger>Clean intermediates</button>`)
	}
	b.WriteString(`</div></div>`)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}

func (s *Server) handleImages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(s.renderImages("", 0)))
}

// handleImagesBuild serves POST /images/build — the confirm's action.
// The tag payload selects the plan: "base" → build-images.sh --base, a
// discovered unit id → --tc <id>, the infra tags build as today
// (`bash <build-container.sh> <tag>`); without a tag all scales to
// --all (base + every discovered component — the infra rows' lazy
// auto-build at member start keeps them out). Unknown args re-render
// with the refusal; a busy key refuses the confirm.
func (s *Server) handleImagesBuild(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.queue.BusyKey(imagesKey) {
		_, _ = w.Write([]byte(s.renderImages("an image operation is already queued or running", 0)))
		return
	}
	tag := strings.TrimSpace(r.FormValue("tag"))
	id := s.queue.reserve()
	switch {
	case tag == "":
		s.queue.Launch(webOp{ID: id, Key: imagesKey, Run: s.buildAllOp()})
	case tag == "base":
		s.queue.Launch(webOp{ID: id, Key: imagesKey, Run: s.buildOneOp("--base")})
	case tag == discovery.ProxyImageTag || tag == discovery.SearxngImageTag || tag == discovery.SttImageTag:
		s.queue.Launch(webOp{ID: id, Key: imagesKey, Run: s.buildInfraOp(tag)})
	default:
		if s.deps.Toolchains != "" {
			if u, ok := discovery.Discover(s.deps.Toolchains).Unit(tag); ok && u.Valid {
				s.queue.Launch(webOp{ID: id, Key: imagesKey, Run: s.buildOneOp("--tc", tag)})
				_, _ = w.Write([]byte(s.renderImages("", id)))
				return
			}
		}
		_, _ = w.Write([]byte(s.renderImages("unknown image: "+tag, 0)))
		return
	}
	_, _ = w.Write([]byte(s.renderImages("", id)))
}

// handleImagesDelete serves POST /images/delete {tag=…} — the danger
// confirm's action. Set/Legacy/infra rows pin by fresh container refs
// (remove its containers first); base/component rows are never pinned
// — the cache, not a dependency. A busy key refuses the confirm;
// otherwise one sequential delete op launches (the op re-checks and
// resolves the family's tags fresh).
func (s *Server) handleImagesDelete(w http.ResponseWriter, r *http.Request) {
	tag := strings.TrimSpace(r.FormValue("tag"))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if tag == "" {
		_, _ = w.Write([]byte(s.renderImages("no image selected", 0)))
		return
	}
	if s.queue.BusyKey(imagesKey) {
		_, _ = w.Write([]byte(s.renderImages("an image operation is already queued or running", 0)))
		return
	}
	switch discovery.ParseImageRef(tag).Kind {
	case discovery.RefBase, discovery.RefToolchain:
		// never in-use-pinned — the cache, not a dependency
	default:
		if pinnedRef(s.imageRefs(), tag) {
			_, _ = w.Write([]byte(s.renderImages(errInUseHint(tag).Error(), 0)))
			return
		}
	}
	s.queue.Launch(webOp{Key: imagesKey, Run: s.deleteImageOp(tag)})
	_, _ = w.Write([]byte(s.renderImages("", 0)))
}

// handleImagesClean serves POST /images/clean — the Clean intermediates
// confirm's action: one sequential op on the shared images key running
// the host-wide dangling-only sweep (the §11 GC engine's shared
// reclaim core). A busy key refuses the confirm.
func (s *Server) handleImagesClean(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.queue.BusyKey(imagesKey) {
		_, _ = w.Write([]byte(s.renderImages("an image operation is already queued or running", 0)))
		return
	}
	id := s.queue.reserve()
	s.queue.Launch(webOp{ID: id, Key: imagesKey, Run: s.cleanIntermediatesOp()})
	_, _ = w.Write([]byte(s.renderImages("", id)))
}

// buildAllOp runs build-images.sh --all as ONE sequential op — base +
// every discovered component (the infra images stay out: they build
// lazily at member start), exactly as the TUI invokes scripts
// (ADR-0007).
func (s *Server) buildAllOp() func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		emit("==> build all (base + every discovered toolchain component)")
		out, err := d.Run.Output("bash", d.ImagesBuildScript, "--all")
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		return err
	}
}

// buildOneOp builds one derived image (`bash <build-images.sh>
// --base` / `--tc <id>` — an existing tag is never rebuilt, the build
// skips cached). Exactly as the TUI invokes scripts (ADR-0007).
func (s *Server) buildOneOp(args ...string) func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		emit("==> build " + strings.Join(args, " "))
		out, err := d.Run.Output("bash", append([]string{d.ImagesBuildScript}, args...)...)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		return err
	}
}

// buildInfraOp builds one shared infra image (`bash
// <build-container.sh> <tag>`) — the infra images live outside the
// toolchain builder's contract and build exactly as today.
func (s *Server) buildInfraOp(tag string) func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		emit("==> build " + tag)
		out, err := d.Run.Output("bash", d.BuildScript, tag)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		return err
	}
}

// deleteImageOp mirrors the TUI Delete-Images op (containerview
// launchImagesDelete): the family ref resolves to its tagged images
// fresh (one podman rmi per tag), each success auto-prunes the
// target's dangling parents (§11 — the GC engine), and the outcome is
// verified gone against a fresh sandbox-image list. In-use refusal ran
// at confirm time (set/Legacy/infra rows) and re-checks inside the op.
// Parent-having rows are refusal-first: podman rmi's exit-2 surfaces
// with the honest reason — no children preflight, no --force.
func (s *Server) deleteImageOp(tag string) func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		kind := discovery.ParseImageRef(tag).Kind
		if kind != discovery.RefBase && kind != discovery.RefToolchain {
			if pinnedRef(s.imageRefs(), tag) {
				return errInUseHint(tag)
			}
		}
		var targets []string
		for _, img := range discovery.SandboxImages(d.Run) {
			r := discovery.ParseImageRef(img.Tag)
			switch {
			case kind == discovery.RefBase && r.Kind == discovery.RefBase:
				targets = append(targets, img.Tag)
			case kind == discovery.RefToolchain && r.Kind == discovery.RefToolchain && r.ID == discovery.ParseImageRef(tag).ID:
				targets = append(targets, img.Tag)
			case kind == discovery.RefBase || kind == discovery.RefToolchain:
			case sameImageRef(img.Tag, tag):
				targets = append(targets, img.Tag)
			}
		}
		if len(targets) == 0 {
			return errStillExists(tag)
		}
		for _, target := range targets {
			emit("==> podman rmi " + target)
			out, err := d.Run.Output("podman", "rmi", target)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				if exitCodeOf(err) == 2 {
					// Refusal-first: the image is a build parent of
					// newer images — it shares their layers and holds
					// no space of its own; no --force exists here.
					reason := fmt.Sprintf("podman refuses to remove %s — build parent of newer images — shares their layers, holds no space of its own; remove the newer ones first", target)
					emit(reason)
					return errors.New(reason)
				}
				return err
			}
			s.pruneDangling(emit)
		}
		for _, img := range discovery.SandboxImages(d.Run) {
			if slices.Contains(targets, img.Tag) {
				return errStillExists(img.Tag)
			}
		}
		return nil
	}
}

// pruneDangling is the §11 GC engine's reclaim core: every successful
// podman rmi auto-prunes the target's dangling parents — the host-wide
// dangling-only sweep (untagged, unused images of any origin; never
// tagged images, never container-referenced ones). The Clean
// intermediates op is the same sweep behind the danger confirm; the
// GC-at-update pass (#214) reuses it.
func (s *Server) pruneDangling(emit func(string)) (string, error) {
	before := reclaimableEstimate(s.deps.Run)
	out, err := s.deps.Run.Output("podman", "image", "prune", "--force")
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	if err != nil {
		return before, err
	}
	if before != "" {
		emit("reclaimed ~" + before + " of dangling intermediates")
	}
	return before, nil
}

// cleanIntermediatesOp runs the host-wide dangling-only sweep as ONE
// sequential op on the images key — the danger-confirmed Clean
// intermediates button's action (podman image prune semantics, never
// -a: untagged, unused images only; never tagged, never
// container-referenced, never host files).
func (s *Server) cleanIntermediatesOp() func(emit func(string)) error {
	return func(emit func(string)) error {
		emit("==> clean intermediates (host-wide dangling sweep)")
		_, err := s.pruneDangling(emit)
		return err
	}
}

// reclaimableEstimate sums the dangling (untagged, unused) images'
// sizes into the human estimate the footer and the Clean confirm show,
// fresh per render; "" when the query fails.
func reclaimableEstimate(run cmdrun.Runner) string {
	out, err := run.Output("podman", "images", "--filter", "dangling=true", "--format", "{{.Size}}")
	if err != nil {
		return ""
	}
	var total float64
	for _, l := range cmdrun.Lines(out) {
		total += parseSizeBytes(l)
	}
	if total == 0 {
		return "" // nothing reclaimable — the footer shows no estimate
	}
	return humanSize(total)
}

// hostFreeSpace reads the host's free space on home's filesystem
// (`df -h -P` — the footer's honest number); "" when unreadable.
func hostFreeSpace(run cmdrun.Runner, dir string) string {
	out, err := run.Output("df", "-h", "-P", dir)
	if err != nil {
		return ""
	}
	lines := cmdrun.Lines(out)
	if len(lines) < 2 {
		return ""
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return ""
	}
	return fields[3]
}

var sizeUnits = map[string]float64{
	"B": 1, "kB": 1e3, "KB": 1e3, "MB": 1e6, "GB": 1e9, "TB": 1e12,
}

// parseSizeBytes parses a podman human size ("1.2 GB", "310 MB") into
// bytes (unparseable → 0).
func parseSizeBytes(s string) float64 {
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.' || s[i] == ',') {
		i++
	}
	num, err := strconv.ParseFloat(strings.ReplaceAll(s[:i], ",", "."), 64)
	if err != nil {
		return 0
	}
	return num * sizeUnits[strings.TrimSpace(s[i:])]
}

// humanSize renders bytes as a one-decimal human size (GB above a
// gigabyte, the footer's unit).
func humanSize(b float64) string {
	switch {
	case b >= 1e12:
		return trimSize(b/1e12) + " TB"
	case b >= 1e9:
		return trimSize(b/1e9) + " GB"
	case b >= 1e6:
		return trimSize(b/1e6) + " MB"
	case b >= 1e3:
		return trimSize(b/1e3) + " kB"
	default:
		return trimSize(b) + " B"
	}
}

func trimSize(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.TrimSuffix(strings.TrimSuffix(s, "0"), ".")
}

// exitCodeOf extracts the exit code of a runner error (both the real
// *exec.ExitError and the test fakes' "exit status N" errors).
func exitCodeOf(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ProcessState != nil {
		return ee.ExitCode()
	}
	const prefix = "exit status "
	if s, ok := strings.CutPrefix(err.Error(), prefix); ok {
		if n, e := strconv.Atoi(s); e == nil {
			return n
		}
	}
	return -1
}

func errInUseHint(tag string) error {
	return fmt.Errorf("in use, remove its containers first: %s", tag)
}
