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
	"net/http"
	"slices"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/openworkspace"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// mismatchSidecar is the start route's edition-mismatch verdict (#154):
// the live container's image edition diverges from the saved one — the
// card offers Use current / Switch to <saved>, the switch behind the
// adopt confirm.
type mismatchSidecar struct {
	Mismatch       bool   `json:"mismatch"`
	RunningEdition string `json:"running_edition"`
	SavedEdition   string `json:"saved_edition"`
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

// handleWorkspaceStop serves POST /workspaces/{name}/stop (#154) — the
// existing graceful stop flow through the op runner (the pod-as-unit
// stop, verified gone before the registry flip): the same op the
// Container center's stop confirm launches, keyed on the workspace's
// container name.
func (s *Server) handleWorkspaceStop(w http.ResponseWriter, r *http.Request) {
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cname := registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(cname) {
		refuseSidecar(w, busyRefusal(cname))
		return
	}
	if s.setupHolds(ws.Name) { // the pending skills session holds the container (#161)
		refuseSidecar(w, "refused — first-run setup session pending for "+ws.Name)
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
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	cname := registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(cname) {
		refuseSidecar(w, busyRefusal(cname))
		return
	}
	if s.setupHolds(ws.Name) { // the pending skills session holds the container (#161)
		refuseSidecar(w, "refused — first-run setup session pending for "+ws.Name)
		return
	}
	if s.runningUnit(cname) {
		refuseSidecar(w, s.stopFirstHint(cname))
		return
	}
	s.queue.Launch(webOp{Key: cname, Run: s.deleteContainerOp(cname)})
	okSidecar(w, s.queue.CurrentOpID(cname))
}

// runningEdition reads the live container's edition off its image (the
// image is the truth about what runs, #115) — "" when it is not live
// or the image is not an edition image.
func (s *Server) runningEdition(cname string) string {
	for _, c := range discovery.RunningContainersWithImages(s.runnerSeam()) {
		if c.Name == cname {
			return discovery.EditionFromImage(c.Image)
		}
	}
	return ""
}

// handleWorkspaceAdopt serves POST /workspaces/{name}/adopt (#154) —
// behind the new web-native adopt confirm (§9): the edition-mismatch
// switch and the web re-adopt both replace a running container (an
// agent may be mid-task), so both confirm first. The switch runs the
// TUI's verified stop → start chain (launchSwitch semantics, mirrored
// exactly): stop the old container, registry → stopped only after it
// verifies gone, start with args fully rebuilt from the current
// settings carrying the requested access variant, registry → running
// on a verified success. A switch whose start fails leaves the
// workspace stopped with the error surfaced in the op log.
func (s *Server) handleWorkspaceAdopt(w http.ResponseWriter, r *http.Request) {
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	variant := r.PostFormValue("variant")
	if variant != "console" && variant != "opencode" && variant != "web" {
		http.Error(w, "invalid variant", http.StatusBadRequest)
		return
	}
	cname := registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(cname) {
		refuseSidecar(w, busyRefusal(cname))
		return
	}
	if s.setupHolds(ws.Name) { // the pending skills session holds the container (#161)
		refuseSidecar(w, "refused — first-run setup session pending for "+ws.Name)
		return
	}
	cfg, err := readSandboxCfg(ws.Path)
	if err != nil {
		refuseSidecar(w, err.Error())
		return
	}
	if err := validateStartCfg(cfg); err != nil {
		refuseSidecar(w, err.Error())
		return
	}
	if !slices.Contains(discovery.RunningContainers(s.runnerSeam()), cname) {
		refuseSidecar(w, errNotRunning(cname).Error())
		return
	}
	running := s.runningEdition(cname)
	if running != "" && running != cfg.ContainerEdition {
		// The mismatch switch (#115/#116): never destroy without a
		// replacement — the saved edition's image must exist before
		// anything stops (#117); the build rides Settings → Image.
		if !discovery.ImageExists(s.runnerSeam(), cfg.ContainerEdition) {
			refuseSidecar(w, errImageMissing(cfg.ContainerEdition).Error()+" — build it first (Settings → Image: Build); nothing was stopped")
			return
		}
	} else {
		// The web re-adopt (TUI access(): a live container without a
		// published 4096 port adopts web by replacing it with a
		// --start_web start). Nothing to replace otherwise.
		if variant != "web" {
			refuseSidecar(w, "nothing to adopt — the running container matches the saved edition")
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

// adoptOp is the verified stop → start replacement chain (TUI
// launchSwitch, mirrored exactly): a bare `podman stop` (containers run
// --rm, gone = removed; start.sh's pod recreate-at-start rebuilds the
// unit), verified gone on a fresh ps before the registry flips to
// "stopped", then start.sh with args rebuilt from the CURRENT settings
// (sandbox_config.json) carrying the requested access variant. The
// registry flips to "running" only on a verified success; a failed
// start leaves the workspace stopped — no rollback (the previous
// edition's image still exists, the user can switch back) — with the
// failure streamed into the op log.
func (s *Server) adoptOp(ws registry.Workspace, cfg sandboxcfg.Config, variant string) func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		cname := registry.ContainerName(ws.ContainerID)
		args := openworkspace.StartArgs(ws.Path, cfg.ContainerEdition, ws.ContainerID,
			cfg.ContainerModes, variant, cfg.UseProxy, cfg.SearchMode)
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
