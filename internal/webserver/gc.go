package webserver

// The §11 image GC pass (spec toolchain-sets §11, GLOSSARY "image GC
// pass"): the automatic reclaim routine with two triggers — update
// completion (inside the update op chain, the "reclaimed ~X GB"
// summary streaming into the update log pane) and a recipe-editor save
// (its own follow-up op on the shared images key, skip-if-busy, never
// queued). The pass removes family-tagged images (base/tc/set) whose
// hash is no longer derivable from the current recipes and that no
// container references; exit-2 child targets are skipped (shared
// layers, zero reclaim lost), and the host-wide dangling sweep follows.
// Abandoned sets (no stored workspace derives them, no container refs)
// die at the next pass; Legacy rows and untagged old-world refs are
// never auto-deleted (§10 — delete-only). `toolchains/snapshots/` is a
// host dir — image GC never touches it by construction.

import (
	"path/filepath"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/identity"
	"github.com/shredingerlabs/oc-sandbox/internal/openworkspace"
)

// updateGCPass is the update op's completion step: after the install
// script applied the release, the pass reclaims the superseded family
// images and runs the dangling sweep, streaming into the update log
// pane. Never fails the update (the release already applied — a broken
// pass surfaces as honest lines, the next pass reclaims).
func (s *Server) updateGCPass(emit func(string)) error {
	if s.deps == nil || s.deps.Run == nil {
		return nil // no podman runner wired — nothing to reclaim
	}
	emit("==> image gc (update applied — reclaiming superseded images)")
	summary, err := s.imageGCPass(emit)
	if err != nil {
		emit("image gc incomplete: " + err.Error())
		return nil
	}
	emit(summary)
	return nil
}

// imageGCPass runs one §11 GC pass and returns its summary line
// ("reclaimed ~X"). Systemic failures (the dangling sweep's prune)
// return as the error; per-image rmi failures surface as honest lines.
func (s *Server) imageGCPass(emit func(string)) (string, error) {
	run := s.deps.Run
	root := s.toolchainsRoot()
	basePath := filepath.Join(root, "base.Dockerfile")
	cat := discovery.Discover(root)
	refs := s.imageRefs()
	storedSets := s.storedSetTags()

	var reclaimed float64
	// Base and component rows: a tag survives iff its hash derives from
	// the current recipes. Untagged old-world refs stay (§10 Legacy —
	// delete-only); a non-derivable tag a container still references
	// stays pinned until its container is gone.
	for _, img := range discovery.SandboxImages(run) {
		r := discovery.ParseImageRef(img.Tag)
		switch r.Kind {
		case discovery.RefBase, discovery.RefToolchain:
		default:
			continue // Legacy/infra/unrelated — never auto-deleted
		}
		if r.Tag == "" {
			emit("kept " + img.Tag + " — old-world image (the Legacy rows on the Images screen are delete-only)")
			continue
		}
		if derivableTag(basePath, cat, r) {
			continue
		}
		if pinnedRef(refs, img.Tag) {
			emit("kept " + img.Tag + " — a container still references it")
			continue
		}
		if gcRmi(run, emit, img.Tag, img.Size, &reclaimed) {
			continue
		}
	}

	// Set rows (label-enumerated — the members label feeds the hash
	// derivation): a set survives iff its hash still derives AND a
	// stored workspace selection derives it (or a container references
	// it — checked above). Abandoned sets die; a workspace returning to
	// a swept set pays one rebuild.
	for _, set := range discovery.SetImages(run) {
		if pinnedRef(refs, set.Tag) {
			continue
		}
		if derivableSetTag(basePath, cat, set) && storedSets[discovery.ParseImageRef(set.Tag).Tag] {
			continue
		}
		if gcRmi(run, emit, set.Tag, set.Size, &reclaimed) {
			continue
		}
	}

	// The host-wide dangling sweep (the shared reclaim core): the pass's
	// own rmis leave dangling parents behind — the sweep catches them.
	before, err := s.pruneDanglingCore(emit)
	reclaimed += parseSizeBytes(before)
	return "reclaimed ~" + humanSize(reclaimed), err
}

// gcRmi removes one image and accounts its size; false means it died
// honestly (exit-2 child target skipped — shared layers, zero reclaim
// lost) or failed loudly. true means reclaimed.
func gcRmi(run cmdrun.Runner, emit func(string), tag, size string, reclaimed *float64) bool {
	*reclaimed += parseSizeBytes(size)
	emit("==> podman rmi " + tag)
	out, err := run.Output("podman", "rmi", tag)
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	if err == nil {
		return true
	}
	*reclaimed -= parseSizeBytes(size)
	if exitCodeOf(err) == 2 {
		emit("skipped " + tag + " — build parent of newer images (shares their layers, holds no space of its own; remove the newer ones first)")
	} else {
		emit("could not remove " + tag + ": " + err.Error())
	}
	return false
}

// derivableTag reports whether a base/tc image tag's hash12 derives
// from the current recipes (spec §6: recipe content, not versions —
// the recipe gone or changed makes the image superseded).
func derivableTag(basePath string, cat discovery.Catalog, r discovery.ImageRef) bool {
	switch r.Kind {
	case discovery.RefBase:
		h, err := identity.BaseHash12(basePath)
		return err == nil && r.Tag == h
	case discovery.RefToolchain:
		u, ok := cat.Unit(r.ID)
		if !ok || !u.Valid {
			return false
		}
		h, err := identity.ComponentHash12(basePath, r.ID, u.Path)
		return err == nil && r.Tag == h
	}
	return false
}

// derivableSetTag reports whether a set image's tag derives from its
// own members label over the current recipes (all members valid).
func derivableSetTag(basePath string, cat discovery.Catalog, set discovery.SetImage) bool {
	members := make([]identity.Member, 0, len(set.Members))
	for _, id := range set.Members {
		u, ok := cat.Unit(id)
		if !ok || !u.Valid {
			return false
		}
		members = append(members, identity.Member{ID: id, RecipePath: u.Path})
	}
	h, err := identity.SetHash12(basePath, members)
	if err != nil {
		return false
	}
	return discovery.ParseImageRef(set.Tag).Tag == h
}

// storedSetTags collects the content-addressed set tags the stored
// workspace selections derive (spec §11 — the abandoned-set rule): a
// materialized config's toolchains vector derives its set against live
// discovery; unreadable/unresolved configs contribute nothing. Errors
// yield an empty set (nothing counts as derived — the pass reclaims).
func (s *Server) storedSetTags() map[string]bool {
	keep := map[string]bool{}
	entries, err := s.regView().AllOrdered()
	if err != nil {
		return keep
	}
	root := s.toolchainsRoot()
	for _, e := range entries {
		cfg, err := s.readSandboxCfg(e.Path)
		if err != nil || !cfg.ToolchainsStored {
			continue
		}
		if tag, ok := openworkspace.SetTagFor(root, cfg.Toolchains); ok {
			keep[discovery.ParseImageRef(tag).Tag] = true // bare hash12 — registry prefixes strip
		}
	}
	return keep
}

// pruneDanglingCore is pruneDangling without the summary line: it runs
// the host-wide dangling-only sweep and returns the reclaim estimate
// taken before it ("", when nothing was reclaimable) — the GC pass
// folds it into its own total.
func (s *Server) pruneDanglingCore(emit func(string)) (string, error) {
	before := reclaimableEstimate(s.deps.Run)
	out, err := s.deps.Run.Output("podman", "image", "prune", "--force")
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	return before, err
}
