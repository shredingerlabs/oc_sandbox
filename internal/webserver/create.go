// New Workspace wizard + create chain (spec §7/§8/§9, #156): the
// Workspace → New screen as one HTMX fragment — source radio pair
// (Blank Workspace / From Repo, name derived live from the repo URL),
// Agent row (default code; pkm pins the edition), Edition preselected
// swdev with the inline "Image build" row when the image is missing,
// VCS-credential and AI-provider selects fed from the global catalogs
// each with an inline "New…" that saves the entry to the catalog before
// applying it, and the token input for the selected provider.
//
// Create runs ONE create-chain task — image build (when missing and
// Build now) → init-workspace.sh → configure-workspace.sh in credential
// order — exactly as the TUI invokes the scripts (native script
// enhancement, ADR-0007), streamed into the log pane under the form via
// SSE line events keyed by the op ID. Standard visible rows only per
// §6; Advanced reveals the full option set (matrix-driven, #152): the
// presets — start web, proxy off, searxng, offline off, CBM-UI off, HIL
// on for embedded — are recorded regardless of the mode.
package webserver

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// nameRe validates the workspace name exactly like the registry
// (^[a-zA-Z0-9_-]+$).
var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// wizardEditionDefault is the §6 preselected edition.
const wizardEditionDefault = "swdev"

// wizardTemplate renders the New Workspace fragments (form, confirm).
// P carries the tokenized path prefix so hx-* targets stay absolute.
var wizardTemplate = template.Must(template.New("wizard").Parse(`
{{define "wizard"}}
<section class="wizard" id="new-workspace" data-screen="ws-new">
  <h2>New Workspace</h2>
  {{if .Error}}<p class="form-error" role="alert">{{.Error}}</p>{{end}}
  {{if or .LogLines (gt .OpID 0)}}
  <div class="logpane" data-role="create-log" data-op-id="{{.OpID}}">{{range .LogLines}}<div class="logline">{{.}}</div>{{end}}</div>
  {{end}}
  <form data-role="new-workspace-form" hx-post="{{.P}}workspaces" hx-target="#new-workspace" hx-swap="outerHTML">
    <fieldset class="rows" data-role="wizard-fields"{{if .Inert}} disabled{{end}}>

    <div class="row" data-row="source">
      <span class="label">Source</span>
      <span class="opts">
        <label><input type="radio" name="source" value="blank" {{if not .FromRepo}}checked{{end}}> Blank Workspace</label>
        <label><input type="radio" name="source" value="repo" {{if .FromRepo}}checked{{end}}> From Repo</label>
      </span>
    </div>
    <div class="row" data-row="repo_url" data-repo-only="{{if not .FromRepo}}1{{end}}">
      <span class="label">Repo URL</span>
      <input name="repo_url" value="{{.RepoURL}}" placeholder="https://github.com/user/repo.git">
    </div>
    <div class="row" data-row="name">
      <span class="label">Workspace name</span>
      <input name="name" value="{{.Name}}" data-role="wizard-name">
    </div>
    <div class="row" data-row="agent">
      <span class="label">Agent</span>
      <select name="agent" data-role="wizard-agent">
        <option value="code" {{if eq .Agent "code"}}selected{{end}}>Code</option>
        <option value="chat" {{if eq .Agent "chat"}}selected{{end}}>Chat Agent (NO_CODE)</option>
        <option value="pkm" {{if eq .Agent "pkm"}}selected{{end}}>PKM (Personal Knowledge Wiki)</option>
      </select>
    </div>
    <div class="row" data-row="edition" {{if eq .Agent "pkm"}}data-pkm-pin{{end}}>
      <span class="label">Edition</span>
      <select name="edition" data-role="wizard-edition">
        {{range .Editions}}<option value="{{.}}" {{if eq . $.Edition}}selected{{end}}>{{.}}</option>{{end}}
      </select>
    </div>
    {{if .ImageMissing}}
    <div class="row" data-row="image_build">
      <span class="label">Image build</span>
      <span class="opts">
        <label><input type="radio" name="build" value="now" {{if not .BuildLater}}checked{{end}}> Build now</label>
        <label><input type="radio" name="build" value="later" {{if .BuildLater}}checked{{end}}> Build later</label>
      </span>
    </div>
    {{end}}

    <div class="row" data-row="vcs">
      <span class="label">VCS credentials</span>
      <select name="vcs" data-role="wizard-vcs">
        <option value="none" {{if eq .VCS "none"}}selected{{end}}>none</option>
        {{range .Trackers}}<option value="{{.Name}}" {{if eq $.VCS .Name}}selected{{end}}>{{.Name}}</option>{{end}}
        <option value="new">New…</option>
      </select>
    </div>
    <div class="row" data-row="vcs_token" data-hidden="{{if eq .VCS "none"}}1{{end}}">
      <span class="label">VCS token</span>
      <input type="password" name="vcs_token" value="" autocomplete="off">
    </div>
    <div class="row" data-row="vcs_new" data-role="vcs-new" data-hidden="{{if not (eq .VCS "new")}}1{{end}}">
      <span class="label">New tracker (saved to the catalog, no token)</span>
      <label>Display name <input name="vcs_name" value="{{.VCSName}}"></label>
      <label>Git name <input name="vcs_git_name" value="{{.VCSGitName}}"></label>
      <label>Git e-mail <input name="vcs_git_email" value="{{.VCSGitMail}}"></label>
      <label>Tracker kind
        <select name="vcs_kind">
          {{range .TrackerKinds}}<option value="{{.}}">{{.}}</option>{{end}}
        </select>
      </label>
      <label>Host <input name="vcs_host" value="{{.VCSHost}}" placeholder="gitlab.example.com"></label>
    </div>

    <div class="row" data-row="provider">
      <span class="label">AI provider</span>
      <select name="provider" data-role="wizard-provider">
        <option value="none" {{if eq .Provider "none"}}selected{{end}}>none</option>
        {{range .Providers}}<option value="{{.Name}}" {{if eq $.Provider .Name}}selected{{end}}>{{.Name}}</option>{{end}}
        <option value="new">New…</option>
      </select>
    </div>
    <div class="row" data-row="provider_new" data-role="provider-new" data-hidden="{{if not (eq .Provider "new")}}1{{end}}">
      <span class="label">Add provider… (saved to the catalog, no token)</span>
      <label>Display name <input name="provider_name" value="{{.ProviderName}}"></label>
      <label>Endpoint <input name="provider_endpoint" value="{{.ProviderEndpoint}}" placeholder="https://api.example.com/v1"></label>
    </div>
    <div class="row" data-row="provider_token">
      <span class="label">Provider token</span>
      <input type="password" name="provider_token" value="" autocomplete="off">
    </div>

    <div class="row" data-row="start_option" data-advanced-only>
      <span class="label">Start option</span>
      <span class="opts">
        <label><input type="radio" name="start_option" value="console" {{if eq .StartOption "console"}}checked{{end}}> console</label>
        <label><input type="radio" name="start_option" value="opencode" {{if eq .StartOption "opencode"}}checked{{end}}> opencode</label>
        <label><input type="radio" name="start_option" value="web" {{if eq .StartOption "web"}}checked{{end}}> web</label>
      </span>
    </div>
    <div class="row" data-row="proxy" data-advanced-only>
      <span class="label">Proxy</span>
      <span class="opts">
        <label><input type="radio" name="proxy" value="no" {{if not .Proxy}}checked{{end}}> off</label>
        <label><input type="radio" name="proxy" value="yes" {{if .Proxy}}checked{{end}}> on</label>
      </span>
    </div>
    <div class="row" data-row="search_mode" data-advanced-only>
      <span class="label">Search mode</span>
      <span class="opts">
        <label><input type="radio" name="search_mode" value="none" {{if eq .SearchMode "none"}}checked{{end}}> none</label>
        <label><input type="radio" name="search_mode" value="websearch" {{if eq .SearchMode "websearch"}}checked{{end}}> websearch</label>
        <label><input type="radio" name="search_mode" value="searxng" {{if eq .SearchMode "searxng"}}checked{{end}}> searxng</label>
      </span>
    </div>
    <div class="row" data-row="offline" data-advanced-only>
      <span class="label">Offline</span>
      <label><input type="checkbox" name="offline" value="on" {{if .Offline}}checked{{end}}> on</label>
    </div>
    <div class="row" data-row="cbm_ui" data-advanced-only>
      <span class="label">CBM-UI</span>
      <label><input type="checkbox" name="cbm_ui" value="on" {{if .CBMUI}}checked{{end}}> on</label>
    </div>
    <div class="row" data-row="hil" data-advanced-only>
      <span class="label">HIL</span>
      <label><input type="checkbox" name="hil" value="on" {{if .HIL}}checked{{end}}> on</label>
    </div>
    <div class="row hint" data-row="presets">{{.PresetsHint}}</div>

    </fieldset>
    <div class="actions">
      <button type="submit" data-role="wizard-create">Create Workspace</button>
    </div>
  </form>
</section>
{{end}}

{{define "confirm"}}
<div class="confirm" id="create-confirm" data-role="create-confirm">
  <h2>Create this workspace?</h2>
  {{if .Error}}<p class="form-error" role="alert">{{.Error}}</p>{{end}}
  <ul class="summary">
    {{range .Lines}}<li>{{.}}</li>{{end}}
  </ul>
  <form hx-post="{{.P}}workspaces" hx-target="#new-workspace" hx-swap="outerHTML">
    {{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
    <div class="actions">
      <button type="submit" name="confirm" value="1">Create</button>
      <button type="submit" name="confirm" value="">Back</button>
    </div>
  </form>
</div>
{{end}}
`))

// wizardForm is the wizard's rendered state — the parsed payload with
// defaults applied (the GET's blank form and the POST's redisplay share
// one shape).
type wizardForm struct {
	P     string // tokenized path prefix
	Error string

	FromRepo bool
	RepoURL  string
	Name     string
	Agent    string
	Edition  string

	ImageMissing bool
	BuildLater   bool

	VCS              string
	VCSName          string
	VCSGitName       string
	VCSGitMail       string
	VCSKind          string
	VCSHost          string
	Provider         string
	ProviderName     string
	ProviderEndpoint string

	StartOption string
	Proxy       bool
	SearchMode  string
	Offline     bool
	CBMUI       bool
	HIL         bool

	Trackers     []catalogs.Tracker
	Providers    []catalogs.Provider
	TrackerKinds []string
	Editions     []string
	PresetsHint  string

	// create-chain log pane state (inert form while the chain runs)
	LogLines []string
	OpID     int
	Inert    bool

	// confirm-popup state (POST validation output)
	Fields *formFields
	Lines  []string
}

// formField is one hidden field of the confirm popup's round-trip form.
type formField struct {
	Name  string
	Value string
}

// formFields is the ordered hidden-payload list of the confirm popup.
type formFields []formField

// wizardDefaults returns the §6 preset state the wizard opens with:
// agent code, edition swdev, start web, proxy off, searxng, offline /
// CBM-UI off. HIL presets on for embedded (passed in at render time —
// the preset follows the selected edition).
func wizardDefaults() wizardForm {
	return wizardForm{
		Agent:        "code",
		Edition:      wizardEditionDefault,
		VCS:          "none",
		Provider:     "none",
		StartOption:  "web",
		SearchMode:   "searxng",
		TrackerKinds: catalogs.TrackerKinds,
	}
}

// handleNewWorkspaceGET serves GET /workspaces/new: the blank wizard
// form with the catalog feeds and the live edition/image discovery.
func (s *Server) handleNewWorkspaceGET(w http.ResponseWriter, r *http.Request) {
	f := wizardDefaults()
	s.renderWizard(w, f)
}

// renderWizard renders the form fragment with the current feeds and
// discovery (fresh per call, data map §2).
func (s *Server) renderWizard(w http.ResponseWriter, f wizardForm) {
	cat := catalogs.Read(s.opts.Home)
	f.P = s.pathPrefix()
	f.PresetsHint = PresetsHint
	f.Trackers = cat.Trackers
	f.Providers = cat.Providers
	f.Editions = s.wizardEditions()
	if f.Edition == "" || !wizardHasEdition(f.Editions, f.Edition) {
		f.Edition = wizardPickEdition(f.Editions)
	}
	if f.Agent != "pkm" { // pkm pins the edition (ADR-0027)
		f.ImageMissing = f.Edition != "" && !discovery.ImageExists(s.runner(), f.Edition)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wizardTemplate.ExecuteTemplate(w, "wizard", f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// wizardEditions lists the selectable editions from the Dockerfile
// registry; an unreadable registry yields nil (the wizard shows the
// refusal on create — never guesses an edition).
func (s *Server) wizardEditions() []string {
	eds, err := discovery.SelectableEditions(s.opts.Dockerfile, s.opts.DockerfileCustom)
	if err != nil {
		return nil
	}
	return eds
}

// wizardPickEdition preselects swdev when present (§6), else the first
// selectable edition.
func wizardPickEdition(eds []string) string {
	if wizardHasEdition(eds, wizardEditionDefault) {
		return wizardEditionDefault
	}
	if len(eds) > 0 {
		return eds[0]
	}
	return ""
}

func wizardHasEdition(eds []string, e string) bool {
	for _, x := range eds {
		if x == e {
			return true
		}
	}
	return false
}

// runner resolves the command runner (nil → exec).
func (s *Server) runner() cmdrun.Runner {
	if s.opts.Runner != nil {
		return s.opts.Runner
	}
	return cmdrun.ExecRunner{}
}

// handleWorkspacesPOST serves POST /workspaces (§9): the wizard payload
// → validate → create-summary confirm → one create-chain task. The
// confirm round-trips the same endpoint (confirm=1 launches; empty
// re-renders the form with the entered values).
func (s *Server) handleWorkspacesPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	f := s.parseWizardForm(r)
	if r.PostFormValue("confirm") != "1" {
		if msg := s.validateWizard(r, f); msg != "" {
			f.Error = msg
			s.renderWizard(w, f)
			return
		}
		s.renderConfirm(w, r, f)
		return
	}
	// confirm=1: refuse while a chain for this workspace is queued or
	// running (one-at-a-time per workspace, parallel across workspaces),
	// then launch the ONE create-chain task.
	if msg := s.validateWizard(r, f); msg != "" {
		f.Error = msg
		s.renderWizard(w, f)
		return
	}
	opID, busy := s.createBegin(f.Name)
	if busy {
		f.Error = "A create chain for this workspace is already queued or running — wait for it to finish."
		s.renderWizard(w, f)
		return
	}
	chain := s.newCreateChain(r, f)
	if chain.err != "" {
		s.createBusyEnd(f.Name) // the launch is abandoned before any script runs
		f.Error = chain.err
		s.renderWizard(w, f)
		return
	}
	chain.opID = opID
	f.OpID = opID
	f.Inert = true // the form stays visible but inert while the chain runs
	s.renderWizard(w, f)
	go chain.run(s)
}

// trackerLabel maps a tracker kind onto the stored vcs_tracking value —
// the exact option vocabulary the settings flow reads back
// (github.com / gitlab.com / own GitLab / others).
func trackerLabel(kind string) string {
	switch kind {
	case catalogs.KindGitHub:
		return "github.com"
	case catalogs.KindGitLab:
		return "gitlab.com"
	case catalogs.KindGitLabSel:
		return "own GitLab"
	case catalogs.KindOther:
		return "others"
	}
	return "none"
}

// wizardVCSTracking resolves the stored vcs_tracking value of the
// payload: the catalog entry's kind label, or "none".
func (s *Server) wizardVCSTracking(f wizardForm) string {
	if f.VCS == "none" {
		return "none"
	}
	var kind string
	if f.VCS == "new" {
		kind = f.VCSKind
	} else if t, ok := s.wizardTracker(f.VCS); ok {
		kind = t.Kind
	}
	return trackerLabel(kind)
}

// wizardTracker finds a catalog tracker by display name (slug match).
func (s *Server) wizardTracker(name string) (catalogs.Tracker, bool) {
	slug := catalogs.Slug(name)
	for _, t := range catalogs.Read(s.opts.Home).Trackers {
		if catalogs.Slug(t.Name) == slug {
			return t, true
		}
	}
	return catalogs.Tracker{}, false
}

// wizardProvider finds a catalog provider by display name (slug match).
func (s *Server) wizardProvider(name string) (catalogs.Provider, bool) {
	slug := catalogs.Slug(name)
	for _, p := range catalogs.Read(s.opts.Home).Providers {
		if catalogs.Slug(p.Name) == slug {
			return p, true
		}
	}
	return catalogs.Provider{}, false
}

// renderConfirm renders the create-summary confirmation popup (§9: the
// TUI confirm set carried 1:1 — newworkspace.askCreate parity) with the
// payload round-tripping in hidden fields.
func (s *Server) renderConfirm(w http.ResponseWriter, r *http.Request, f wizardForm) {
	if f.Agent == "pkm" { // pkm pins the edition (ADR-0027)
		f.Edition = "pkm"
	}
	lines := []string{
		"name: " + f.Name,
		"path: " + s.wizardRoot(f.Name),
		"edition: " + f.Edition,
	}
	if f.Agent == "chat" {
		lines = append(lines, "agent: chat")
	}
	lines = append(lines,
		"start: "+f.StartOption,
		"search: "+f.SearchMode,
		"VCS tracking: "+s.wizardVCSTracking(f),
	)
	if f.Provider == "new" {
		lines = append(lines, "AI provider: "+f.ProviderName)
	} else if p, ok := s.wizardProvider(f.Provider); ok {
		lines = append(lines, "AI provider: "+p.Name)
	} else {
		lines = append(lines, "AI provider: none")
	}
	if !discovery.ImageExists(s.runner(), f.Edition) {
		line := "image build: Build later"
		if !f.BuildLater {
			line = "image build: Build now"
		}
		lines = append(lines, line)
	}
	fields := formFields{}
	for _, k := range sortedFormKeys(r.PostForm) {
		if k == "confirm" {
			continue
		}
		fields = append(fields, formField{Name: k, Value: r.PostForm.Get(k)})
	}
	data := struct {
		P      string
		Error  string
		Lines  []string
		Fields formFields
	}{P: s.pathPrefix(), Lines: lines, Fields: fields}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wizardTemplate.ExecuteTemplate(w, "confirm", data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// sortedFormKeys gives the hidden round-trip a deterministic order.
func sortedFormKeys(vs url.Values) []string {
	keys := make([]string, 0, len(vs))
	for k := range vs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// parseWizardForm reads the wizard payload into the rendered state,
// applying the §6 presets where a row is absent.
func (s *Server) parseWizardForm(r *http.Request) wizardForm {
	f := wizardDefaults()
	f.FromRepo = r.PostFormValue("source") == "repo"
	f.RepoURL = strings.TrimSpace(r.PostFormValue("repo_url"))
	f.Name = strings.TrimSpace(r.PostFormValue("name"))
	f.Agent = r.PostFormValue("agent")
	f.Edition = r.PostFormValue("edition")
	f.BuildLater = r.PostFormValue("build") == "later"
	f.VCS = r.PostFormValue("vcs")
	if f.VCS == "" {
		f.VCS = "none"
	}
	f.VCSName = r.PostFormValue("vcs_name")
	f.VCSGitName = r.PostFormValue("vcs_git_name")
	f.VCSGitMail = r.PostFormValue("vcs_git_email")
	f.VCSKind = r.PostFormValue("vcs_kind")
	f.VCSHost = strings.TrimSpace(r.PostFormValue("vcs_host"))
	f.Provider = r.PostFormValue("provider")
	if f.Provider == "" {
		f.Provider = "none"
	}
	f.ProviderName = r.PostFormValue("provider_name")
	f.ProviderEndpoint = strings.TrimSpace(r.PostFormValue("provider_endpoint"))
	f.StartOption = r.PostFormValue("start_option")
	if f.StartOption == "" {
		f.StartOption = "web" // §6 preset — presets, not pinning
	}
	f.Proxy = r.PostFormValue("proxy") == "yes"
	f.SearchMode = r.PostFormValue("search_mode")
	if f.SearchMode == "" {
		f.SearchMode = "searxng" // §6 preset
	}
	f.Offline = r.PostFormValue("offline") == "on"
	f.CBMUI = r.PostFormValue("cbm_ui") == "on"
	f.HIL = r.PostFormValue("hil") == "on"
	return f
}

// validateWizard checks the payload; "" when valid. Mirrors the TUI's
// validateInputs plus the ADR-0026 exclusions start.sh hard-exits on.
func (s *Server) validateWizard(r *http.Request, f wizardForm) string {
	if !nameRe.MatchString(f.Name) {
		return "Invalid workspace name — use [a-zA-Z0-9_-]+."
	}
	if f.FromRepo && f.RepoURL == "" {
		return "Repo URL is required for a From Repo workspace."
	}
	if f.Agent == "pkm" { // pkm pins the edition (ADR-0027)
		f.Edition = "pkm"
	}
	if f.Edition == "" {
		return "No container editions found in Dockerfile/Dockerfile.custom."
	}
	reg := s.registry()
	if _, err := reg.ByName(f.Name); err == nil {
		return "Duplicate workspace name — choose a unique display name."
	}
	if _, err := reg.ByPath(s.wizardRoot(f.Name)); err == nil {
		return "Duplicate workspace path — choose a unique location."
	}
	if f.SearchMode == "searxng" && (f.Proxy || f.Offline) {
		return "SearXNG needs network — it excludes the proxy and offline mode."
	}
	if f.Offline && f.CBMUI {
		return "offline and cbm_ui are mutually exclusive."
	}
	if f.VCS == "new" {
		t := catalogs.Tracker{Name: f.VCSName, GitName: f.VCSGitName, GitEmail: f.VCSGitMail, Kind: f.VCSKind, Host: f.VCSHost}
		if err := catalogs.ValidateTracker(t); err != nil {
			return err.Error()
		}
	}
	if f.Provider == "new" {
		p := catalogs.Provider{Name: f.ProviderName, Endpoint: f.ProviderEndpoint}
		if err := catalogs.ValidateProvider(p); err != nil {
			return err.Error()
		}
	}
	if f.Provider != "none" && !providerid.ValidToken(r.PostFormValue("provider_token")) {
		return "Token must not be empty or contain whitespace/newlines."
	}
	return ""
}

// registry returns the registry view for the server's home.
func (s *Server) registry() *registry.Registry {
	return &registry.Registry{Home: s.opts.Home}
}

// wizardRoot derives the workspace root from the name — the default
// workspace path base + the name (the wizard has no path row; §6).
func (s *Server) wizardRoot(name string) string {
	base := s.opts.DefaultWorkspacePath
	if base == "" {
		base = s.opts.Home + "/oc-sandbox"
	}
	return registry.CanonicalPath(base + "/" + pathSlug(name))
}

// pathSlug squeezes a name into a directory tail (newworkspace parity):
// lowercase, alphanumerics kept, separator-ish characters become '-'.
func pathSlug(name string) string {
	lower := strings.ToLower(name)
	var b []rune
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b = append(b, r)
		case r == '-' || r == '_' || r == '.' || r == ' ':
			b = append(b, '-')
		}
	}
	return string(b)
}

// --- the create chain (ONE task: image build → init → configure) ------------

// createBegin reserves the workspace's chain slot and assigns the op ID
// for the SSE line events. The reservation is the conflict semantics:
// while a chain is queued or running for the name, confirms refuse;
// chains for other names run in parallel.
func (s *Server) createBegin(name string) (int, bool) {
	s.createMu.Lock()
	defer s.createMu.Unlock()
	if s.createBusy[name] {
		return 0, true
	}
	s.createSeq++
	s.createBusy[name] = true
	return s.createSeq, false
}

func (s *Server) createBusyEnd(name string) {
	s.createMu.Lock()
	defer s.createMu.Unlock()
	delete(s.createBusy, name)
}

// createChain is the captured payload of one confirmed create — every
// request-scoped value (tokens included) is read before the chain goes
// to its own goroutine. err carries a catalog-save refusal (the launch
// is abandoned before any script runs).
type createChain struct {
	err string

	opID int
	name string
	root string

	fromRepo bool
	repoURL  string
	agent    string // code | chat | pkm
	edition  string // pkm pinned for the pkm agent

	vcsLabel string // stored vcs_tracking value (none/github.com/…)
	vcsKind  string // catalog kind slug driving the invocation
	vcsHost  string
	vcsToken string
	gitName  string
	gitEmail string

	providerName     string
	providerEndpoint string
	providerToken    string
	providerID       string

	modes  []string
	start  string
	search string
	proxy  bool
	build  bool // Build now decision (the row only drives visibility)
}

// newCreateChain captures the confirmed payload (fresh image/edition
// decision re-checked here, like the TUI's create()).
func (s *Server) newCreateChain(r *http.Request, f wizardForm) *createChain {
	if f.Agent == "pkm" { // pkm pins the edition (ADR-0027)
		f.Edition = "pkm"
	}
	c := &createChain{
		name:             f.Name,
		root:             s.wizardRoot(f.Name),
		fromRepo:         f.FromRepo,
		repoURL:          f.RepoURL,
		agent:            f.Agent,
		edition:          f.Edition,
		vcsLabel:         s.wizardVCSTracking(f),
		vcsHost:          f.VCSHost,
		vcsToken:         r.PostFormValue("vcs_token"),
		gitName:          strings.TrimSpace(f.VCSGitName),
		gitEmail:         strings.TrimSpace(f.VCSGitMail),
		providerName:     f.ProviderName,
		providerEndpoint: f.ProviderEndpoint,
		providerToken:    r.PostFormValue("provider_token"),
		modes:            wizardModes(f),
		start:            f.StartOption,
		search:           f.SearchMode,
		proxy:            f.Proxy,
	}
	c.build = !discovery.ImageExists(s.runner(), c.edition) && !f.BuildLater
	// resolve catalog entries (picked or the inline "New…" — the entry
	// saves to the catalog before applying it, §8; an empty catalog
	// never dead-ends first-run)
	var tracker catalogs.Tracker
	var provider catalogs.Provider
	if f.VCS == "new" {
		tracker = catalogs.Tracker{Name: f.VCSName, GitName: f.VCSGitName, GitEmail: f.VCSGitMail, Kind: f.VCSKind, Host: f.VCSHost}
		c.vcsKind = tracker.Kind
		if err := s.saveCatalogEntry(&tracker, nil); err != "" {
			c.err = err
			return c
		}
	} else if t, ok := s.wizardTracker(f.VCS); ok {
		tracker = t
		c.vcsKind = t.Kind
		if c.vcsHost == "" {
			c.vcsHost = t.Host
		}
	}
	c.gitName, c.gitEmail = strings.TrimSpace(tracker.GitName), strings.TrimSpace(tracker.GitEmail)
	if f.Provider == "new" {
		provider = catalogs.Provider{Name: f.ProviderName, Endpoint: f.ProviderEndpoint}
		if err := s.saveCatalogEntry(nil, &provider); err != "" {
			c.err = err
			return c
		}
	} else if p, ok := s.wizardProvider(f.Provider); ok {
		provider = p
	}
	if provider.Name != "" {
		c.providerName = provider.Name
		c.providerEndpoint = provider.Endpoint
		c.providerID = providerid.Slug(provider.Name)
	}
	return c
}

// saveCatalogEntry persists the inline "New…" entry to the global
// catalog before applying it (§8); entries stay token-free — the typed
// tokens ride the chain payload only.
func (s *Server) saveCatalogEntry(t *catalogs.Tracker, p *catalogs.Provider) string {
	d := catalogs.Read(s.opts.Home)
	var err error
	if t != nil {
		err = d.AddTracker(*t)
	} else {
		err = d.AddProvider(*p)
	}
	if err == nil {
		err = catalogs.WriteFile(s.opts.Home, d)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// wizardModes computes container_modes from the Advanced toggles: the
// §6 presets are recorded regardless of the display mode — proxy off /
// offline off / CBM-UI off stay absent, HIL on for embedded (the row's
// checked state posts the preset).
func wizardModes(f wizardForm) []string {
	var modes []string
	add := func(m string) { modes = append(modes, m) }
	if f.Offline {
		add("offline")
	}
	if f.CBMUI {
		add("cbm_ui")
	}
	if f.HIL {
		add("hil_mode")
	}
	return modes
}

// run executes the chain: every step streams into the log pane under
// the form via SSE line events keyed by the op ID; the scripts are
// invoked exactly as the TUI invokes them — `bash <script> <args>` —
// through the runner seam (ADR-0007).
func (c *createChain) run(s *Server) {
	emit := func(line string) { s.hub.PublishLine(c.opID, line) }
	emitf := func(format string, a ...any) { emit(fmt.Sprintf(format, a...)) }
	runScript := func(script string, args ...string) error {
		out, err := s.runner().Output("bash", append([]string{script}, args...)...)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		return err
	}
	fail := func(msg string) {
		emit(msg)
		s.createBusyEnd(c.name)
		s.hub.PublishWorkspaceOp(c.opID, "failed")
	}

	// image build (only when the image is missing and Build now won)
	if c.build {
		emitf("==> Building image %s", c.edition)
		if err := runScript(s.opts.BuildScript, c.edition); err != nil ||
			!discovery.ImageExists(s.runner(), c.edition) {
			fail("Build failed — the " + c.edition + " image is still unavailable. Press Create Workspace to retry.")
			return
		}
	}

	// init-workspace.sh BEFORE any state write (registry/config)
	emitf("==> Creating workspace %s", c.name)
	initArgs := []string{c.root}
	if kind := agentKind(c.agent); kind != "" {
		initArgs = append(initArgs, "--workspace_kind", kind)
	}
	if c.fromRepo {
		initArgs = append(initArgs, "--repo_url", c.repoURL)
	}
	if err := runScript(s.opts.InitScript, initArgs...); err != nil {
		fail("Workspace initialization failed: " + err.Error() + " — no workspace state was registered.")
		return
	}

	// sandbox_config.json: the Create payload + the atomic patches
	cfgPath := sandboxcfg.Path(c.root)
	providerID := c.providerID
	if providerID == "" {
		providerID = "none"
	}
	if err := sandboxcfg.Create(s.opts.Home, cfgPath, c.edition, c.modes, c.start, providerID, c.search); err != nil {
		fail("sandbox_config.json could not be created: " + err.Error())
		return
	}
	if err := c.patchConfig(s.opts.Home, cfgPath); err != nil {
		fail("sandbox_config.json could not be patched: " + err.Error())
		return
	}

	// configure-workspace.sh in credential order (TUI buildCreds parity)
	if c.vcsKind != "" && strings.TrimSpace(c.vcsToken) != "" {
		emit("==> Configuring credentials")
		if err := runScript(s.opts.ConfigureScript, vcsCredArgs(c)...); err != nil {
			fail("Workspace configuration failed — " + err.Error())
			return
		}
	}
	if c.vcsKind != "" && c.gitName != "" && c.gitEmail != "" {
		emit("==> Configuring credentials")
		if err := runScript(s.opts.ConfigureScript, "git-identity", c.root, "--name", c.gitName, "--email", c.gitEmail); err != nil {
			fail("Workspace configuration failed — " + err.Error())
			return
		}
	}
	if c.providerName != "" {
		emit("==> Configuring credentials")
		if err := runScript(s.opts.ConfigureScript, "ai-provider", c.root, "--name", c.providerName,
			"--endpoint", c.providerEndpoint, "--token", c.providerToken); err != nil {
			fail("Workspace configuration failed — " + err.Error())
			return
		}
		if err := runScript(s.opts.ConfigureScript, "opencode-config", c.root, "--id", c.providerID,
			"--name", c.providerName, "--endpoint", c.providerEndpoint); err != nil {
			fail("Workspace configuration failed — " + err.Error())
			return
		}
	}

	// the registry write (missing workspaces.json seeds once)
	reg := &registry.Registry{Home: s.opts.Home}
	req := registry.AddRequest{
		Name: c.name, Path: c.root,
		GitTracking: c.vcsLabel, RepoURL: c.repoURL,
	}
	err := reg.AddWorkspace(req)
	if errors.Is(err, registry.ErrNotFound) {
		err = reg.CreateWorkspacesFile()
		if err == nil {
			err = reg.AddWorkspace(req)
		}
	}
	if err != nil {
		fail("Workspace not registered: " + err.Error())
		return
	}
	emit("Workspace created successfully.")
	s.createBusyEnd(c.name)
	s.hub.PublishWorkspaceOp(c.opID, "done")
}

// patchConfig applies the field patches via the atomic paths
// (newworkspace.patchConfig parity).
func (c *createChain) patchConfig(home, cfgPath string) error {
	source := "empty"
	if c.fromRepo {
		source = "cloned"
	}
	if err := sandboxcfg.PatchString(home, cfgPath, "workspace_source", source); err != nil {
		return err
	}
	if source == "cloned" {
		if err := sandboxcfg.PatchString(home, cfgPath, "repo_url", c.repoURL); err != nil {
			return err
		}
	}
	if err := sandboxcfg.PatchString(home, cfgPath, "vcs_tracking", c.vcsLabel); err != nil {
		return err
	}
	if kind := agentKind(c.agent); kind != "" { // kind recorded once, never re-applied
		if err := sandboxcfg.PatchString(home, cfgPath, "workspace_kind", kind); err != nil {
			return err
		}
	}
	if c.proxy {
		return sandboxcfg.PatchBool(home, cfgPath, "use_proxy", true)
	}
	return nil
}

// agentKind maps the agent to the --workspace_kind value ("" for code).
func agentKind(agent string) string {
	switch agent {
	case "chat":
		return "chat"
	case "pkm":
		return "pkm"
	}
	return ""
}

// vcsCredArgs renders the configure-workspace.sh vcs-credentials
// invocation from the catalog kind (newworkspace.vcsCredStep parity,
// host-from-entry instead of the free-form row).
func vcsCredArgs(c *createChain) []string {
	switch c.vcsKind {
	case catalogs.KindGitHub:
		return []string{"vcs-credentials", "github", c.root, "--token", c.vcsToken}
	case catalogs.KindGitLab:
		return []string{"vcs-credentials", "gitlab", c.root, "--token", c.vcsToken, "--host", "gitlab.com"}
	case catalogs.KindGitLabSel:
		return []string{"vcs-credentials", "gitlab", c.root, "--token", c.vcsToken, "--host", c.vcsHost}
	case catalogs.KindOther:
		return []string{"vcs-credentials", "custom", c.root, "--token", c.vcsToken, "--host", c.vcsHost}
	}
	return nil
}
