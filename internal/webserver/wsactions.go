// Start/access endpoints (#153, spec §9 Start/access): POST
// /workspaces/{name}/start runs the Container-center reuse semantics —
// a running container is reused, never restarted; a stopped one goes
// through start.sh with the stored StartArgs vector — as a queued op
// (queued-not-refused, one-at-a-time per workspace). The registry name
// is the URL identity; unknown names refuse with 404.
package webserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/openworkspace"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// runner is the command seam: production runs real commands.
func (s *Server) runner() cmdrun.Runner {
	if s.opts.Runner != nil {
		return s.opts.Runner
	}
	return cmdrun.ExecRunner{}
}

// startScript resolves the shipped start.sh path for the home
// (app.LayoutFor — the TUI invokes the same scripts, ADR-0007).
func (s *Server) startScript() string {
	return app.LayoutFor(s.opts.Home).StartScript
}

// lookupWorkspace resolves the URL-identity name to the registry entry.
func (s *Server) lookupWorkspace(name string) (registry.Workspace, error) {
	return (&registry.Registry{Home: s.opts.Home, Now: s.opts.Now}).ByName(name)
}

// handleStart serves POST /workspaces/{name}/start: enqueue the start
// op (queued-not-refused) and answer with the re-rendered card — the
// busy state arrives via the fragment plus the workspace SSE events.
func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.queue.Enqueue(ws.ContainerID, func(emit func(string)) error {
		return s.runStart(ws, emit)
	})
	s.renderCard(w, ws.Name)
}

// renderCard re-renders one workspace's card fragment (the POST swap
// target of the card actions).
func (s *Server) renderCard(w http.ResponseWriter, name string) {
	ws, err := s.lookupWorkspace(name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wsTemplate.ExecuteTemplate(w, "card", s.cardFor(ws, s.displayMode())); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// runStart is the start op: Container-center reuse semantics, mirrored
// from containerview — validate stored config, reuse running
// containers, else run start.sh detached; verify live on a fresh ps
// before any registry write.
func (s *Server) runStart(ws registry.Workspace, emit func(string)) error {
	run := s.runner()
	ctr := registry.ContainerName(ws.ContainerID)
	if hasCtr(discovery.RunningContainers(run), ctr) {
		emit("already running — reusing " + ctr)
	} else {
		cfg, err := sandboxcfg.Read(sandboxcfg.Path(ws.Path))
		if err != nil {
			return fmt.Errorf("cannot read sandbox_config.json for %s: %v", ws.Name, err)
		}
		if !openworkspace.ValidModes(cfg.ContainerModes) {
			return errors.New("offline and cbm_ui are mutually exclusive — fix settings first")
		}
		if !openworkspace.ValidSearchCombination(cfg.SearchMode, cfg.ContainerModes, cfg.UseProxy) {
			return errors.New("searxng search requires network and no proxy (ADR 0026) — fix settings first")
		}
		if !discovery.ImageExists(run, cfg.ContainerEdition) {
			return fmt.Errorf("image opencode-sandbox-%s is not built yet", cfg.ContainerEdition)
		}
		emit("==> " + ctr)
		args := openworkspace.StartArgs(ws.Path, cfg.ContainerEdition, ws.ContainerID,
			cfg.ContainerModes, cfg.StartOption, cfg.UseProxy, cfg.SearchMode)
		out, err := run.Output("bash", append([]string{s.startScript()}, args...)...)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		if err != nil {
			return fmt.Errorf("start.sh failed for %s: %v", ws.Name, err)
		}
	}
	// Verify before any write: every started container must be live on
	// a fresh podman ps (containerview parity).
	if !hasCtr(discovery.RunningContainers(run), ctr) {
		return fmt.Errorf("%s is not running", ctr)
	}
	reg := &registry.Registry{Home: s.opts.Home, Now: s.opts.Now}
	if err := reg.UpdateStatus(ws.Path, "running"); err != nil {
		return err
	}
	return reg.UpdateLastUsed(ws.Path)
}

// handlePairLink serves POST /workspaces/{name}/pair-link (§10): a
// fresh one-time sign-in link per request — nothing persisted, every
// access pairs afresh — as the {pair_link, plain_url} JSON sidecar.
// plain_url is the published host binding: the fallback when pairing
// fails or the v2 service is mid-boot.
func (s *Server) handlePairLink(w http.ResponseWriter, r *http.Request) {
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	run := s.runner()
	ctr := registry.ContainerName(ws.ContainerID)
	port := discovery.WebPort(run, ctr)
	var v pairLinkView
	if port != "" {
		v.PlainURL = "http://" + port
		v.PairLink = discovery.PairLink(run, ctr, "http://"+port)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(jsonfmtMarshal(v)))
}

// jsonDecode decodes a JSON sidecar body.
func jsonDecode(line string, v any) error {
	return json.NewDecoder(strings.NewReader(line)).Decode(v)
}

func jsonfmtMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// pairLinkView is the {pair_link, plain_url} JSON sidecar (§10): the
// fresh one-time sign-in link plus the published binding as the plain
// fallback when pairing fails or the v2 service is mid-boot.
type pairLinkView struct {
	PairLink string `json:"pair_link"`
	PlainURL string `json:"plain_url"`
}

// accessView is the POST …/access sidecar shape: opened tells whether a
// host terminal window could be spawned; command is the exact copyable
// command for the headless fallback (the session stays pending — no
// in-browser terminal, §3). Web carries the pair-link resolution.
type accessView struct {
	Variant  string `json:"variant"`
	Opened   bool   `json:"opened"`
	Command  string `json:"command"`
	PairLink string `json:"pair_link"`
	PlainURL string `json:"plain_url"`
}

// handleAccess serves POST /workspaces/{name}/access (§9) with
// variant=console|opencode|web: console/OpenCode spawn a host terminal
// window (§3 machinery) — or the headless fallback renders the exact
// copyable command and the session stays pending; web resolves the
// fresh pair-link sidecar (§10).
func (s *Server) handleAccess(w http.ResponseWriter, r *http.Request) {
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
	ctr := registry.ContainerName(ws.ContainerID)
	arg := "bash"
	if variant == "opencode" {
		arg = "opencode"
	}
	v := accessView{Variant: variant, Command: "podman exec -it --user dev " + ctr + " " + arg}
	switch variant {
	case "console", "opencode":
		if err := s.terminalSpawn()(v.Command); err != nil {
			v.Opened = false // headless fallback: copyable command, pending session
		} else {
			v.Opened = true
		}
	case "web":
		run := s.runner()
		port := discovery.WebPort(run, ctr)
		if port != "" {
			v.PlainURL = "http://" + port
			v.PairLink = discovery.PairLink(run, ctr, "http://"+port)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(jsonfmtMarshal(v)))
}

func hasCtr(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
