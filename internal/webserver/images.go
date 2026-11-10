package webserver

import (
	"fmt"
	"html"
	"net/http"
	"slices"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
)

// images.go serves the Image screen (spec §7/#158): edition rows from
// the live edition registry (ADR 0019) with built/missing markers and
// sizes via discovery; Build (scaled to `all`) runs build-container.sh
// exactly as the TUI invokes scripts (ADR-0007) as one sequential op
// through the op queue into the embedded log pane; delete is the
// danger confirm with in-use refusal semantics. Both refresh the
// markers after the op (the workspace SSE event re-renders the rows).

// imagesKey is the queue key of the image ops: build and delete-image
// run one-at-a-time (one sequential op), independent of the
// per-workspace keys.
const imagesKey = "images"

// imageRow is one row of the image screen.
type imageRow struct {
	Edition string // edition name, or the infra tag for SearXNG
	Tag     string // full image tag (opencode-sandbox-<ed> / oc-searxng)
	Infra   bool   // shared SearXNG infra row (non-edition)
	Built   bool
	Size    string // human-readable size, "" when missing
	InUse   bool
	Busy    bool
}

// imageRefs computes the enriched ref list pinning images in use
// (containerview.imageRefs parity): every container's image reference
// in any state, plus — while an edition image is in use — the parent
// stages and the infra refs. Errors yield an empty list.
func (s *Server) imageRefs() []string {
	refs := discovery.ContainerImageRefs(s.deps.Run)
	if !slices.ContainsFunc(refs, func(ref string) bool {
		return discovery.EditionFromImage(ref) != ""
	}) {
		return refs
	}
	for _, parent := range discovery.ParentStages(s.deps.Dockerfile, s.deps.DockerfileCustom) {
		if discovery.EditionFromImage(parent) != "" && !slices.Contains(refs, parent) {
			refs = append(refs, parent)
		}
	}
	if !slices.Contains(refs, discovery.BaseImageRef) {
		refs = append(refs, discovery.BaseImageRef)
	}
	return append(refs, discovery.ProxyImageTag, discovery.SearxngImageTag)
}

// imageRows builds the image screen rows fresh per request: every
// discovered edition plus the shared SearXNG infra row, each with its
// built/missing marker, size and in-use marker.
func (s *Server) imageRows() []imageRow {
	d := s.deps
	sizes := map[string]string{}
	for _, img := range discovery.SandboxImages(d.Run) {
		sizes[img.Tag] = img.Size
	}
	refs := s.imageRefs()
	busy := s.queue.BusyKey(imagesKey)
	var rows []imageRow
	eds, err := discovery.Editions(d.Dockerfile, d.DockerfileCustom)
	if err == nil {
		for _, ed := range eds {
			tag := "opencode-sandbox-" + ed
			rows = append(rows, imageRow{
				Edition: ed,
				Tag:     tag,
				Built:   discovery.ImageExists(d.Run, ed),
				Size:    sizes[tag],
				InUse:   discovery.ImageInUse(refs, tag),
				Busy:    busy,
			})
		}
	}
	tag := discovery.SearxngImageTag
	rows = append(rows, imageRow{
		Edition: "SearXNG",
		Tag:     tag,
		Infra:   true,
		Built:   discovery.ImageExistsTag(d.Run, tag),
		Size:    sizes[tag],
		InUse:   discovery.ImageInUse(refs, tag),
		Busy:    busy,
	})
	return rows
}

// renderImages renders the image screen: the Build (all) confirm
// button, the edition rows with markers and sizes, and the embedded
// log pane the build op streams into (Op.ID in data-op; streamed lines
// ride the SSE `line` events). Toast (a refused confirm) rides at the
// top when msg is non-empty.
func (s *Server) renderImages(msg string, opID int) string {
	var b strings.Builder
	b.WriteString(`<section class="card" data-screen="images">`)
	b.WriteString(`<div class="card-head"><h2>Images</h2>`)
	if !s.queue.BusyKey(imagesKey) {
		b.WriteString(`<button data-action="build" data-href="images/build" data-confirm="Build all images? This can take a while.">Build all</button>`)
	}
	b.WriteString(`</div>`)
	if msg != "" {
		b.WriteString(`<div class="toast" data-toast>` + html.EscapeString(msg) + `</div>`)
	}
	b.WriteString(`<div class="rows">`)
	for _, row := range s.imageRows() {
		b.WriteString(`<div class="row" data-tag="` + html.EscapeString(row.Tag) + `"`)
		if row.Busy {
			b.WriteString(` data-busy`)
		}
		b.WriteString(`><div class="row-main">`)
		b.WriteString(`<span class="row-name">` + html.EscapeString(row.Edition) + `</span>`)
		if row.Built {
			b.WriteString(`<span class="marker st-running">● built</span>`)
			if row.Size != "" {
				b.WriteString(`<span class="row-sub">` + html.EscapeString(row.Size) + `</span>`)
			}
		} else {
			b.WriteString(`<span class="marker st-off">○ missing</span>`)
		}
		if row.InUse {
			b.WriteString(`<span class="marker st-running">● in use</span>`)
		} else if row.Built {
			b.WriteString(`<span class="marker st-off">○ unused</span>`)
		}
		if row.Busy {
			b.WriteString(busyMarker())
		}
		b.WriteString(`</div><div class="actions">`)
		if row.Built && !row.Busy && !row.InUse {
			b.WriteString(`<button data-action="img-delete" data-href="images/delete" data-tag="` +
				html.EscapeString(row.Tag) + `" data-confirm="Delete image ` +
				html.EscapeString(row.Tag) + `?" data-danger>Delete</button>`)
		}
		b.WriteString(`</div></div>`)
	}
	b.WriteString(`</div>`)
	b.WriteString(`<pre class="logpane" data-role="log" data-op="` + itoa(opID) + `"></pre>`)
	b.WriteString(`</section>`)
	return b.String()
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

// handleImagesBuild serves POST /images/build — the confirm's action
// ("Build all images?"): the confirm refuses while an image op is
// queued or running; otherwise one sequential build op launches, all
// editions scaled to `all` via build-container.sh (script contract:
// no-argument run builds every discovered edition).
func (s *Server) handleImagesBuild(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.queue.BusyKey(imagesKey) {
		_, _ = w.Write([]byte(s.renderImages("an image operation is already queued or running", 0)))
		return
	}
	id := s.queue.Launch(imagesKey, s.buildAllOp())
	_, _ = w.Write([]byte(s.renderImages("", id)))
}

// handleImagesDelete serves POST /images/delete {tag=…} — the danger
// confirm's action; in-use images are refused with the
// remove-containers-first hint.
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
	s.queue.Launch(imagesKey, s.deleteImageOp(tag))
	_, _ = w.Write([]byte(s.renderImages("", 0)))
}

// buildAllOp runs build-container.sh scaled to `all` as ONE sequential
// op — the script builds every discovered edition (and the infra
// images) in one invocation, exactly as the TUI invokes scripts:
// `bash <script> all` through the runner (ADR-0007).
func (s *Server) buildAllOp() func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		emit("==> build all editions")
		out, err := d.Run.Output("bash", d.BuildScript, "all")
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		if err != nil {
			return err
		}
		return nil
	}
}

// deleteImageOp mirrors the TUI Delete-Images op (containerview
// launchImagesDelete, one tag): podman rmi, verified gone against a
// fresh sandbox-image list. In-use refusal ran at confirm time and
// re-checks inside the op.
func (s *Server) deleteImageOp(tag string) func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		if refs := s.imageRefs(); discovery.ImageInUse(refs, tag) {
			return errInUseHint(tag)
		}
		out, err := d.Run.Output("podman", "rmi", tag)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		if err != nil {
			return err
		}
		for _, img := range discovery.SandboxImages(d.Run) {
			if img.Tag == tag {
				return errStillExists(tag)
			}
		}
		return nil
	}
}

func errInUseHint(tag string) error {
	return fmt.Errorf("in use, remove its containers first: %s", tag)
}
