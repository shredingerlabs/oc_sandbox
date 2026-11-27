// New Workspace wizard + create chain (spec §7/§8/§9, #156; toolchain
// sets #205 WP-3): the Workspace → New screen as one HTMX fragment —
// source radio pair (Blank Workspace / From Repo, name derived live
// from the repo URL), Agent row (default code; pkm pins the set to base
// only), the bundle row in today's Edition slot (the 9 manifest bundles
// as chips, chat/pkm rendered "base only", a missing-member bundle
// rendered disabled with its reason) over the Advanced-only toolchain
// multi-select (flat alphabetical, live cached markers, invalid units
// disabled with their reason, "n of m selected" header) — the
// multi-select is the payload, a bundle chip overwrites it, editing it
// flips the row to Custom, the default preselect is the swdev bundle,
// and the build-plan row keys to the selection's set-tag existence with
// the honest cost line (counts, no time estimates) —, VCS-credential
// and AI-provider selects fed from the global catalogs (pick-only —
// entries are managed in Settings › VCS Tracker / AI-Provider), each
// with its per-workspace token input inline.
//
// Create runs ONE create-chain task — image build (when missing and
// Build now) → init-workspace.sh → configure-workspace.sh in credential
// order — exactly as the TUI invokes the scripts (native script
// enhancement, ADR-0007), streamed into the log pane under the form via
// SSE line events keyed by the op ID. Standard visible rows only per
// §6; Advanced reveals the full option set (matrix-driven, #152): the
// presets — start web, proxy off, searxng, offline off, CBM-UI off, HIL
// on iff the selection carries a hardware unit — are recorded
// regardless of the mode.
package webserver

import (
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/identity"
	"github.com/shredingerlabs/oc-sandbox/internal/newworkspace"
	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// wizardBundleDefault is the §6 preselected bundle.
const wizardBundleDefault = "swdev"

// wizardTemplate renders the New Workspace fragments (form, confirm).
// P carries the tokenized path prefix so hx-* targets stay absolute.
// Styling per the signed-off prototype (ws-new): a pagehead with the
// Blank/From-Repo segment, .flabel/.sub rows, .edotsel chips for the
// edition, .switch toggles for the Advanced booleans. The functional
// payload (field names, defaults, hidden round-trip) keeps the previous
// contract minus the removed inline-new rows.
var wizardTemplate = template.Must(template.New("wizard").
	Funcs(template.FuncMap{"hueFor": editionHue}).
	Funcs(iconFuncs).
	Parse(`
{{define "wizard"}}
<section class="wizard" id="new-workspace" data-screen="ws-new">
  <div class="pagehead"><div>
  <h2>Create a workspace</h2>
  <div class="desc">Name, source, edition and provider are recorded once; later changes apply on next start.</div>
  </div>
  <span class="seg" role="group" aria-label="creation path">
    <label>{{icon "file-plus"}}Blank Workspace<input type="radio" name="source" value="blank" form="new-workspace-form" {{if not .FromRepo}}checked{{end}}></label>
    <label>{{icon "git-fork"}}From Repo<input type="radio" name="source" value="repo" form="new-workspace-form" {{if .FromRepo}}checked{{end}}></label>
  </span></div>
  {{if .Error}}<p class="form-error" role="alert">{{.Error}}</p>{{end}}
  {{if or .LogLines (gt .OpID 0)}}
  <div class="logpane" data-role="create-log" data-op-id="{{.OpID}}">{{range .LogLines}}<div class="logline">{{.}}</div>{{end}}</div>
  {{end}}
  <form data-role="new-workspace-form" id="new-workspace-form" data-draft-key="new-workspace" hx-post="{{.P}}workspaces" hx-target="#new-workspace" hx-swap="outerHTML">
    <fieldset class="rows" data-role="wizard-fields"{{if .Inert}} disabled{{end}}>

    <div class="row" data-row="name">
      <label class="flabel" for="wiz-name">Workspace name</label>
      <input id="wiz-name" name="name" value="{{.Name}}" data-role="wizard-name" placeholder="my-workspace">
      <div class="sub">Under your workspaces root — derived from the repo URL on From Repo, still editable.</div>
    </div>
    <div class="row" data-row="repo_url" data-repo-only="{{if not .FromRepo}}1{{end}}">
      <label class="flabel" for="wiz-url">Repo URL</label>
      <input id="wiz-url" name="repo_url" value="{{.RepoURL}}" placeholder="https://github.com/user/repo.git">
      <div class="sub">Cloned at create (in-container clone during first-run setup).</div>
    </div>
    <div class="row" data-row="agent">
      <span class="label">Agent</span>
      <select name="agent" data-role="wizard-agent">
        <option value="code" {{if eq .Agent "code"}}selected{{end}}>Code</option>
        <option value="chat" {{if eq .Agent "chat"}}selected{{end}}>Chat Agent (NO_CODE)</option>
        <option value="pkm" {{if eq .Agent "pkm"}}selected{{end}}>PKM (Personal Knowledge Wiki)</option>
      </select>
      <span class="sub">PKM pins the set to base only (ADR-0027).</span>
    </div>
    <div class="row" data-row="bundle" {{if eq .Agent "pkm"}}data-pkm-pin{{end}}>
      <span class="label">Toolchain set</span>
      <span class="edotsel" data-role="wizard-bundle">
        {{range .Bundles}}<button type="button" class="edopt" data-bundle="{{.Name}}" data-members="{{.Members}}" aria-pressed="{{if .Pressed}}true{{else}}false{{end}}"{{if not .Valid}} disabled title="{{.Reason}}"{{end}}><i style="--ed:{{hueFor .Name}}"></i>{{.Name}}{{if .BaseOnly}} (base only){{end}}</button>{{end}}
        <button type="button" class="edopt" data-bundle="custom" data-members="" aria-pressed="{{if .Custom}}true{{else}}false{{end}}" disabled>Custom</button>
      </span>
      <div class="sub">A bundle preselects the units below (Advanced); a custom unit list reads Custom.</div>
    </div>
    <div class="row" data-row="toolchains" data-advanced-only{{if eq .Agent "pkm"}} data-pkm-pin{{end}}>
      <span class="label">Toolchains</span>
      <span class="edotsel units" data-role="wizard-units">
        <span class="sub" data-role="units-count">{{.SelCount}} of {{.UnitCount}} selected</span>
        <input type="hidden" name="units_present" value="1">
        {{range .Units}}<label class="unit"{{if not .Valid}} data-invalid{{end}}><input type="checkbox" name="toolchain" value="{{.ID}}"{{if .Checked}} checked{{end}}{{if not .Valid}} disabled{{end}}>{{.ID}}{{if .Cached}}<span class="cached">cached</span>{{end}}{{if not .Valid}}<span class="reason">{{.Reason}}</span>{{end}}</label>{{end}}
      </span>
      <div class="sub">The set image assembles these toolchains — a cached one skips its build.</div>
    </div>
    {{if .SetMissing}}
    <div class="row" data-row="image_build">
      <span class="label">Image build</span>
      <span class="seg">
        <label>{{icon "hammer"}}Build now<input type="radio" name="build" value="now" {{if not .BuildLater}}checked{{end}}></label>
        <label>{{icon "clock"}}Build later<input type="radio" name="build" value="later" {{if .BuildLater}}checked{{end}}></label>
      </span>
      <span class="sub">{{.BuildCost}}</span>
    </div>
    {{end}}

    <div class="row" data-row="vcs">
      <span class="label">VCS credentials</span>
      <select name="vcs" data-role="wizard-vcs">
        <option value="none" {{if eq .VCS "none"}}selected{{end}}>none</option>
        {{range .Trackers}}<option value="{{.Name}}" {{if eq $.VCS .Name}}selected{{end}}>{{.Name}} — {{.Kind}}</option>{{end}}
      </select>
      <input type="password" name="vcs_token" value="" autocomplete="off" placeholder="VCS token">
      <span class="sub">From the global tracker list (Settings › VCS Tracker); the typed token is per-workspace, never an entry of the catalog.</span>
    </div>

    <div class="row" data-row="provider">
      <span class="label">AI provider</span>
      <select name="provider" data-role="wizard-provider">
        <option value="none" {{if eq .Provider "none"}}selected{{end}}>none</option>
        {{range .Providers}}<option value="{{.Name}}" {{if eq $.Provider .Name}}selected{{end}}>{{.Name}}</option>{{end}}
      </select>
      <input type="password" name="provider_token" value="" autocomplete="off" placeholder="API token" data-provider-token>
      <span class="sub">Tokens live with the workspace; entries are managed in Settings › AI-Provider.</span>
      <span class="sub">From the global provider list (Settings › AI-Provider); the typed token is per-workspace, never an entry of the catalog.</span>
    </div>

    <div class="row" data-row="modes" data-advanced-only>
      <span class="label">Modes</span>
      <span class="swgroup">
        <label class="switch">use_proxy<input type="checkbox" name="proxy" value="yes" {{if .Proxy}}checked{{end}}><i></i></label>
        <label class="switch">offline<input type="checkbox" name="offline" value="on" {{if .Offline}}checked{{end}}><i></i></label>
        <label class="switch">CBM-UI<input type="checkbox" name="cbm_ui" value="on" {{if .CBMUI}}checked{{end}}><i></i></label>
        <label class="switch">HIL<input type="checkbox" name="hil" value="on" {{if .HIL}}checked{{end}}><i></i></label>
        <label class="switch">dictation<input type="checkbox" name="use_stt" value="on" {{if .STT}}checked{{end}}><i></i><input type="hidden" name="use_stt_present" value="1"></label>
      </span>
      <div class="sub">start.sh flags — apply on next start (presets, not pinning: the stored config stays the truth).</div>
    </div>
    <div class="row" data-row="search" data-advanced-only>
      <span class="label">Search</span>
      <span class="edotsel">
        <label>none<input type="radio" name="search_mode" value="none" {{if eq .SearchMode "none"}}checked{{end}}></label>
        <label>websearch<input type="radio" name="search_mode" value="websearch" {{if eq .SearchMode "websearch"}}checked{{end}}></label>
        <label>searxng<input type="radio" name="search_mode" value="searxng" {{if eq .SearchMode "searxng"}}checked{{end}}></label>
      </span>
      <div class="sub">SearXNG runs as a companion service — searxng excludes the proxy and offline mode.</div>
    </div>
    <div class="row hint" data-row="presets">{{.PresetsHint}}</div>

    </fieldset>
    <div class="actions">
      <button type="submit" class="btn" data-role="wizard-create">Create Workspace</button>
    </div>
  </form>
</section>
{{end}}

{{define "confirm"}}
<div class="create-confirm" id="create-confirm" data-role="create-confirm">
  <h2>Create this workspace?</h2>
  {{if .Error}}<p class="form-error" role="alert">{{.Error}}</p>{{end}}
  <ul class="summary">
    {{range .Lines}}<li>{{.}}</li>{{end}}
  </ul>
  <form hx-post="{{.P}}workspaces" hx-target="#create-confirm" hx-swap="outerHTML">
    {{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
    <div class="actions">
      <button type="submit" class="btn" name="confirm" value="1">Create</button>
      <button type="submit" class="btn ghost" name="confirm" value="">Back</button>
    </div>
  </form>
</div>
{{end}}
`))

// wizardBundle is one chip of the bundle row.
type wizardBundle struct {
	Name     string
	Members  string // comma-joined member ids (the chip's preselect data)
	Pressed  bool
	BaseOnly bool // chat/pkm: the empty bundle renders "base only"
	Valid    bool
	Reason   string // why unselectable (missing/invalid member, bad name)
}

// wizardUnit is one checkbox of the toolchain multi-select.
type wizardUnit struct {
	ID      string
	Checked bool
	Cached  bool // the live marker: the unit's component image exists
	Valid   bool
	Reason  string // invalid units render disabled with their reason
}

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

	// The toolchain selection (spec §9 WP-3): the multi-select is the
	// payload — canonicalized sorted per ADR 0042; the bundle row is its
	// chip preselect (Custom when it matches no bundle); pkm pins it to
	// the empty set. nil (no units row posted) resolves to the §6
	// default (the swdev bundle's members).
	Selected     []string
	UnitsPresent bool // the units row rendered (units_present) — gates the default
	Bundles      []wizardBundle
	Units        []wizardUnit
	Custom       bool // the selection matches no bundle
	SelCount     int
	UnitCount    int
	SetMissing   bool   // the selection's set image is missing
	BuildCost    string // the honest cost line (counts, no time estimates)
	BuildLater   bool

	VCS      string
	Provider string

	StartOption string
	Proxy       bool
	SearchMode  string
	Offline     bool
	CBMUI       bool
	HIL         bool
	STT         bool
	STTPresent  bool // the row rendered (use_stt_present) — gates the patch

	Trackers    []catalogs.Tracker
	Providers   []catalogs.Provider
	PresetsHint string

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
// agent code, the swdev bundle preselected, start web, proxy off,
// searxng, offline / CBM-UI off. HIL presets on iff the selection
// carries a hardware unit (client-side preset — the checkbox posts the
// preset).
func wizardDefaults() wizardForm {
	return wizardForm{
		Agent:       "code",
		VCS:         "none",
		Provider:    "none",
		StartOption: "web",
		SearchMode:  "searxng",
		STT:         true, // dictation preset ON (the sandboxcfg.Create default)
	}
}

// handleNewWorkspaceGET serves GET /workspaces/new: the blank wizard
// form with the catalog feeds and the live toolchain discovery.
func (s *Server) handleNewWorkspaceGET(w http.ResponseWriter, r *http.Request) {
	f := wizardDefaults()
	s.renderWizard(w, f)
}

// renderWizard renders the form fragment with the current feeds and
// discovery (fresh per call, data map §2). Retired-kind entries
// (ADR-0038) never reach the pick list.
func (s *Server) renderWizard(w http.ResponseWriter, f wizardForm) {
	cat := s.wizardCatalog()
	wizardResolveSelection(&f, cat)
	s.fillWizardSelection(&f, cat)
	if f.Agent != "pkm" { // pkm pins the set — the build-plan row hides
		f.SetMissing, f.BuildCost, _ = s.wizardBuildPlan(cat, f.Selected)
	}
	cat0 := catalogs.Read(s.opts.Home)
	f.P = s.pathPrefix()
	f.PresetsHint = PresetsHint
	for _, t := range cat0.Trackers {
		if catalogs.SelectableKind(t.Kind) {
			f.Trackers = append(f.Trackers, t)
		}
	}
	f.Providers = cat0.Providers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wizardTemplate.ExecuteTemplate(w, "wizard", f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// wizardCatalog is the live toolchain discovery over the server's
// toolchains dir (fresh per call — nothing cached, spec §2).
func (s *Server) wizardCatalog() discovery.Catalog {
	return discovery.Discover(s.toolchainsRoot())
}

// wizardResolveSelection applies the payload rules: pkm pins the
// selection to the empty set (ADR-0027); a post without the units row
// (and the blank GET) resolves to the §6 default — the swdev bundle's
// members, else the first valid bundle.
func wizardResolveSelection(f *wizardForm, cat discovery.Catalog) {
	if f.Agent == "pkm" {
		f.Selected = nil
		return
	}
	if f.Selected == nil && !f.UnitsPresent {
		f.Selected = wizardDefaultSelection(cat)
	}
}

// wizardDefaultSelection preselects the swdev bundle's members (§6),
// else the first valid bundle's, else none.
func wizardDefaultSelection(cat discovery.Catalog) []string {
	if b, ok := cat.Bundle(wizardBundleDefault); ok && b.Valid {
		return b.Members
	}
	for _, b := range cat.Bundles {
		if b.Valid {
			return b.Members
		}
	}
	return nil
}

// fillWizardSelection renders the bundle row's pressed state (exact
// member-match, else Custom — never auto-back) and the units
// multi-select's checked/cached/disabled states.
func (s *Server) fillWizardSelection(f *wizardForm, cat discovery.Catalog) {
	for _, b := range cat.Bundles {
		wb := wizardBundle{
			Name:     b.Name,
			Members:  strings.Join(b.Members, ","),
			BaseOnly: b.Valid && len(b.Members) == 0,
			Valid:    b.Valid,
			Reason:   b.Reason,
			Pressed:  b.Valid && slices.Equal(b.Members, f.Selected),
		}
		f.Bundles = append(f.Bundles, wb)
	}
	f.Custom = true
	for _, wb := range f.Bundles {
		if wb.Pressed {
			f.Custom = false
		}
	}
	for _, u := range cat.Units {
		f.Units = append(f.Units, wizardUnit{
			ID:      u.ID,
			Valid:   u.Valid,
			Reason:  u.Reason,
			Checked: slices.Contains(f.Selected, u.ID),
			Cached:  u.Valid && s.componentCached(u),
		})
	}
	f.SelCount = len(f.Selected)
	f.UnitCount = len(cat.Units)
}

// componentCached is the live cached marker: the unit's
// content-addressed component image exists (identity.ComponentTag + a
// per-tag podman query).
func (s *Server) componentCached(u discovery.Unit) bool {
	tag, err := identity.ComponentTag(filepath.Join(s.toolchainsRoot(), "base.Dockerfile"), u.ID, u.Path)
	return err == nil && discovery.ImageExistsTag(s.runner(), tag)
}

// setTagFor derives the selection's content-addressed set tag
// (identity.SetTag over the base recipe and the discovered recipes).
// ok=false names an underivable tag (an invalid member, no base
// recipe) — honestly rendered as a missing set image.
func (s *Server) setTagFor(cat discovery.Catalog, ids []string) (string, bool) {
	var members []identity.Member
	for _, id := range ids {
		u, ok := cat.Unit(id)
		if !ok || !u.Valid {
			return "", false
		}
		members = append(members, identity.Member{ID: id, RecipePath: u.Path})
	}
	tag, err := identity.SetTag(filepath.Join(s.toolchainsRoot(), "base.Dockerfile"), members)
	if err != nil {
		return "", false
	}
	return tag, true
}

// wizardBuildPlan keys the Build-plan row to set-tag existence (spec §9
// WP-3): the honest cost line carries counts — never time estimates.
// The summary is the confirm popup's "image build:" members summary.
func (s *Server) wizardBuildPlan(cat discovery.Catalog, ids []string) (missing bool, cost, summary string) {
	tag, ok := s.setTagFor(cat, ids)
	if ok && discovery.ImageExistsTag(s.runner(), tag) {
		return false, "", ""
	}
	n := 0
	for _, id := range ids {
		if u, found := cat.Unit(id); found && u.Valid && !s.componentCached(u) {
			n++
		}
	}
	switch {
	case len(ids) == 0:
		summary = "the base-only set image"
		cost = "the set image is missing — Create assembles it from the base image"
	case n == 0:
		summary = fmt.Sprintf("all %d toolchains are cached plus the set image", len(ids))
		cost = fmt.Sprintf("the set image is missing — all %d toolchains are cached, Create assembles the set image", len(ids))
	default:
		summary = fmt.Sprintf("%d of %d toolchains need building plus the set image", n, len(ids))
		cost = fmt.Sprintf("the set image is missing — Create builds %d of %d toolchains, then assembles the set image", n, len(ids))
	}
	return true, cost, summary
}

// runner resolves the command runner (nil → exec).
func (s *Server) runner() cmdrun.Runner {
	if s.deps != nil && s.deps.Run != nil {
		return s.deps.Run
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
		if r.PostForm.Has("confirm") { // the confirm popup's Back button —
			s.renderWizard(w, f) // re-render the form with the entered values
			return
		}
		if msg := s.validateWizard(r, f); msg != "" {
			f.Error = msg
			s.renderWizard(w, f)
			return
		}
		s.renderConfirm(w, r, f)
		return
	}
	// confirm=1: refuse while an op for this workspace is queued or
	// running (one-at-a-time per workspace, parallel across workspaces),
	// then launch the ONE create-chain task through the shared op queue.
	if msg := s.validateWizard(r, f); msg != "" {
		f.Error = msg
		s.renderWizard(w, f)
		return
	}
	if s.queue.BusyKey(f.Name) {
		f.Error = "A create chain for this workspace is already queued or running — wait for it to finish."
		s.renderWizard(w, f)
		return
	}
	chain := s.newCreateChain(r, f)
	chain.opID = s.queue.reserve()
	f.OpID = chain.opID
	f.Inert = true // the form stays visible but inert while the chain runs
	s.renderWizard(w, f)
	s.queue.Launch(webOp{
		ID:  chain.opID,
		Key: chain.name,
		Run: func(emit func(string)) error { return chain.runSteps(s, emit) },
		Done: func(err error) {
			if err != nil {
				s.hub.PublishWorkspaceOp(chain.opID, "failed")
			} else {
				s.hub.PublishWorkspaceOp(chain.opID, "done")
			}
		},
	})
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
// payload: the catalog entry's kind label, or "none" (an unknown pick
// — crafted "new" included — reads back as none, fail-soft).
func (s *Server) wizardVCSTracking(f wizardForm) string {
	if f.VCS == "none" {
		return "none"
	}
	var kind string
	if t, ok := s.wizardTracker(f.VCS); ok {
		kind = t.Kind
	}
	return trackerLabel(kind)
}

// wizardTracker finds a catalog tracker by display name (slug match);
// retired-kind entries never resolve (ADR-0038) — a crafted pick of
// one reads back as none, fail-soft like an unknown name.
func (s *Server) wizardTracker(name string) (catalogs.Tracker, bool) {
	slug := catalogs.Slug(name)
	for _, t := range catalogs.Read(s.opts.Home).Trackers {
		if catalogs.Slug(t.Name) == slug && catalogs.SelectableKind(t.Kind) {
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
	summary := "base only"
	if len(f.Selected) > 0 {
		summary = strings.Join(f.Selected, ", ")
	}
	lines := []string{
		"name: " + f.Name,
		"path: " + s.wizardRoot(f.Name),
		"toolchains: " + summary,
	}
	if f.Agent == "chat" {
		lines = append(lines, "agent: chat")
	}
	lines = append(lines,
		"search: "+f.SearchMode,
		"VCS tracking: "+s.wizardVCSTracking(f),
	)
	if p, ok := s.wizardProvider(f.Provider); ok {
		lines = append(lines, "AI provider: "+p.Name)
	} else {
		lines = append(lines, "AI provider: none")
	}
	if _, cost, sum := s.wizardBuildPlan(s.wizardCatalog(), f.Selected); cost != "" {
		decision := "Build later"
		if !f.BuildLater {
			decision = "Build now"
		}
		lines = append(lines, "image build: "+decision+" — "+sum)
	}
	fields := formFields{}
	for _, k := range sortedFormKeys(r.PostForm) {
		if k == "confirm" {
			continue
		}
		// repeated keys (the toolchain multi-select) round-trip every
		// value, never just the first
		for _, v := range r.PostForm[k] {
			fields = append(fields, formField{Name: k, Value: v})
		}
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
	f.Selected = canonicalIDs(r.PostForm["toolchain"])
	f.UnitsPresent = r.PostFormValue("units_present") == "1"
	wizardResolveSelection(&f, s.wizardCatalog())
	f.BuildLater = r.PostFormValue("build") == "later"
	f.VCS = r.PostFormValue("vcs")
	if f.VCS == "" {
		f.VCS = "none"
	}
	f.Provider = r.PostFormValue("provider")
	if f.Provider == "" {
		f.Provider = "none"
	}
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
	f.STT = r.PostFormValue("use_stt") == "on" // absent (row hidden AND stripped) keeps the ON default
	f.STTPresent = r.PostFormValue("use_stt_present") == "1"
	return f
}

// canonicalIDs dedupes and sorts the posted toolchain ids — the
// canonical order of every stored id list (ADR 0042).
func canonicalIDs(vals []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vals {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Strings(out)
	return out
}

// validateWizard checks the payload; "" when valid. Mirrors the TUI's
// validateInputs plus the ADR-0026 exclusions start.sh hard-exits on.
func (s *Server) validateWizard(r *http.Request, f wizardForm) string {
	if !newworkspace.NameRe.MatchString(f.Name) {
		return "Invalid workspace name — use [a-zA-Z0-9_-]+."
	}
	if f.FromRepo && f.RepoURL == "" {
		return "Repo URL is required for a From Repo workspace."
	}
	// every posted id must be a discovered, valid unit (spec §8's
	// validation rule at the wizard's write path)
	cat := s.wizardCatalog()
	for _, id := range f.Selected {
		if u, ok := cat.Unit(id); !ok || !u.Valid {
			return "unknown toolchain '" + id + "' — choose a listed one"
		}
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
	if f.Provider != "none" && !providerid.ValidToken(r.PostFormValue("provider_token")) {
		return "Token must not be empty or contain whitespace/newlines."
	}
	return ""
}

// registry returns the Deps registry view.
func (s *Server) registry() *registry.Registry {
	return s.deps.Registry
}

// wizardRoot derives the workspace root from the name — the default
// workspace path base + the name (the wizard has no path row; §6).
// The slug is newworkspace's canonical PathSlug (bash-rule parity).
func (s *Server) wizardRoot(name string) string {
	base := s.deps.DefaultWorkspacePath
	if base == "" {
		base = s.opts.Home + "/oc-sandbox"
	}
	return registry.CanonicalPath(base + "/" + newworkspace.PathSlug(name))
}

// --- the create chain (ONE task: image build → init → configure) ------------

// editionHue is the template's handle on the Signal hue map — the
// per-edition hues died with the stage world (toolchain-sets WP-5):
// every dot tints with the neutral accent until the wizard/settings
// re-key lands.
func editionHue(name string) string {
	return "var(--accent)"
}

// createChain is the captured payload of one confirmed create — every
// request-scoped value (tokens included) is read before the chain goes
// to its own goroutine.
type createChain struct {
	opID int
	name string
	root string

	fromRepo   bool
	repoURL    string
	agent      string   // code | chat | pkm
	toolchains []string // the posted selection (sorted; pkm pinned empty)

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
	stt    bool // the dictation preset (use_stt, NOT a container mode)
	// sttPresent carries the wizard row's rendered sentinel (D9): the
	// patch writes use_stt only when the row rendered, so a Standard
	// post without the fields keeps the Create default (never re-imposes).
	sttPresent bool
	build      bool // Build now decision (the row only drives visibility)
}

// newCreateChain captures the confirmed payload (fresh image/selection
// decision re-checked here, like the TUI's create()).
func (s *Server) newCreateChain(r *http.Request, f wizardForm) *createChain {
	c := &createChain{
		name:       f.Name,
		root:       s.wizardRoot(f.Name),
		fromRepo:   f.FromRepo,
		repoURL:    f.RepoURL,
		agent:      f.Agent,
		toolchains: f.Selected,
		vcsLabel:   s.wizardVCSTracking(f),
		vcsToken:   r.PostFormValue("vcs_token"),

		providerToken: r.PostFormValue("provider_token"),
		modes:         wizardModes(f),
		start:         f.StartOption,
		search:        f.SearchMode,
		proxy:         f.Proxy,
		stt:           f.STT,
		sttPresent:    f.STTPresent,
	}
	_, cost, _ := s.wizardBuildPlan(s.wizardCatalog(), c.toolchains)
	c.build = cost != "" && !f.BuildLater
	// resolve the picked catalog entries (the wizard only picks — the
	// entries live in Settings › VCS Tracker / AI-Provider); an unknown
	// pick materializes nothing (none stays the default, fail-soft)
	var tracker catalogs.Tracker
	var provider catalogs.Provider
	if t, ok := s.wizardTracker(f.VCS); ok {
		tracker = t
		c.vcsKind = t.Kind
		c.vcsHost = t.Host
	}
	c.gitName, c.gitEmail = strings.TrimSpace(tracker.GitName), strings.TrimSpace(tracker.GitEmail)
	if p, ok := s.wizardProvider(f.Provider); ok {
		provider = p
	}
	if provider.Name != "" {
		c.providerName = provider.Name
		c.providerEndpoint = provider.Endpoint
		c.providerID = providerid.Slug(provider.Name)
	}
	return c
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

// runSteps executes the chain, streaming every step into the log pane
// under the form via the op queue's emit (SSE line events keyed by the
// op ID); the scripts are invoked exactly as the TUI invokes them —
// `bash <script> <args>` — through the runner seam (ADR-0007). The
// returned error is the chain outcome (Done publishes the workspace op
// event).
func (c *createChain) runSteps(s *Server, emit func(string)) error {
	emitf := func(format string, a ...any) { emit(fmt.Sprintf(format, a...)) }
	runScript := func(script string, args ...string) error {
		out, err := s.runner().Output("bash", append([]string{script}, args...)...)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		return err
	}
	fail := func(msg string) error {
		emit(msg)
		return errors.New(msg)
	}

	// image build (only when the set image is missing and Build now
	// won): build-images.sh --set <ids> builds the missing components
	// and assembles the set image (lazy, cached-skip per tag); "" is
	// the base-only set (#206 re-keyed the interim build-container.sh
	// call onto the builder, spec §9 WP-3).
	if c.build {
		ids := strings.Join(c.toolchains, ",")
		if ids == "" {
			emit("==> Building base-only set image")
		} else {
			emitf("==> Building toolchain set (%s)", strings.Join(c.toolchains, ", "))
		}
		// The §7 preflight (WP-8 #212) refuses first on a low-disk or
		// offline host — the chain stops before any build call.
		if plan, ok := s.builderPlanSet(c.toolchains); ok {
			if err := s.preflightBuild(plan); err != nil {
				return fail(err.Error())
			}
		}
		if err := runScript(s.deps.BuildImagesScript, "--set", ids); err != nil {
			return fail("Build failed — the set image is still unavailable. Press Create Workspace to retry.")
		}
		if tag, ok := s.setTagFor(s.wizardCatalog(), c.toolchains); !ok ||
			!discovery.ImageExistsTag(s.runner(), tag) {
			return fail("Build failed — the set image is still unavailable. Press Create Workspace to retry.")
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
	if err := runScript(s.deps.InitScript, initArgs...); err != nil {
		return fail("Workspace initialization failed: " + err.Error() + " — no workspace state was registered.")
	}

	// sandbox_config.json: the Create payload + the atomic patches
	cfgPath := sandboxcfg.Path(c.root)
	providerID := c.providerID
	if providerID == "" {
		providerID = "none"
	}
	// The create payload is the posted, validated selection itself —
	// materialized as the sorted id list (spec §10, ADR 0042).
	if err := sandboxcfg.Create(s.opts.Home, cfgPath, c.toolchains, c.modes, c.start, providerID, c.search); err != nil {
		return fail("sandbox_config.json could not be created: " + err.Error())
	}
	if err := c.patchConfig(s, s.opts.Home, cfgPath); err != nil {
		return fail("sandbox_config.json could not be patched: " + err.Error())
	}

	// configure-workspace.sh in credential order (TUI buildCreds parity)
	if c.vcsKind != "" && strings.TrimSpace(c.vcsToken) != "" {
		emit("==> Configuring credentials")
		if err := runScript(s.deps.ConfigureScript, vcsCredArgs(c)...); err != nil {
			return fail("Workspace configuration failed — " + err.Error())
		}
	}
	if c.vcsKind != "" && c.gitName != "" && c.gitEmail != "" {
		emit("==> Configuring credentials")
		if err := runScript(s.deps.ConfigureScript, "git-identity", c.root, "--name", c.gitName, "--email", c.gitEmail); err != nil {
			return fail("Workspace configuration failed — " + err.Error())
		}
	}
	if c.providerName != "" {
		emit("==> Configuring credentials")
		if err := runScript(s.deps.ConfigureScript, "ai-provider", c.root, "--name", c.providerName,
			"--endpoint", c.providerEndpoint, "--token", c.providerToken); err != nil {
			return fail("Workspace configuration failed — " + err.Error())
		}
		if err := runScript(s.deps.ConfigureScript, "opencode-config", c.root, "--id", c.providerID,
			"--name", c.providerName, "--endpoint", c.providerEndpoint); err != nil {
			return fail("Workspace configuration failed — " + err.Error())
		}
	}

	// the registry write (missing workspaces.json seeds once)
	reg := s.registry()
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
		return fail("Workspace not registered: " + err.Error())
	}
	emit("Workspace created successfully.")
	return nil
}

// patchConfig applies the field patches via the atomic paths
// (newworkspace.patchConfig parity).
func (c *createChain) patchConfig(s *Server, home, cfgPath string) error {
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
	// Dictation (#167, D9): the Create payload seeds use_stt true; the
	// wizard's patch runs only when the row rendered — a Standard post
	// without the fields never re-imposes (and never pins false).
	if c.sttPresent {
		if err := sandboxcfg.PatchBool(home, cfgPath, "use_stt", c.stt); err != nil {
			return err
		}
	}
	if c.proxy {
		if err := sandboxcfg.PatchBool(home, cfgPath, "use_proxy", true); err != nil {
			return err
		}
		// Seed the workspace allowlist from shared (ADR 0034,
		// best-effort — the member start re-seeds).
		_ = s.seedWorkspaceAllowlist(c.root)
		return nil
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
// host-from-entry instead of the free-form row). The retired "other"
// kind never resolves (ADR-0038), so no custom case exists here — the
// retained writer for grandfathered workspaces lives in wssettings.
func vcsCredArgs(c *createChain) []string {
	switch c.vcsKind {
	case catalogs.KindGitHub:
		return []string{"vcs-credentials", "github", c.root, "--token", c.vcsToken}
	case catalogs.KindGitLab:
		return []string{"vcs-credentials", "gitlab", c.root, "--token", c.vcsToken, "--host", "gitlab.com"}
	case catalogs.KindGitLabSel:
		return []string{"vcs-credentials", "gitlab", c.root, "--token", c.vcsToken, "--host", c.vcsHost}
	}
	return nil
}
