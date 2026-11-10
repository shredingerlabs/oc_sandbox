package webserver

// Settings → Uninstall (spec §7/§9): wizard toggles mapped onto
// uninstall.sh flags exactly like the TUI maps them, the typed
// `uninstall` text confirmation, and the confirm popup before anything
// runs. The script is invoked as the TUI invokes it (bash <script>
// <flags>, always with --force) and runs as one settings-key queue op —
// its output streams over the SSE line events into the embedded log
// pane (#158 log-pane convention: data-op + JS-side line buffering).

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
)

// uninstallTmpl renders the Uninstall screen: the warning block (running
// containers fresh per render, with their stop commands), the three
// option toggles, the typed confirmation and the popup-confirmed button.
// The typed input arms the submit client-side (main.js data-arms) — the
// server re-validates it on POST. opID wires the log pane the run
// streams into (0 = no pane).
var uninstallTmpl = template.Must(template.New("uninstall").Parse(
	`<div class="pagehead"><div><h2>Uninstall oc-sandbox</h2>
<div class="desc">Removes the oc-sandbox installation. Workspace roots are never touched.</div></div></div>
{{if .Running}}<div class="panel">
<div class="notice warn">Running containers:</div>
<div class="rows">{{range .Running}}
<div class="cfgrow"><span class="cfgname mono">{{.}}</span><span class="hint">stop it first: podman stop {{.}}</span></div>
{{end}}</div></div>
{{else}}<div class="panel"><div class="hint">No running containers detected.</div></div>
{{end}}
{{if .Notice}}<div class="notice ok">{{.Notice}}</div>{{end}}
<div class="panel dangerzone">
<div class="notice warn">This removes the oc-sandbox installation.</div>
<form hx-post="uninstall" hx-target="[data-role='screen']" hx-swap="innerHTML">
<div class="optrow"><label><input type="checkbox" name="remove_config"> Remove config (backup is made first)</label></div>
<div class="optrow"><label><input type="checkbox" name="remove_symlinks"> Remove symlinks</label></div>
<div class="optrow"><label><input type="checkbox" name="remove_shortcuts"> Remove desktop shortcuts</label></div>
<label class="flabel" for="uninstall-confirm">Type "uninstall" to confirm</label>
<input id="uninstall-confirm" class="field" type="text" name="confirm_text" autocomplete="off" data-arms="uninstall-btn">
<button id="uninstall-btn" class="btn danger" type="submit" disabled
 data-confirm-popup data-confirm-title="Uninstall oc-sandbox?"
 data-confirm-question="This removes images, symlinks, shortcuts and (if selected) the config — a config backup is made first. This cannot be undone." data-confirm-label="Uninstall">Uninstall</button>
</form>
{{if .OpID}}<pre class="logpane" data-role="log" data-op="{{.OpID}}"></pre>{{end}}
</div>`))

// uninstallConfirmFailedTmpl is the typed-confirmation refusal.
var uninstallConfirmFailedTmpl = template.Must(template.New("uninstallconfirm").Parse(
	`<div class="panel"><div class="notice bad">Type "uninstall" to confirm.</div>
<button class="btn ghost" hx-get="uninstall" hx-target="[data-role='screen']" hx-swap="innerHTML">Back</button></div>`))

// uninstallArmed reports whether the typed confirmation unlocks the
// button; case-insensitive, surrounding whitespace ignored (TUI parity).
func uninstallArmed(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "uninstall") }

// handleUninstallScreen renders the Uninstall screen (GET /uninstall).
func (s *Server) handleUninstallScreen(w http.ResponseWriter, r *http.Request) {
	s.renderUninstall(w, "")
}

// renderUninstall renders the screen with an optional result notice and
// the log pane of the last uninstall op.
func (s *Server) renderUninstall(w http.ResponseWriter, notice string) {
	running := discovery.RunningContainers(s.runnerSeam())
	renderT(w, uninstallTmpl, struct {
		Running []string
		Notice  string
		OpID    int
	}{
		Running: running,
		Notice:  notice,
		OpID:    s.lastUninstallOp,
	})
}

// handleUninstall validates the typed confirmation, refuses while busy,
// maps the toggles onto the script flags and runs uninstall.sh as one
// settings-key queue op (POST /uninstall). Flags exactly like the TUI's
// runUninstall: always --force; keeping the symlinks adds --no-symlinks;
// the other two are opt-in.
func (s *Server) handleUninstall(w http.ResponseWriter, r *http.Request) {
	if !uninstallArmed(r.FormValue("confirm_text")) {
		renderT(w, uninstallConfirmFailedTmpl, nil)
		return
	}
	if s.busy() {
		s.renderUninstall(w, "Another operation is queued or running — try again when it finishes.")
		return
	}
	// Flag mapping exactly like the TUI's runUninstall: always --force;
	// keeping the symlinks adds --no-symlinks; the other two are opt-in.
	args := []string{"--force"}
	if r.FormValue("remove_symlinks") == "" {
		args = append(args, "--no-symlinks")
	}
	if r.FormValue("remove_shortcuts") != "" {
		args = append(args, "--remove-shortcuts")
	}
	if r.FormValue("remove_config") != "" {
		args = append(args, "--remove-config")
	}
	script := s.uninstallScript()
	runner := s.runnerSeam()
	id := s.queue.reserve()
	s.lastUninstallOp = id
	s.queue.Launch(webOp{
		ID:  id,
		Key: "settings", // config-file ops serialize (TUI parity)
		Run: func(emit func(string)) error {
			out, err := runner.Output("bash", append([]string{script}, args...)...)
			for _, line := range cmdrun.Lines(out) {
				emit(line)
			}
			return err
		},
	})
	s.renderUninstall(w, "Uninstall started — the script output streams in the log pane.")
}
