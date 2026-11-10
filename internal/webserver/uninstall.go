package webserver

// Settings → Uninstall (spec §7/§9): wizard toggles mapped onto
// uninstall.sh flags exactly like the TUI maps them, the typed
// `uninstall` text confirmation, and the confirm popup before anything
// runs. The script is invoked as the TUI invokes it (bash <script>
// <flags>, always with --force) and runs as one task-manager op — its
// output streams over the SSE line events.

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
// The typed input arms the submit client-side (main.js) — the server
// re-validates it on POST.
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
<pre class="op-log" data-role="op-log" hidden></pre>
</form></div>`))

// uninstallDoneTmpl reports the queued/finished state of the script run.
var uninstallDoneTmpl = template.Must(template.New("uninstalldone").Parse(
	`<div class="panel"><div class="notice ok">Uninstall started — the script output streams in the log.</div>
<button class="btn ghost" hx-get="uninstall" hx-target="[data-role='screen']" hx-swap="innerHTML">Back</button></div>`))

// uninstallConfirmFailedTmpl is the typed-confirmation refusal.
var uninstallConfirmFailedTmpl = template.Must(template.New("uninstallconfirm").Parse(
	`<div class="panel"><div class="notice bad">Type "uninstall" to confirm.</div>
<button class="btn ghost" hx-get="uninstall" hx-target="[data-role='screen']" hx-swap="innerHTML">Back</button></div>`))

// uninstallArmed reports whether the typed confirmation unlocks the
// button; case-insensitive, surrounding whitespace ignored (TUI parity).
func uninstallArmed(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "uninstall") }

// handleUninstallScreen renders the Uninstall screen (GET /uninstall).
func (s *Server) handleUninstallScreen(w http.ResponseWriter, r *http.Request) {
	running := discovery.RunningContainers(s.runner)
	renderT(w, uninstallTmpl, struct{ Running []string }{Running: running})
}

// handleUninstall validates the typed confirmation, refuses while busy,
// maps the toggles onto the script flags and runs uninstall.sh as one
// queue op (POST /uninstall). Flags exactly like the TUI's runUninstall:
// always --force; keeping the symlinks adds --no-symlinks; the other two
// are opt-in.
func (s *Server) handleUninstall(w http.ResponseWriter, r *http.Request) {
	if !uninstallArmed(r.FormValue("confirm_text")) {
		renderT(w, uninstallConfirmFailedTmpl, nil)
		return
	}
	if s.busy() {
		renderT(w, busyNoticeTmpl, nil)
		return
	}
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
	runner := s.runner
	s.queue.Launch(webOp{
		Key: "settings", // config-file ops serialize (TUI parity)
		Run: func(emit func(string)) error {
			out, err := runner.Output("bash", append([]string{script}, args...)...)
			for _, line := range cmdrun.Lines(out) {
				emit(line)
			}
			return err
		},
		Done: func(err error) {
			s.hub.PublishWorkspace() // busy state changed — tabs re-render
		},
	})
	renderT(w, uninstallDoneTmpl, nil)
}
