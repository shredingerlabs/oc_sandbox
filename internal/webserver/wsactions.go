// Workspace access verbs (#153, spec §9 Start/access): POST
// /workspaces/{name}/access (console|opencode|web) and POST
// /workspaces/{name}/pair-link. The start verb lives in containers.go
// (#158's Container-center route — the same URL the Open action posts
// to); the registry name is the URL identity; unknown names refuse
// with 404.
package webserver

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

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

// accessRunner is the command seam of the access verbs: the Options
// runner overrides #156's deps-first resolution (tests inject fakes).
func (s *Server) accessRunner() cmdrun.Runner {
	if s.opts.Runner != nil {
		return s.opts.Runner
	}
	return s.runner()
}

// startScript resolves the shipped start.sh path for the home (the TUI
// invokes the same scripts, ADR-0007).
func (s *Server) startScript() string {
	if s.deps != nil && s.deps.StartScript != "" {
		return s.deps.StartScript
	}
	return app.LayoutFor(s.opts.Home).StartScript
}

// lookupWorkspace resolves the URL-identity name to the registry entry.
func (s *Server) lookupWorkspace(name string) (registry.Workspace, error) {
	if s.deps != nil && s.deps.Registry != nil {
		return s.deps.Registry.ByName(name)
	}
	return (&registry.Registry{Home: s.opts.Home, Now: s.opts.Now}).ByName(name)
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
	run := s.accessRunner()
	ctr := registry.ContainerName(ws.ContainerID)
	port := discovery.WebPort(run, ctr)
	var v pairLinkView
	if port != "" {
		v.PlainURL = "http://" + port
		v.PairLink = discovery.PairLink(run, ctr, "http://"+port)
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(jsonMarshal(v)))
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
		run := s.accessRunner()
		port := discovery.WebPort(run, ctr)
		if port != "" {
			v.PlainURL = "http://" + port
			v.PairLink = discovery.PairLink(run, ctr, "http://"+port)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(jsonMarshal(v)))
}

func jsonMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// jsonDecode decodes a JSON sidecar body.
func jsonDecode(line string, v any) error {
	return json.NewDecoder(strings.NewReader(line)).Decode(v)
}

// CurrentOpID returns the op id of the key's in-flight (running or
// first queued) op, 0 when idle — the id busy cards carry so streamed
// line events find their log across re-renders (#153).
func (r *opRunner) CurrentOpID(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if op, busy := r.running[key]; busy {
		return op.ID
	}
	for _, p := range r.pending {
		if p.Key == key {
			return p.ID
		}
	}
	return 0
}
