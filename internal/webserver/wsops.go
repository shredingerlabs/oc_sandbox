// Workspace ops (#154, spec §9 Workspace ops + §7 card confirm rows):
// POST /workspaces/{name}/stop and POST /workspaces/{name}/delete wire
// the Open screen's Stop/Delete confirm rows to the real ops; POST
// /workspaces/{name}/adopt is the new web-native adopt confirm's action
// — both running-container replacements (the edition-mismatch switch
// and the web re-adopt) ride it, since an agent may be mid-task. The
// registry workspace name is the URL identity (§9); the ops share the
// workspace's container name as their queue key, so the confirms refuse
// across all of a workspace's actions. The card rows answer with small
// JSON sidecars (§9: plain fetch instead of HTMX on the Open screen).
package webserver

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/identity"
	"github.com/shredingerlabs/oc-sandbox/internal/openworkspace"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// mismatchSidecar is the start route's set-tag mismatch verdict (#154,
// re-keyed to the derived set tag — spec §9 WP-6/§10): the live
// container's image ref diverges from the stored selection's derived
// set tag — the card offers Use current / Switch to <saved>, the
// switch behind the adopt confirm; body is the member delta the prompt
// shows.
type mismatchSidecar struct {
	Mismatch bool   `json:"mismatch"`
	Running  string `json:"running"` // the live container's image ref
	Saved    string `json:"saved"`   // the stored set's derived label
	Body     string `json:"body"`    // the member-delta line
}

// buildSidecar is the start route's missing-set verdict (WP-6): the
// derived set image is not built — the card offers Build now / Build
// later / Go back; set is the member summary, hint the Build-later
// escape.
type buildSidecar struct {
	Build bool   `json:"build"`
	Set   string `json:"set"`
	Hint  string `json:"hint"`
}

// opSidecar is the {ok, error} answer of the confirm-row verbs; op is
// the queued op's id when one launched (the SSE line events key on it).
type opSidecar struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Op    int    `json:"op,omitempty"`
}

// refuseSidecar writes an ok:false sidecar — the confirm refusal (busy
// key, stop-first, missing image, nothing to adopt) as 200 + payload,
// like the container center's inline-toast refusals.
func refuseSidecar(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(jsonMarshal(opSidecar{OK: false, Error: msg})))
}

// okSidecar writes the op's acceptance: {ok:true, op:<id>} — the id the
// busy card carries so streamed line events find their log.
func okSidecar(w http.ResponseWriter, opID int) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(jsonMarshal(opSidecar{OK: true, Op: opID})))
}

// busyRefusal is the shared per-key confirm-refusal message (§9:
// confirms refuse while an op is queued or running).
func busyRefusal(key string) string {
	return "an operation is already queued or running for " + key
}

// setupRefusal is the shared per-workspace pending-setup refusal
// message (§11: a pending first-run skills session holds the
// workspace's container actions).
func setupRefusal(name string) string {
	return "refused — first-run setup session pending for " + name
}

// guardWorkspaceOp is the shared stop/delete/adopt guard preamble: the
// URL identity resolves to a workspace, and the confirm refuses while
// an op is queued or running for the workspace's container (§9) or a
// first-run setup session holds it (§11). Returns the workspace and
// its container-name queue key; refused=true means the request was
// already answered.
func (s *Server) guardWorkspaceOp(w http.ResponseWriter, r *http.Request) (registry.Workspace, string, bool) {
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return registry.Workspace{}, "", true
	}
	cname := registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(cname) {
		refuseSidecar(w, busyRefusal(cname))
		return registry.Workspace{}, "", true
	}
	if s.setupHolds(ws.Name) {
		refuseSidecar(w, setupRefusal(ws.Name))
		return registry.Workspace{}, "", true
	}
	return ws, cname, false
}

// handleWorkspaceRestart serves POST /workspaces/{name}/restart — the
// card's Restart confirm (ADR-0033 chain, ADR-0037 activation): an
// explicit, confirmed stop → fresh-start replacement of a running
// container with the saved settings applied, always available on a
// running container. Always re-checked fresh at click time: an already
// stopped container refuses (the start options are the recovery), a
// busy key or a pending setup session holds it, and the replacement is
// built BEFORE anything stops (WP-6: never stop without a replacement
// → build the replacement, then stop; the builder's preflight refusal
// or a failed build leaves the container untouched).
func (s *Server) handleWorkspaceRestart(w http.ResponseWriter, r *http.Request) {
	ws, cname, refused := s.guardWorkspaceOp(w, r)
	if refused {
		return
	}
	if !slices.Contains(discovery.RunningContainers(s.runnerSeam()), cname) {
		// The start options are the recovery — restart is never
		// "start with extra steps" (ADR-0033).
		refuseSidecar(w, "stopped — restart starts nothing; use the start options")
		return
	}
	cfg, err := s.readSandboxCfg(ws.Path)
	if err != nil {
		refuseSidecar(w, err.Error())
		return
	}
	if err := validateStartCfg(cfg); err != nil {
		refuseSidecar(w, err.Error())
		return
	}
	if !cfg.Resolved {
		// The §10 unresolved refusal (WP-10): before any machinery —
		// the old image/container is never touched.
		refuseSidecar(w, errUnresolvedSelection(cfg).Error())
		return
	}
	s.queue.Launch(webOp{Key: cname, Run: s.restartOp(ws)})
	okSidecar(w, s.queue.CurrentOpID(cname))
}

// restartOp is the verified replacement chain (ADR-0033): the inline
// Build now for a missing set image (WP-6 — the replacement is built
// while the old container still runs), then the pod-aware stop with the
// registry flipping truthful after the verified-gone check, then the
// fresh start from the CURRENT stored settings (the image check re-runs
// inside the op — builds may have landed since the confirm), the
// registry flipping to "running" only after a fresh-ps verification. A
// failed build or start leaves the workspace untouched/stopped with the
// failure streamed into the op log (no rollback — start options are the
// recovery).
func (s *Server) restartOp(ws registry.Workspace) func(emit func(string)) error {
	return func(emit func(string)) error {
		cname := registry.ContainerName(ws.ContainerID)
		emit("==> restart " + cname)
		if cfg, err := s.readSandboxCfg(ws.Path); err == nil && cfg.Resolved {
			// Build the replacement before anything stops (WP-6): the
			// builder's own preflight refuses offline/low-disk hosts
			// (§7; the Go preflight is #212).
			if err := s.buildSetNow(cfg, emit); err != nil {
				emit("Restart refused — " + err.Error())
				return err
			}
		}
		if err := s.stopUnitOnce(cname, emit); err != nil {
			return err
		}
		if err := s.deps.Registry.UpdateStatus(ws.Path, "stopped"); err != nil {
			return err
		}
		emit("Registry status: stopped")
		if err := s.freshStart(ws, emit); err != nil {
			emit("Restart failed — " + err.Error())
			return err
		}
		if err := s.finishVerifiedStart(ws); err != nil {
			emit("Restart failed — " + err.Error())
			return err
		}
		return nil
	}
}

// handleWorkspaceStop serves POST /workspaces/{name}/stop (#154) — the
// existing graceful stop flow through the op runner (the pod-as-unit
// stop, verified gone before the registry flip): the same op the
// Container center's stop confirm launches and, since ADR-0033's
// update, the workspace card's stop confirm. Always re-checked fresh at
// click time (ADR-0037's server-as-gatekeeper, mirroring restart): a
// workspace whose fresh ps reads not-running refuses — the stop op
// would error against the already-gone unit.
func (s *Server) handleWorkspaceStop(w http.ResponseWriter, r *http.Request) {
	_, cname, refused := s.guardWorkspaceOp(w, r)
	if refused {
		return
	}
	if !slices.Contains(discovery.RunningContainers(s.runnerSeam()), cname) {
		refuseSidecar(w, "stopped — nothing to stop")
		return
	}
	s.queue.Launch(webOp{Key: cname, Run: s.stopOp(cname)})
	okSidecar(w, s.queue.CurrentOpID(cname))
}

// handleWorkspaceDelete serves POST /workspaces/{name}/delete (#154) —
// the card confirm row's danger-confirmed delete: the Container-center
// delete op (never force-killing a running unit — the stop-first
// refusal re-checks fresh podman state; pod-as-unit), keyed on the
// workspace's container name.
func (s *Server) handleWorkspaceDelete(w http.ResponseWriter, r *http.Request) {
	_, cname, refused := s.guardWorkspaceOp(w, r)
	if refused {
		return
	}
	if s.runningUnit(cname) {
		refuseSidecar(w, s.stopFirstHint(cname))
		return
	}
	s.queue.Launch(webOp{Key: cname, Run: s.deleteContainerOp(cname)})
	okSidecar(w, s.queue.CurrentOpID(cname))
}

// runningImageRef reads the live container's image ref (the image is
// the truth about what runs, #115) — "" when the container is not live.
func (s *Server) runningImageRef(cname string) string {
	for _, c := range discovery.RunningContainersWithImages(s.runnerSeam()) {
		if c.Name == cname {
			return c.Image
		}
	}
	return ""
}

// savedSetTag derives the stored selection's content-addressed set tag
// (spec toolchain-sets §6): the resolved member ids — the read path
// already expanded a legacy edition through the bundle map (§10) —
// content-addressed against live discovery. ok=false names an
// underivable tag (an invalid or unknown member, no base recipe): the
// honest missing-set state, never guessed around.
func (s *Server) savedSetTag(cfg sandboxcfg.Config) (string, bool) {
	return openworkspace.SetTagFor(s.toolchainsRoot(), cfg.Toolchains)
}

// setMismatchVerdict classifies the live container's image ref against
// the stored selection's derived set tag (spec §9 WP-6, §10): reuse
// matches set-tag refs only — an old-world ref mismatches on every
// touch. The body is the member delta the three-way prompt shows:
// added/removed ids off the running set's members label; same members
// with a new hash → "toolchain recipes updated"; an old-world ref →
// "predates toolchain sets" plus the bundle-map-expanded delta;
// members unknown → the honest Switch-rebuilds line.
func (s *Server) setMismatchVerdict(cfg sandboxcfg.Config, runningRef string) mismatchSidecar {
	v := mismatchSidecar{Running: runningRef}
	id := setIdentityFor(cfg, s.toolchainsRoot(), cfg.WorkspaceKind)
	v.Saved = id.Label
	parsed := discovery.ParseImageRef(runningRef)
	v.Mismatch = true // reuse matches the derived set tag only (§10)
	switch parsed.Kind {
	case discovery.RefSet:
		tag, ok := s.savedSetTag(cfg)
		if ok && parsed.Tag == strings.TrimPrefix(tag, identity.SetRepo+":") {
			return mismatchSidecar{Running: runningRef, Saved: id.Label} // the derived set tag — reuse
		}
		running, known := discovery.SetMembers(s.runnerSeam(), runningRef)
		if !known {
			v.Body = "members unknown — Switch rebuilds from your set"
			return v
		}
		if v.Body = memberDelta(running, cfg.Toolchains); v.Body == "" {
			v.Body = "toolchain recipes updated"
		}
	case discovery.RefLegacy:
		v.Body = "this container predates toolchain sets"
		if members, ok := sandboxcfg.ExpandBundle(s.toolchainsRoot(), parsed.ID); ok {
			if d := memberDelta(members, cfg.Toolchains); d != "" {
				v.Body += " — " + d
			}
		} else {
			v.Body += " — members unknown, Switch rebuilds from your set"
		}
	default:
		v.Body = "members unknown — Switch rebuilds from your set"
	}
	return v
}

// memberDelta renders the added/removed member delta line ("added go /
// removed java, rust"); empty when the member lists agree.
func memberDelta(running, saved []string) string {
	var added, removed []string
	for _, id := range saved {
		if !slices.Contains(running, id) {
			added = append(added, id)
		}
	}
	for _, id := range running {
		if !slices.Contains(saved, id) {
			removed = append(removed, id)
		}
	}
	var parts []string
	if len(added) > 0 {
		parts = append(parts, "added "+strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		parts = append(parts, "removed "+strings.Join(removed, ", "))
	}
	return strings.Join(parts, " / ")
}

// buildSetNow runs the builder's --set plan (missing components +
// assembly, spec §7) streamed through emit, then re-verifies the derived
// tag landed. The Go preflight (§7 items 2+3, WP-8 #212) refuses first
// on a low-disk or offline host — the builder's own preflight re-checks
// at build time; a refusal stops nothing.
func (s *Server) buildSetNow(cfg sandboxcfg.Config, emit func(string)) error {
	tag, ok := s.savedSetTag(cfg)
	if ok && discovery.ImageExistsTag(s.runnerSeam(), tag) {
		return nil
	}
	ids := strings.Join(cfg.Toolchains, ",")
	if ids == "" {
		emit("==> Building base-only set image")
	} else {
		emit("==> Building toolchain set (" + strings.Join(cfg.Toolchains, ", ") + ")")
	}
	if plan, pok := s.builderPlanSet(cfg.Toolchains); pok {
		if err := s.preflightBuild(plan); err != nil {
			emit(err.Error())
			return err
		}
	}
	out, err := s.runnerSeam().Output("bash", append([]string{s.deps.BuildImagesScript}, "--set", ids)...)
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	if err != nil {
		return errors.New("build failed — the set image is still unavailable; nothing was stopped")
	}
	tag, ok = s.savedSetTag(cfg)
	if !ok || !discovery.ImageExistsTag(s.runnerSeam(), tag) {
		return errors.New("build failed — the set image is still unavailable; nothing was stopped")
	}
	return nil
}

// errSetImageMissing is the missing-set refusal: the Images hint is the
// Build-later escape (WP-6).
func errSetImageMissing(tag string) error {
	return fmt.Errorf("set image %s is not built yet — build it under Settings → Images, or start again and choose Build now", tag)
}

// handleWorkspaceAdopt serves POST /workspaces/{name}/adopt (#154) —
// behind the new web-native adopt confirm (§9): the set-tag mismatch
// switch (WP-6) and the web re-adopt both replace a running container
// (an agent may be mid-task), so both confirm first. The switch runs
// the verified build → stop → start chain (WP-6: the replacement is
// built inline before anything stops): build the stored set's image
// when missing, stop the old container, registry → stopped only after
// it verifies gone, start with args fully rebuilt from the current
// settings carrying the requested access variant, registry → running
// on a verified success. A switch whose build or start fails leaves
// the workspace stopped (a failed build stops nothing) with the error
// surfaced in the op log.
func (s *Server) handleWorkspaceAdopt(w http.ResponseWriter, r *http.Request) {
	ws, cname, refused := s.guardWorkspaceOp(w, r)
	if refused {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	variant := r.PostFormValue("variant")
	if !validVariant(variant) {
		http.Error(w, "invalid variant", http.StatusBadRequest)
		return
	}
	cfg, err := s.readSandboxCfg(ws.Path)
	if err != nil {
		refuseSidecar(w, err.Error())
		return
	}
	if err := validateStartCfg(cfg); err != nil {
		refuseSidecar(w, err.Error())
		return
	}
	if !cfg.Resolved {
		// The §10 unresolved refusal (WP-10): before any prompt
		// machinery — the old image/container is never touched.
		refuseSidecar(w, errUnresolvedSelection(cfg).Error())
		return
	}
	if !slices.Contains(discovery.RunningContainers(s.runnerSeam()), cname) {
		refuseSidecar(w, errNotRunning(cname).Error())
		return
	}
	running := s.runningImageRef(cname)
	if running == "" || s.setMismatchVerdict(cfg, running).Mismatch {
		// The mismatch switch (WP-6 re-key): "never stop without a
		// replacement" became "build the replacement, then stop" — the
		// inline Build now rides the op BEFORE anything stops; a failed
		// build or start leaves the workspace stopped with the error in
		// the busy pane (or untouched, when the build refuses).
	} else {
		// The web re-adopt (TUI access(): a live container matching the
		// stored set without a published 4096 port adopts web by
		// replacing it with a --start_web start). Nothing to replace
		// otherwise.
		if variant != "web" {
			refuseSidecar(w, "nothing to adopt — the running container matches the saved set")
			return
		}
		if port := discovery.WebPort(s.runnerSeam(), cname); port != "" {
			refuseSidecar(w, "OpenCode Web is already published at http://"+port)
			return
		}
	}
	s.queue.Launch(webOp{Key: cname, Run: s.adoptOp(ws, cfg, variant)})
	okSidecar(w, s.queue.CurrentOpID(cname))
}

// adoptOp is the verified build → stop → start replacement chain (TUI
// launchSwitch, WP-6 re-key): the stored set's image is built inline
// FIRST when missing (build-images.sh --set, the builder's preflight
// refusing offline hosts — a failed build stops nothing), then a bare
// `podman stop` (containers run --rm, gone = removed; start.sh's pod
// recreate-at-start rebuilds the unit), verified gone on a fresh ps
// before the registry flips to "stopped", then start.sh with args
// rebuilt from the CURRENT settings (sandbox_config.json) carrying the
// requested access variant. The registry flips to "running" only on a
// verified success; a failed start leaves the workspace stopped — no
// rollback (the previous image still exists, the user can switch back)
// — with the failure streamed into the op log.
func (s *Server) adoptOp(ws registry.Workspace, cfg sandboxcfg.Config, variant string) func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		cname := registry.ContainerName(ws.ContainerID)
		args := openworkspace.StartArgs(ws.Path, cfg, ws.ContainerID, variant)
		// 0. Build the replacement while the old container still runs
		// (WP-6: never stop without a replacement).
		if cfg.Resolved {
			if err := s.buildSetNow(cfg, emit); err != nil {
				emit("Switch refused — " + err.Error())
				return err
			}
		}
		// 1. Stop the old container, verify gone (fresh ps), registry
		// → stopped only after verification.
		out, err := d.Run.Output("podman", "stop", cname)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		if err != nil {
			return err
		}
		if slices.Contains(discovery.RunningContainers(d.Run), cname) {
			return errStillRunning(cname)
		}
		if err := d.Registry.UpdateStatus(ws.Path, "stopped"); err != nil {
			return err
		}
		emit("Registry status: stopped")
		// 2. Start with fully rebuilt args from the current settings.
		out, err = d.Run.Output("bash", append([]string{d.StartScript}, args...)...)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		if err != nil {
			emit("Switch failed — " + err.Error())
			return err
		}
		if !slices.Contains(discovery.RunningContainers(d.Run), cname) {
			err := errNotRunning(cname)
			emit("Switch failed — " + err.Error())
			return err
		}
		// 3. Registry → running on the verified success (opDone parity).
		if err := d.Registry.UpdateStatus(ws.Path, "running"); err != nil {
			return err
		}
		return d.Registry.UpdateLastUsed(ws.Path)
	}
}
