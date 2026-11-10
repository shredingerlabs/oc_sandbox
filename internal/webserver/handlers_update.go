package webserver

import (
	"html/template"
	"net/http"
)

// Settings → Update screen handlers (spec §7/§9): HTMX-first HTML
// fragments. GET /update renders the card from the updater state;
// POST /update/check re-runs the check; POST /update/run starts the
// exclusive run and answers at once — the phase events ride /events, so
// the POSTs never block on the op.

// keepNote is the preservation fact the popup copy carries (§12) —
// mirrors settingsview's keepNote so the two never drift apart.
const keepNote = "Kept: proxy/allowlist.txt, dist/Dockerfile.custom, dist/searxng/settings.yml, your configs."

// confirmTitle/confirmBody are the shared confirm popup's copy for the
// update run (ADR-0021 set carried 1:1; the popup carries the keep-note).
const (
	confirmTitle = "Update oc-sandbox?"
	confirmBody  = "This runs the install script for the latest release.\n" + keepNote
)

func (s *Server) handleUpdateCard(w http.ResponseWriter, r *http.Request) {
	writeUpdateCard(w, s.updater.cardState())
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	s.updater.Check()
	writeUpdateCard(w, s.updater.cardState())
}

func (s *Server) handleUpdateRun(w http.ResponseWriter, r *http.Request) {
	s.updater.Run()
	writeUpdateCard(w, s.updater.cardState())
}

func writeUpdateCard(w http.ResponseWriter, st updateState) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := updateCardTmpl.Execute(w, st); err != nil {
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

// latestRow renders the "Latest version" row: not checked yet, check
// failed, or the tag.
func latestRow(st updateState) string {
	switch {
	case !st.Checked:
		return "Latest version: checking…"
	case st.CheckFailed:
		return "Latest version: not available (check failed)"
	case st.Latest == "":
		return "Latest version: not available (check failed)"
	default:
		return "Latest version: " + st.Latest
	}
}

// verdictRow renders the phase verdict line.
func verdictRow(st updateState) string {
	switch st.Phase {
	case PhaseAvailable:
		return "Update available!"
	case PhaseUpToDate:
		return "You are up to date."
	case PhaseInstalling:
		return "Updating…"
	}
	return ""
}

// inRun reports whether the phase belongs to (or follows) a run — the
// log pane stays visible then, its content streaming via line events.
func inRun(phase string) bool {
	return phase == PhaseInstalling || phase == PhaseApplied || phase == PhaseFailed
}

// updateCardTmpl renders the Update card fragment. The verdict rows are
// the phase rendering (§12): purely from the updater state — the same
// state the SSE `update` event carries.
var updateCardTmpl = template.Must(template.New("updatecard").
	Funcs(template.FuncMap{
		"latestRow":    latestRow,
		"verdictRow":   verdictRow,
		"inRun":        inRun,
		"keepNote":     func() string { return keepNote },
		"confirmTitle": func() string { return confirmTitle },
		"confirmBody":  func() string { return confirmBody },
	}).
	Parse(
		`<section class="update-card" data-role="update-card">
  <h2>Update</h2>
  <div class="rows">
    <div class="row">Installed version: {{.Installed}}</div>
    <div class="row" data-role="update-latest">{{latestRow .}}</div>
    <div class="row verdict" data-role="update-verdict" data-phase="{{.Phase}}">{{verdictRow .}}</div>
    <div class="row warn" data-role="update-error"{{if not .RunErr}} hidden{{end}}>{{if .RunErr}}Update failed: {{.RunErr}}{{end}}</div>
    <div class="row note" data-role="update-note"{{if not .Updated}} hidden{{end}}>Update installed — restart oc-sandbox to apply.</div>
    <div class="row muted">The update runs the install script for the latest release.</div>
    <div class="row muted">{{keepNote}}</div>
  </div>
  <div class="actions">
    <button class="btn" data-role="update-check" hx-post="update/check" hx-target="closest section" hx-swap="outerHTML">Check now</button>
    <button class="btn primary" data-role="update-run"{{if eq .Phase "installing"}} disabled{{end}}
      data-confirm-title="{{confirmTitle}}" data-confirm-body="{{confirmBody}}">Update</button>
  </div>
  <pre class="logpane" data-role="update-log"{{if not (inRun .Phase)}} hidden{{end}}></pre>
</section>
`))
