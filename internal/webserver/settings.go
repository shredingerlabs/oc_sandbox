// Settings endpoints (spec §9): GET/POST /settings carry the global
// settings — display mode + default workspace path — persisted in
// global_config.json, the single truth, read per request. HTMX-first
// hybrid: the display-mode toggle is a small JSON sidecar (plain fetch
// is simpler here); screens re-render reactively on the client, no
// reload. POST accepts display_mode (standard/advanced); unknown values
// are refused with 400 and never touch the stored file.
package webserver

import (
	"net/http"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/jsonfmt"
)

// settingsView is the GET /settings sidecar: the global config's
// WebUI-relevant keys. An unreadable global_config.json falls back to
// the defaults (standard mode, empty path) — the UI must not dead-end.
type settingsView struct {
	DisplayMode          string `json:"display_mode"`
	DefaultWorkspacePath string `json:"default_workspace_path"`
}

func (s *Server) currentSettings() settingsView {
	v := settingsView{DisplayMode: config.DisplayStandard}
	if s.opts.Home == "" {
		return v
	}
	gc, err := config.ReadGlobalConfig(s.opts.Home)
	if err != nil {
		return v
	}
	return settingsView{DisplayMode: gc.DisplayMode, DefaultWorkspacePath: gc.DefaultWorkspacePath}
}

func writeJSON(w http.ResponseWriter, v settingsView) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(jsonfmt.Marshal(v))) // jq-style, trailing newline
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.currentSettings())
}

func (s *Server) handleSettingsPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	mode := r.PostFormValue("display_mode")
	if !config.ValidDisplayMode(mode) {
		http.Error(w, "invalid display_mode", http.StatusBadRequest)
		return
	}
	if err := config.SetDisplayMode(s.opts.Home, mode); err != nil {
		http.Error(w, "persist failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.currentSettings())
}
