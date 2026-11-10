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

func jsonfmtMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func hasCtr(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
