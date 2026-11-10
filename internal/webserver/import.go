package webserver

// Settings → Import Workspace screen (spec §7/§9): a path input with
// server-side validation. Matching roots get the ordinary import summary
// flow (register-only, nothing moved); legacy project/-layout folders get
// the informational prompt offering the New Workspace wizard plus the
// explicit "Convert to workspace layout" action (the same per-root
// converter the first-start migration runs — never a silent transform);
// broken configs get the repair prompt; registered paths hit the
// registered guard (ADR 0017).
//
// The screen is one HTMX flow: the check POST returns the next stage's
// fragment, which swaps into the screen slot and posts back here.

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/importcheck"
	"github.com/shredingerlabs/oc-sandbox/internal/migration"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// fragmentFuncs are the template funcs the screen fragments share.
var fragmentFuncs = template.FuncMap{
	// backBtn is the shared Back action: re-render the import screen.
	"backBtn": func() template.HTML {
		return `<button class="btn ghost" hx-get="import" hx-target="[data-role='screen']" hx-swap="innerHTML">Back</button>`
	},
	// backBtnConfig re-renders the Export/Restore screen.
	"backBtnConfig": func() template.HTML {
		return `<button class="btn ghost" hx-get="config" hx-target="[data-role='screen']" hx-swap="innerHTML">Back</button>`
	},
	"joinModes": func(modes []string) string { return strings.Join(modes, ", ") },
}

// importScreenTmpl renders the path-input screen.
var importScreenTmpl = template.Must(template.New("import").Parse(
	`<div class="pagehead"><div><h2>Import a workspace</h2>
<div class="desc">Registers an existing sandbox workspace root — a strict structure check; nothing is moved or transformed.</div></div></div>
<form class="panel" hx-post="import-workspace" hx-target="[data-role='screen']" hx-swap="innerHTML">
<label class="flabel" for="import-path">Workspace folder path</label>
<input id="import-path" class="field" type="text" name="path" required placeholder="/home/you/workspaces/my-root">
<div class="rowacts"><button class="btn" type="submit">Import Workspace</button></div>
</form>`))

// importStageData shapes the check outcome for importStageTmpl.
type importStageData struct {
	Guard       bool
	Broken      bool
	Legacy      bool
	NonMatching bool
	Path        string
	Name        string
	Config      sandboxcfg.Config
}

// importStageTmpl renders the outcome of the server-side check: guard /
// repair / referrer / legacy prompts or the registration summary.
var importStageTmpl = template.Must(template.New("importstage").Funcs(fragmentFuncs).Parse(
	`<div class="pagehead"><div><h2>Import a workspace</h2></div></div>
{{if .Guard}}<div class="panel">
<div class="notice warn">Already registered — this path is a registered workspace.</div>
{{backBtn}}</div>
{{else if .Broken}}<div class="panel">
<div class="notice bad">Config is broken — sandbox_config.json is not valid JSON.</div>
<div class="hint">Repair it before importing.</div>
{{backBtn}}</div>
{{else if .Legacy}}<div class="panel">
<div class="notice warn">Import isn't possible here</div>
<div class="hint">This folder does not match the opencode-sandbox workspace structure: a sandbox_config.json in .opencode_config/ and a workspace/ git worktree.</div>
<div class="hint">This is a pre-rename project/ layout. Converting renames project/ to workspace/ and rewrites the sandbox_config keys — files and your content are kept.</div>
<div class="rowacts">
<form hx-post="import-convert" hx-target="[data-role='screen']" hx-swap="innerHTML" class="inline">
<input type="hidden" name="path" value="{{.Path}}">
<button class="btn" type="submit">Convert to workspace layout</button>
</form>
{{backBtn}}</div></div>
{{else if .NonMatching}}<div class="panel">
<div class="notice warn">Import isn't possible here</div>
<div class="hint">This folder does not match the opencode-sandbox workspace structure: a sandbox_config.json in .opencode_config/ and a workspace/ git worktree.</div>
<div class="hint">Use the New Workspace workflow instead and clone the repo there via URL (in-container clone).</div>
{{backBtn}}</div>
{{else}}<div class="panel">
<div class="notice">Import summary</div>
<form hx-post="import-register" hx-target="[data-role='screen']" hx-swap="innerHTML">
<input type="hidden" name="path" value="{{.Path}}">
<label class="flabel" for="import-name">Workspace name</label>
<input id="import-name" class="field" type="text" name="name" value="{{.Name}}" required>
<dl class="kv">
<dt>Path</dt><dd>{{.Path}}</dd>
<dt>Edition</dt><dd>{{.Config.ContainerEdition}}</dd>
<dt>Modes</dt><dd>{{joinModes .Config.ContainerModes}}</dd>
<dt>Start option</dt><dd>{{.Config.StartOption}}</dd>
<dt>Status</dt><dd>stopped (starts on demand via Open Workspace)</dd>
</dl>
<div class="rowacts"><button class="btn" type="submit">Register</button>
{{backBtn}}</div>
</form></div>
{{end}}`))

// busyNoticeTmpl is the refusal fragment for op-launching POSTs while
// any op is queued or running (taskmgr semantics mirrored).
var busyNoticeTmpl = template.Must(template.New("busy").Parse(
	`<div class="notice warn">Another operation is queued or running — try again when it finishes.</div>`))

// importedTmpl confirms a registration.
var importedTmpl = template.Must(template.New("imported").Funcs(fragmentFuncs).Parse(
	`<div class="panel"><div class="notice ok">Imported {{.}} — registered, stopped.</div>{{backBtn}}</div>`))

// convertFailedTmpl reports a failed conversion — the folder is kept
// (the converter aborts before touching anything on failure).
var convertFailedTmpl = template.Must(template.New("convertfailed").Funcs(fragmentFuncs).Parse(
	`<div class="panel"><div class="notice bad">Converting the legacy layout failed: {{.}}</div>{{backBtn}}</div>`))

// renderT writes one fragment; template errors leave a truncated
// response, which the tests catch as missing markers.
func renderT(w http.ResponseWriter, t *template.Template, data any) {
	_ = t.Execute(w, data)
}

// handleImportScreen renders the path-input screen (GET /import).
func (s *Server) handleImportScreen(w http.ResponseWriter, r *http.Request) {
	renderT(w, importScreenTmpl, nil)
}

// handleImportWorkspace runs the server-side validation (POST
// /import-workspace {path}) and renders the next stage.
func (s *Server) handleImportWorkspace(w http.ResponseWriter, r *http.Request) {
	root := canonicalImportPath(r.FormValue("path"))
	res, err := importcheck.Check(root, s.regView())
	switch {
	case err != nil:
		renderT(w, importStageTmpl, importStageData{Broken: true, Path: root})
		return
	case res.Kind == importcheck.KindRegistered:
		renderT(w, importStageTmpl, importStageData{Guard: true, Path: root})
		return
	case res.Kind == importcheck.KindBrokenConfig:
		renderT(w, importStageTmpl, importStageData{Broken: true, Path: root})
		return
	case res.Kind == importcheck.KindLegacy:
		renderT(w, importStageTmpl, importStageData{Legacy: true, Path: root})
		return
	case res.Kind != importcheck.KindMatch:
		renderT(w, importStageTmpl, importStageData{NonMatching: true, Path: root})
		return
	}
	renderT(w, importStageTmpl, importStageData{
		Path:   root,
		Name:   res.Name,
		Config: res.Config,
	})
}

// handleImportRegister performs the registry-only change from the
// summary (POST /import-register {path, name}): the workspace is added
// as stopped, no folder content is touched. Refuses while any op is
// queued/running.
func (s *Server) handleImportRegister(w http.ResponseWriter, r *http.Request) {
	if s.busy() {
		renderT(w, busyNoticeTmpl, nil)
		return
	}
	root := canonicalImportPath(r.FormValue("path"))
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
	name := strings.TrimSpace(r.FormValue("name"))
	req := registry.AddRequest{Name: name, Path: root}
	if err == nil { // stored config drives the VCS link, like the TUI
		req.GitTracking = cfg.VCSTracking
		req.RepoURL = cfg.RepoURL
	}
	if err := s.regView().AddWorkspace(req); err != nil {
		renderT(w, registerFailedTmpl, importErrMsg(err))
		return
	}
	s.hub.PublishWorkspace() // re-render cards on open tabs
	renderT(w, importedTmpl, name)
}

// registerFailedTmpl + importErrMsg mirror the TUI's error toasts.
var registerFailedTmpl = template.Must(template.New("registerfailed").Funcs(fragmentFuncs).Parse(
	`<div class="panel"><div class="notice bad">{{.}}</div>{{backBtn}}</div>`))

func importErrMsg(err error) string {
	switch {
	case errors.Is(err, registry.ErrNameExists):
		return "Duplicate workspace name — choose a unique display name."
	case errors.Is(err, registry.ErrPathExists):
		return "Already registered — this path is a registered workspace."
	case errors.Is(err, registry.ErrBadName):
		return "Invalid workspace name (letters, digits, _, -)."
	case errors.Is(err, registry.ErrNotFound):
		return "workspaces.json not found — run first-run setup first."
	default:
		return fmt.Sprintf("Import failed: %v", err)
	}
}

// handleImportConvert runs the same per-root converter the first-start
// migration uses (stages 1–2), then renders the ordinary summary —
// convert then imports normally (spec §2). Refuses while busy.
func (s *Server) handleImportConvert(w http.ResponseWriter, r *http.Request) {
	if s.busy() {
		renderT(w, busyNoticeTmpl, nil)
		return
	}
	root := canonicalImportPath(r.FormValue("path"))
	if err := migration.ConvertRoot(migration.Root{Name: filepath.Base(root), Path: root}); err != nil {
		renderT(w, convertFailedTmpl, err.Error())
		return
	}
	s.handleImportWorkspace(w, r) // re-run the check → summary (or guard)
}

// canonicalImportPath normalizes the user-entered path exactly like the
// TUI's entry (TrimRight "/" + CanonicalPath).
func canonicalImportPath(p string) string {
	return registry.CanonicalPath(strings.TrimSpace(strings.TrimRight(strings.TrimSpace(p), "/")))
}
