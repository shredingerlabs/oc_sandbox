package webserver

import (
	"html"
	"net/http"
	"slices"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// containers.go serves the Container center (spec §7/#158): one
// container list with the three actions — start (Container-center
// reuse semantics, no confirm), stop and delete behind the popup
// confirms (delete the danger one). The registry workspace name is the
// URL identity (§9); leftover pods/containers of gone workspaces list
// as delete-only rows (pod-as-unit, like the TUI delete tab).

// wireRoutes registers the screen routes on the token-validated inner
// mux (the auth middleware stripped /t/<token>/ already).
func (s *Server) wireRoutes(inner *http.ServeMux) {
	inner.HandleFunc("GET /containers", s.handleContainers)
	inner.HandleFunc("POST /containers/{name}/stop", s.handleContainerStop)
	inner.HandleFunc("POST /containers/{name}/delete", s.handleContainerDelete)
	inner.HandleFunc("POST /workspaces/{name}/start", s.handleWorkspaceStart)
	inner.HandleFunc("POST /workspaces/{name}/stop", s.handleWorkspaceStop)
	inner.HandleFunc("POST /workspaces/{name}/delete", s.handleWorkspaceDelete)
	inner.HandleFunc("POST /workspaces/{name}/adopt", s.handleWorkspaceAdopt)
	inner.HandleFunc("GET /images", s.handleImages)
	inner.HandleFunc("POST /images/build", s.handleImagesBuild)
	inner.HandleFunc("POST /images/delete", s.handleImagesDelete)
}

// unitRow is one row of the container list: a registry workspace's
// container (or its pod — the pod row stands for the pod as a unit) or
// a leftover pod/container of a gone workspace.
type unitRow struct {
	Workspace string // registry name ("" for leftovers)
	Unit      string // container or pod name
	Pod       bool
	State     string // running / exited / created / missing
	Busy      bool
}

// containerRows builds the one container list fresh per request (data
// map §3): registered workspaces first (registry order), then leftover
// pods and containers that resolve to no registry workspace. Pods are
// the unit wherever a pod exists (pod-as-unit; members ride its row).
func (s *Server) containerRows() []unitRow {
	d := s.deps
	podState := map[string]string{}
	var allPods []discovery.PodState
	for _, p := range discovery.AllPods(d.Run) {
		allPods = append(allPods, p)
		podState[p.Name] = p.State
	}
	containerState := map[string]string{}
	for _, c := range discovery.AllContainers(d.Run) {
		containerState[c.Name] = c.State
	}

	rows := []unitRow{}
	registryPods := map[string]bool{}
	registryContainers := map[string]bool{}
	workspaces, err := d.Registry.AllOrdered()
	if err != nil {
		workspaces = nil // no workspaces.json yet: a clean empty list
	}
	for _, p := range workspaces {
		cname := registry.ContainerName(p.ContainerID)
		podName := registry.PodName(p.ContainerID)
		state, isPod := "missing", false
		if st, ok := podState[podName]; ok {
			state, isPod = st, true
		} else if st, ok := containerState[cname]; ok {
			state = st
		}
		if isPod {
			registryPods[podName] = true
		} else {
			registryContainers[cname] = true
		}
		rows = append(rows, unitRow{
			Workspace: p.Name,
			Unit:      cname,
			Pod:       isPod,
			State:     state,
			Busy:      s.queue.BusyKey(cname),
		})
	}
	// leftovers: pods as units (their members ride the pod row),
	// pod-less containers in any state — delete-only rows.
	for _, pod := range allPods {
		if registryPods[pod.Name] {
			continue
		}
		rows = append(rows, unitRow{Unit: pod.Name, Pod: true, State: podState[pod.Name]})
	}
	for _, c := range discovery.AllContainers(d.Run) {
		if registry.IsPodName(c.Name) || podState[registry.PodName(registry.ContainerID(c.Name))] != "" {
			continue // infra container, or a pod member (pod-as-unit)
		}
		if registryContainers[c.Name] {
			continue
		}
		rows = append(rows, unitRow{Unit: c.Name, State: containerState[c.Name]})
	}
	return rows
}

// stateMarker renders a unit's live state: running in the on-color,
// anything else muted — the distinction never rides on color alone.
func stateMarker(state string) string {
	if state == "running" {
		return `<span class="marker st-running">● running</span>`
	}
	return `<span class="marker st-off">○ ` + html.EscapeString(state) + `</span>`
}

// busyMarker renders the queue-state marker of a busy row.
func busyMarker() string {
	return `<span class="marker st-busy">◌ op running</span>`
}

// renderContainers renders the one container list (spec §7) with the
// three actions per registry row and the delete-only leftovers. Toast
// (a refused confirm) rides at the top when msg is non-empty.
func (s *Server) renderContainers(msg string) string {
	var b strings.Builder
	b.WriteString(`<section class="card" data-screen="containers">`)
	b.WriteString(`<div class="card-head"><h2>Containers</h2></div>`)
	if msg != "" {
		b.WriteString(`<div class="inline-toast" data-toast>` + html.EscapeString(msg) + `</div>`)
	}
	b.WriteString(`<div class="units">`)
	for _, row := range s.containerRows() {
		b.WriteString(`<div class="unit" data-unit="` + html.EscapeString(row.Unit) + `"`)
		if row.Busy {
			b.WriteString(` data-busy`)
		}
		b.WriteString(`>`)
		b.WriteString(`<div class="unit-main">`)
		if row.Workspace != "" {
			b.WriteString(`<span class="unit-name">` + html.EscapeString(row.Workspace) + `</span>`)
		} else {
			b.WriteString(`<span class="unit-name muted">leftover</span>`)
		}
		b.WriteString(`<span class="unit-sub">` + html.EscapeString(row.Unit) + `</span>`)
		b.WriteString(stateMarker(row.State))
		if row.Busy {
			b.WriteString(busyMarker())
		}
		b.WriteString(`</div><div class="actions">`)
		if row.Workspace != "" {
			b.WriteString(`<button data-action="start" data-href="workspaces/` +
				html.EscapeString(row.Workspace) + `/start">Start</button>`)
		}
		if !row.Busy {
			b.WriteString(`<button data-action="stop" data-href="containers/` +
				html.EscapeString(row.Unit) + `/stop" data-confirm="Stop ` +
				html.EscapeString(row.Unit) + `?"` + disabledAttr(row.State, row.Workspace) + `>Stop</button>`)
			b.WriteString(`<button data-action="delete" data-href="containers/` +
				html.EscapeString(row.Unit) + `/delete" data-confirm="Delete ` +
				html.EscapeString(row.Unit) + `?" data-danger` +
				disabledAttr(row.State, row.Workspace) + `>Delete</button>`)
		}
		b.WriteString(`</div></div>`)
	}
	if len(s.containerRows()) == 0 {
		b.WriteString(`<div class="empty">no oc-sandbox containers</div>`)
	}
	b.WriteString(`</div></section>`)
	return b.String()
}

// disabledAttr greys the stop button of a unit that is not running
// (stop acts on live units only; delete acts on any stopped one).
func disabledAttr(state, workspace string) string {
	if workspace != "" && state != "running" {
		return ` disabled`
	}
	return ""
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(s.renderContainers("")))
}

// handleContainerStop serves POST /containers/{name}/stop — the
// confirm's action. The confirm refuses while an op is queued or
// running for the unit (spec §9); otherwise the stop op launches.
func (s *Server) handleContainerStop(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.queue.BusyKey(name) {
		_, _ = w.Write([]byte(s.renderContainers("an operation is already queued or running for " + name)))
		return
	}
	s.queue.Launch(webOp{Key: name, Run: s.stopOp(name)})
	_, _ = w.Write([]byte(s.renderContainers("")))
}

// handleContainerDelete serves POST /containers/{name}/delete — the
// danger confirm's action. Running units are refused from the fresh
// podman state with the stop-first hint (TUI parity: deleting a
// running container would be an ungraceful kill; the op re-checks), a
// busy key refuses the confirm; otherwise the delete op launches.
func (s *Server) handleContainerDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if s.queue.BusyKey(name) {
		_, _ = w.Write([]byte(s.renderContainers("an operation is already queued or running for " + name)))
		return
	}
	if s.runningUnit(name) {
		_, _ = w.Write([]byte(s.renderContainers(s.stopFirstHint(name))))
		return
	}
	s.queue.Launch(webOp{Key: name, Run: s.deleteContainerOp(name)})
	_, _ = w.Write([]byte(s.renderContainers("")))
}

// runningUnit reports whether the named pod or container is live right
// now (fresh podman state, the confirm-time guard).
func (s *Server) runningUnit(name string) bool {
	if registry.IsPodName(name) {
		for _, p := range discovery.AllPods(s.deps.Run) {
			if p.Name == name && p.State == "running" {
				return true
			}
		}
		return false
	}
	return slices.Contains(discovery.RunningContainers(s.deps.Run), name)
}

// stopFirstHint is the running-unit refusal: pods and containers each
// carry their own stop hint (TUI uninstall parity).
func (s *Server) stopFirstHint(name string) string {
	if registry.IsPodName(name) {
		return errStopFirstPod(name).Error()
	}
	return errStopFirst(name).Error()
}

// handleWorkspaceStart serves POST /workspaces/{name}/start — the
// Container-center reuse semantics: no confirm, queued per workspace
// key (the workspace's container name — the key the stop/delete ops
// share, so confirms refuse across all of a workspace's actions).
// With a variant form field (#154, the Open chain's card start) the
// route answers a JSON sidecar and runs the mismatch detection the TUI
// reconcile does (#115): a live container whose image edition diverges
// from the saved one refuses the silent reuse with a {mismatch} sidecar
// — the registry flips truthful first (the container IS running,
// verified live) and last_used bumps (the Open action is a use); the
// replacement itself is the adopt confirm's move. A first-run setup
// session holds the workspace under the one-at-a-time rule (#161) —
// both paths refuse while it holds.
func (s *Server) handleWorkspaceStart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	ws, err := s.deps.Registry.ByName(name)
	if err != nil {
		_, _ = w.Write([]byte(s.renderContainers("workspace " + name + " is not registered")))
		return
	}
	if s.setupHolds(name) {
		http.Error(w, "refused — first-run setup session pending for "+name, http.StatusConflict)
		return
	}
	if variant := r.PostFormValue("variant"); variant != "" {
		if variant != "console" && variant != "opencode" && variant != "web" {
			http.Error(w, "invalid variant", http.StatusBadRequest)
			return
		}
		cname := registry.ContainerName(ws.ContainerID)
		if slices.Contains(discovery.RunningContainers(s.deps.Run), cname) {
			// Truthfulness first (#115 reconcile parity): the live
			// status rides the fresh ps before any verdict, and the
			// Open action is a use.
			_ = s.deps.Registry.UpdateStatus(ws.Path, "running")
			_ = s.deps.Registry.UpdateLastUsed(ws.Path)
			if cfg, cfgErr := readSandboxCfg(ws.Path); cfgErr == nil {
				if running := s.runningEdition(cname); running != "" &&
					running != cfg.ContainerEdition {
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(jsonMarshal(mismatchSidecar{
						Mismatch:       true,
						RunningEdition: running,
						SavedEdition:   cfg.ContainerEdition,
					})))
					return
				}
			}
			// Agreeing editions (or an unreadable config): the queued
			// reuse op re-verifies the live container.
		}
		s.queue.Launch(webOp{Key: cname, Run: s.startOp(name)})
		okSidecar(w, s.queue.CurrentOpID(cname))
		return
	}
	s.queue.Launch(webOp{Key: registry.ContainerName(ws.ContainerID), Run: s.startOp(name)})
	_, _ = w.Write([]byte(s.renderContainers("")))
}

// startOp mirrors the TUI Container-center start (containerview
// startStart, one workspace): already-running containers are reused,
// the rest run start.sh with the values from the workspace's
// sandbox_config.json (openworkspace.StartArgs, --detach); the registry
// receives "running" only after the container is verified live against
// a fresh podman ps.
func (s *Server) startOp(name string) func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		ws, err := d.Registry.ByName(name)
		if err != nil {
			return err
		}
		cname := registry.ContainerName(ws.ContainerID)
		if slices.Contains(discovery.RunningContainers(d.Run), cname) {
			emit("already running — reusing " + cname)
		} else {
			cfg, err := readSandboxCfg(ws.Path)
			if err != nil {
				return err
			}
			if err := validateStartCfg(cfg); err != nil {
				return err
			}
			if !discovery.ImageExists(d.Run, cfg.ContainerEdition) {
				return errImageMissing(cfg.ContainerEdition)
			}
			emit("==> " + cname)
			args := startScriptArgs(ws.Path, ws.ContainerID, cfg)
			out, err := d.Run.Output("bash", append([]string{d.StartScript}, args...)...)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return err
			}
		}
		// verified live before any write (data map §3).
		if !slices.Contains(discovery.RunningContainers(d.Run), cname) {
			return errNotRunning(cname)
		}
		if err := d.Registry.UpdateStatus(ws.Path, "running"); err != nil {
			return err
		}
		// The Open action is a use: the registry's last_used updates (#153).
		return d.Registry.UpdateLastUsed(ws.Path)
	}
}

// stopOp mirrors the TUI Container-center stop (containerview
// startStop, one unit): a container running inside a pod takes its
// companions down with it — stop the pod as the unit (podman pod stop
// when the workspace's pod exists, podman stop alone for pre-upgrade
// containers without a pod); container_status="stopped" only after the
// container is verified gone.
func (s *Server) stopOp(name string) func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		pod := registry.PodName(registry.ContainerID(name))
		if discovery.PodExists(d.Run, pod) {
			out, err := d.Run.Output("podman", "pod", "stop", pod)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return err
			}
		} else {
			out, err := d.Run.Output("podman", "stop", name)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return err
			}
		}
		// verified gone before any write (data map §3).
		if slices.Contains(discovery.RunningContainers(d.Run), name) {
			return errStillRunning(name)
		}
		return s.mirrorStopped(name)
	}
}

// deleteContainerOp mirrors the TUI Container-center delete
// (containerview launchDelete, one unit): never force-killing a
// running unit (stop-first refusal, the pre-op guard re-checks),
// podman pod rm / podman rm, verified gone before any registry touch.
func (s *Server) deleteContainerOp(name string) func(emit func(string)) error {
	d := s.deps
	return func(emit func(string)) error {
		if registry.IsPodName(name) {
			for _, p := range discovery.AllPods(d.Run) {
				if p.Name == name && p.State == "running" {
					return errStopFirstPod(name)
				}
			}
		} else if slices.Contains(discovery.RunningContainers(d.Run), name) {
			return errStopFirst(name)
		}
		if registry.IsPodName(name) {
			out, err := d.Run.Output("podman", "pod", "rm", name)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return err
			}
			if slices.ContainsFunc(discovery.AllPods(d.Run), func(p discovery.PodState) bool { return p.Name == name }) {
				return errStillExists(name)
			}
		} else {
			out, err := d.Run.Output("podman", "rm", name)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return err
			}
			if slices.ContainsFunc(discovery.AllContainers(d.Run), func(c discovery.ContainerState) bool { return c.Name == name }) {
				return errStillExists(name)
			}
		}
		return s.mirrorStopped(name)
	}
}

// mirrorStopped reconciles the registry after a verified removal/stop:
// the container_status flips to "stopped" — mirroring a stale registry
// status (TUI parity); leftovers of gone workspaces have nothing to
// mirror.
func (s *Server) mirrorStopped(name string) error {
	ws, err := s.deps.Registry.ByID(registry.ContainerID(name))
	if err != nil {
		return nil // not a registry workspace
	}
	if ws.ContainerStatus == "stopped" {
		return nil
	}
	return s.deps.Registry.UpdateStatus(ws.Path, "stopped")
}
