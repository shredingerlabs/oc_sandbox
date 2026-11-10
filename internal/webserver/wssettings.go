// Workspace Settings screen (#157, spec §7): the per-workspace settings
// screen, reachable from a card's settings icon and via the menu — the
// menu entry (GET /workspace-settings) carries the letter-tinted
// workspace selector dropdown with the running state, and a card jump
// (GET /workspaces/{name}/settings) enters preselected. Standard rows:
// AI-Provider (catalog prefill), VCS Tracking (with the
// replace-credentials confirm), remove-workspace-from-list behind the
// red confirm; Advanced rows per the §6 matrix — Edition (switchable,
// next-start apply, image/mismatch rules unchanged), Modes incl. the
// HIL row for every edition, Start option triad, Proxy, Search mode
// triad (searxng preselected; an absent legacy key still reads
// websearch). Rows the matrix hides are not rendered at all — Standard
// hides, never re-imposes; the stored sandbox_config.json stays the
// single truth, so cross-editing Standard↔Advanced in either order just
// works. The first-run setup retry stays a link only (#161 owns POST
// …/setup/retry). Web-style pair-link handling belongs to the Open
// screen (#153) and is not duplicated here.
package webserver

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// wsSettingsRouteNames are the screen's URL identities (§9): the
// registry workspace name is the URL identity; the menu entry is the
// name-less alias (MRU workspace preselected).
const wsSettingsMenuPath = "workspace-settings"

// settingsScreen is the presentation matrix identity of this screen.
const settingsScreen = "ws-settings"

// wsSelOpt is one entry of the workspace selector dropdown.
type wsSelOpt struct {
	Name    string
	Running bool
	Sel     bool
}

// provEntryView is one provider entry row of the AI-Provider section.
type provEntryView struct {
	ID       string
	Name     string
	Endpoint string
	Orphan   bool // stored credential missing — read-only (ADR 0025)
}

// provConfirmView is the Keep/Replace round-trip (§9 replace-credentials
// confirm, TUI parity): the pending write round-trips in hidden fields.
type provConfirmView struct {
	ID     string
	Action string // add | edit
	Orig   string // edit: the id being edited (renamed away from)
	Name   string
	Endpoint string
	Token  string
}

// wsSettingsView is the template data of the whole screen fragment.
type wsSettingsView struct {
	P     string // tokenized path prefix
	Name  string
	Mode  string
	Error string

	Hue        string // Signal hue of the edited workspace's edition
	Letter     string // first letter of the workspace name (selector avatar)
	Running    bool
	Workspaces []wsSelOpt

	// AI-Provider section (Standard row)
	Providers   []provEntryView
	ProvCatalog []catalogs.Provider
	ProvConfirm *provConfirmView

	// VCS Tracking section (Standard row)
	VCSCatalog    []catalogs.Tracker
	TrackerKinds  []string
	VCSKind       string // preselected kind (from stored vcs_tracking)
	VCSHost       string // prefill (catalog entry host)
	VCSHasCreds   bool   // the tracker's host credential file exists
	VCSTrackingSt string // stored vcs_tracking label

	// Advanced rows (§6 matrix; hidden rows are not rendered)
	Edition      string
	Editions     []string
	ImageMissing bool
	StartOption  string
	Proxy        bool
	SearchMode   string
	Offline      bool
	CBMUI        bool
	HIL          bool

	SettingsForm bool // ≥1 advanced row visible → the next-start form renders

	PresetsHint string
	SetupDone   bool // first-run setup flags (the retry link's context)
	OpID        int  // in-flight op id — the log pane's data-op
	LogLines    []string
}

// visRow reports whether the (screen, key) row shows in the display
// mode — the §6 matrix is the single visibility authority.
func visRow(mode, key string) bool {
	return RowVisible(mode, settingsScreen, key)
}

// visRowFor is the template's handle on the matrix visibility query (a
// template func — the mode travels with the view).
func visRowFor(mode, key string) bool { return visRow(mode, key) }

var wsSettingsTemplate = template.Must(template.New("wssettings").
	Funcs(template.FuncMap{"vis": visRowFor}).
	Parse(`
{{define "ws-settings-empty"}}
<section class="wssettings" id="ws-settings" data-screen="ws-settings">
  <div class="pagehead"><div><h2>Workspace Settings</h2></div></div>
  <p class="empty" data-empty="workspaces">No workspaces registered</p>
</section>
{{end}}

{{define "ws-settings"}}
<section class="wssettings" id="ws-settings" data-screen="ws-settings" data-ws="{{.Name}}">
  <div class="pagehead">
    <div>
      <h2>Workspace Settings</h2>
      <p class="desc">Changes apply on <b>next start</b> — a running workspace keeps its current settings until stopped.</p>
    </div>
    <span class="chip{{if .Running}} on{{end}}" data-chip="state">{{if .Running}}running{{else}}stopped{{end}}</span>
  </div>
  {{if .Error}}<p class="form-error" role="alert">{{.Error}}</p>{{end}}

  <div class="panel" data-panel="selector">
    <div class="selrow">
      <span class="avatar"{{if .Hue}} style="--ed:{{.Hue}}"{{end}}>{{.Letter}}</span>
      <div>
        <label class="flabel" for="ws-settings-select">Editing workspace</label>
        <select id="ws-settings-select" data-role="ws-selector" data-ws="{{.Name}}">
          {{range .Workspaces}}<option value="{{.Name}}" {{if .Sel}}selected{{end}}>{{.Name}}{{if .Running}} — running{{end}}</option>{{end}}
        </select>
        <div class="field-hint">Cards on Workspace › Open jump straight to their settings.</div>
      </div>
    </div>
  </div>

  <div class="panel">
    <div class="row" data-row="provider">
      <div class="rowhead"><span class="label">AI-Provider</span></div>
      <div class="entries" data-role="ws-providers">
        {{if .Providers}}
        {{range .Providers}}
        <div class="entry" data-entry="{{.ID}}"{{if .Orphan}} data-orphan{{end}}>
          <div class="entry-head"><strong>{{.Name}}</strong>
            {{if .Orphan}}<span class="badge warn">stored credential missing</span>{{end}}</div>
          <div class="entry-sub">{{.Endpoint}}</div>
          {{if not .Orphan}}
          <details class="entry-edit">
            <summary>Edit</summary>
            <form hx-post="{{$.P}}workspaces/{{$.Name}}/providers" hx-target="#ws-settings" hx-swap="outerHTML">
              <input type="hidden" name="action" value="edit">
              <input type="hidden" name="orig" value="{{.ID}}">
              <label>Display name <input name="name" value="{{.Name}}"></label>
              <label>Endpoint <input name="endpoint" value="{{.Endpoint}}"></label>
              <label>Token <input type="password" name="token" value="" autocomplete="off" placeholder="(keep stored credential)"></label>
              <button type="submit">Save</button>
            </form>
          </details>
          <form class="entry-remove" hx-post="{{$.P}}workspaces/{{$.Name}}/providers" hx-target="#ws-settings" hx-swap="outerHTML"
                data-confirm-popup data-confirm-title="Remove provider {{.ID}}?"
                data-confirm-question="Deletes its stored credential and opencode.json section."
                data-confirm-label="Remove">
            <input type="hidden" name="action" value="remove">
            <input type="hidden" name="orig" value="{{.ID}}">
            <button type="submit" class="danger">Remove</button>
          </form>
          {{end}}
        </div>
        {{end}}
        {{else}}
        <p class="empty" data-empty="providers">no entries — add a provider below</p>
        {{end}}
      </div>
      <form class="add-form" hx-post="{{.P}}workspaces/{{.Name}}/providers" hx-target="#ws-settings" hx-swap="outerHTML">
        <input type="hidden" name="action" value="add">
        <label>Prefill from catalog
          <select name="prefill" data-role="provider-prefill">
            <option value="">(free-form)</option>
            {{range .ProvCatalog}}<option value="{{.Name}}" data-endpoint="{{.Endpoint}}">{{.Name}}</option>{{end}}
          </select>
        </label>
        <label>Display name <input name="name"></label>
        <label>Endpoint <input name="endpoint" placeholder="https://api.example.com/v1"></label>
        <label>Token <input type="password" name="token" autocomplete="off"></label>
        <button type="submit">Add provider</button>
      </form>
    </div>

    {{if .ProvConfirm}}
    <div class="confirm-block" data-role="provider-confirm">
      <p class="warn">Credentials for '{{.ProvConfirm.ID}}' exist.</p>
      <p class="field-hint">Keep existing keeps the stored token.</p>
      <form hx-post="{{.P}}workspaces/{{.Name}}/providers" hx-target="#ws-settings" hx-swap="outerHTML">
        <input type="hidden" name="action" value="{{.ProvConfirm.Action}}">
        <input type="hidden" name="orig" value="{{.ProvConfirm.Orig}}">
        <input type="hidden" name="name" value="{{.ProvConfirm.Name}}">
        <input type="hidden" name="endpoint" value="{{.ProvConfirm.Endpoint}}">
        <input type="hidden" name="token" value="{{.ProvConfirm.Token}}">
        <button type="submit" name="decision" value="keep">Keep existing</button>
        <button type="submit" name="decision" value="replace">Replace</button>
        <button type="submit" name="decision" value="">Cancel</button>
      </form>
    </div>
    {{end}}

    <div class="row" data-row="vcs">
      <div class="rowhead"><span class="label">VCS Tracking</span></div>
      <form class="add-form" hx-post="{{.P}}workspaces/{{.Name}}/vcs-credentials" hx-target="#ws-settings" hx-swap="outerHTML">
        <label>Prefill from catalog
          <select name="prefill" data-role="vcs-prefill">
            <option value="">(free-form)</option>
            {{range .VCSCatalog}}<option value="{{.Name}}" data-kind="{{.Kind}}" data-host="{{.Host}}">{{.Name}}</option>{{end}}
          </select>
        </label>
        <label>Tracker
          <select name="kind">
            <option value="none" {{if eq .VCSKind ""}}selected{{end}}>none</option>
            {{range .TrackerKinds}}<option value="{{.}}" {{if eq . $.VCSKind}}selected{{end}}>{{.}}</option>{{end}}
          </select>
        </label>
        <label>Host <input name="host" value="{{.VCSHost}}" placeholder="gitlab.example.com"> <span class="field-hint">required for own GitLab / others</span></label>
        <label>Token <input type="password" name="token" autocomplete="off"></label>
        <div class="actions">
          <button type="submit" name="replace" value="1"
            {{if .VCSHasCreds}}data-confirm-popup data-confirm-title="Replace credentials?"
              data-confirm-question="Credentials for this tracker already exist — replace them with the typed token?"
              data-confirm-label="Replace"{{end}}>Save</button>
          {{if .VCSHasCreds}}<button type="submit" name="replace" value="keep">Keep credentials</button>{{end}}
        </div>
        {{if .VCSHasCreds}}<div class="field-hint">credentials exist — Save replaces them after the confirm</div>{{end}}
      </form>
    </div>

    {{if .SettingsForm}}
    <form class="settings-form" hx-post="{{.P}}workspaces/{{.Name}}/settings" hx-target="#ws-settings" hx-swap="outerHTML">
      {{if vis .Mode "edition"}}
      <div class="row" data-row="edition">
        <span class="label">Edition</span>
        <select name="edition">
          {{range .Editions}}<option value="{{.}}" {{if eq . $.Edition}}selected{{end}}>{{.}}</option>{{end}}
        </select>
        {{if .ImageMissing}}<span class="field-hint warn">image missing — build it under Settings › Images</span>{{end}}
        <span class="field-hint">switchable — applies on next start</span>
      </div>
      {{end}}
      {{if vis .Mode "start_option"}}
      <div class="row" data-row="start_option">
        <span class="label">Start option</span>
        <span class="opts">
          <label><input type="radio" name="start_option" value="console" {{if eq .StartOption "console"}}checked{{end}}> console</label>
          <label><input type="radio" name="start_option" value="opencode" {{if eq .StartOption "opencode"}}checked{{end}}> OpenCode TUI</label>
          <label><input type="radio" name="start_option" value="web" {{if or (eq .StartOption "web") (eq .StartOption "")}}checked{{end}}> Web</label>
        </span>
      </div>
      {{end}}
      {{if vis .Mode "proxy"}}
      <div class="row" data-row="proxy">
        <span class="label">Proxy</span>
        <span class="opts">
          <label><input type="radio" name="proxy" value="no" {{if not .Proxy}}checked{{end}}> off</label>
          <label><input type="radio" name="proxy" value="yes" {{if .Proxy}}checked{{end}}> on</label>
        </span>
      </div>
      {{end}}
      {{if vis .Mode "search_mode"}}
      <div class="row" data-row="search_mode">
        <span class="label">Search mode</span>
        <span class="opts">
          <label><input type="radio" name="search_mode" value="none" {{if eq .SearchMode "none"}}checked{{end}}> none</label>
          <label><input type="radio" name="search_mode" value="websearch" {{if eq .SearchMode "websearch"}}checked{{end}}> websearch</label>
          <label><input type="radio" name="search_mode" value="searxng" {{if eq .SearchMode "searxng"}}checked{{end}}> searxng</label>
        </span>
      </div>
      {{end}}
      {{if vis .Mode "offline"}}
      <div class="row" data-row="offline">
        <span class="label">Offline</span>
        <label><input type="checkbox" name="offline" value="on" {{if .Offline}}checked{{end}}> on</label>
      </div>
      {{end}}
      {{if vis .Mode "cbm_ui"}}
      <div class="row" data-row="cbm_ui">
        <span class="label">CBM-UI</span>
        <label><input type="checkbox" name="cbm_ui" value="on" {{if .CBMUI}}checked{{end}}> on</label>
      </div>
      {{end}}
      {{if vis .Mode "hil"}}
      <div class="row" data-row="hil">
        <span class="label">HIL</span>
        <label><input type="checkbox" name="hil" value="on" {{if .HIL}}checked{{end}}> on</label>
        <span class="field-hint">human-in-the-loop — every edition, free toggle</span>
      </div>
      {{end}}
      <div class="actions"><button type="submit">Save settings</button></div>
      <div class="field-hint">next-start apply — a running container is never reconfigured</div>
    </form>
    {{end}}

    <div class="row hint" data-row="presets">{{.PresetsHint}}</div>

    <div class="row" data-row="setup">
      <span class="label">First-run setup</span>
      <span class="cell">{{if .SetupDone}}complete{{else}}incomplete — retry from here{{end}}</span>
      <button class="btn" data-act="setup-retry" title="first-run setup recovery">Retry setup…</button>
    </div>

    <div class="row danger-row" data-row="remove">
      <div class="rowhead"><span class="label danger">Remove workspace</span>
        <span class="field-hint">unregisters the workspace; files, configs and the container stay on disk.</span></div>
      <form hx-post="{{.P}}workspaces/{{.Name}}/remove-from-list" hx-target="#ws-settings" hx-swap="outerHTML"
            data-confirm-popup data-confirm-title="Remove {{.Name}} from the workspace list?"
            data-confirm-question="Removes the registry entry only — files, configs and the container stay on disk. A running container keeps running untracked."
            data-confirm-label="Remove">
        <button type="submit" class="danger">Remove from list…</button>
      </form>
    </div>
  </div>

  {{if or .LogLines (gt .OpID 0)}}
  <div class="logpane" data-role="log" data-op-id="{{.OpID}}">{{range .LogLines}}<div class="logline">{{.}}</div>{{end}}</div>
  {{end}}
</section>
{{end}}
`))

// handleWsSettingsGET serves GET /workspaces/{name}/settings — the
// screen fragment with that workspace preselected (the card settings
// icon's jump, §7).
func (s *Server) handleWsSettingsGET(w http.ResponseWriter, r *http.Request) {
	if !s.renderWsSettingsScreen(w, r.PathValue("name"), "") {
		http.NotFound(w, r)
	}
}

// handleWsSettingsMenu serves GET /workspace-settings — the menu entry:
// no name, so the most recent workspace jumps in preselected (§7); an
// empty registry renders the explicit empty state, never a dead end.
func (s *Server) handleWsSettingsMenu(w http.ResponseWriter, r *http.Request) {
	entries, _ := s.regView().AllOrdered()
	if len(entries) == 0 {
		s.renderWsSettingsEmpty(w)
		return
	}
	s.renderWsSettingsScreen(w, entries[0].Name, "")
}

// renderWsSettingsEmpty renders the empty-state screen fragment.
func (s *Server) renderWsSettingsEmpty(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wsSettingsTemplate.ExecuteTemplate(w, "ws-settings-empty", s.pathPrefix()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderWsSettingsScreen assembles and renders the fragment for one
// workspace; unknown names render nothing and return false (the caller
// refuses with 404 — the registry name is the URL identity, §9).
func (s *Server) renderWsSettingsScreen(w http.ResponseWriter, name, errMsg string) bool {
	ws, err := s.lookupWorkspace(name)
	if err != nil {
		return false
	}
	mode := s.displayMode()
	cfg, cfgErr := readSandboxCfg(ws.Path)

	v := wsSettingsView{
		P:            s.pathPrefix(),
		Name:         ws.Name,
		Mode:         mode,
		Error:        errMsg,
		PresetsHint:  PresetsHint,
		TrackerKinds: catalogs.TrackerKinds,
		SearchMode:   sandboxcfg.SearchWebsearch, // absent legacy key reads websearch (§6)
		StartOption:  "web",
	}
	if cfgErr == nil {
		v.Edition = cfg.ContainerEdition
		v.StartOption = cfg.StartOption
		v.Proxy = cfg.UseProxy
		v.SearchMode = cfg.SearchMode
		v.VCSTrackingSt = cfg.VCSTracking
		v.Running = ws.ContainerStatus == "running"
		for _, m := range cfg.ContainerModes {
			switch m {
			case "offline":
				v.Offline = true
			case "cbm_ui":
				v.CBMUI = true
			case "hil_mode":
				v.HIL = true
			}
		}
	}
	v.VCSKind = trackerKindForLabel(v.VCSTrackingSt)
	v.Hue = editionFor(v.Edition).Hue
	v.Letter = wsLetter(ws.Name)
	v.SetupDone = cfgErr == nil && cfg.SetupComplete

	// Selector dropdown: every registered workspace, running state shown.
	entries, _ := s.regView().AllOrdered()
	for _, e := range entries {
		v.Workspaces = append(v.Workspaces, wsSelOpt{
			Name: e.Name, Running: e.ContainerStatus == "running", Sel: e.Name == ws.Name,
		})
	}

	// Advanced rows: the edition list comes fresh from the Dockerfile
	// registry (discovery, never cached); the image marker mirrors the
	// next-start rules (unchanged — the start op refuses a missing image).
	for _, k := range []string{"edition", "start_option", "proxy", "search_mode", "offline", "cbm_ui", "hil"} {
		if visRow(mode, k) {
			v.SettingsForm = true
			if k == "edition" {
				dockerfile, dockerfileCustom := "", ""
				if s.deps != nil {
					dockerfile, dockerfileCustom = s.deps.Dockerfile, s.deps.DockerfileCustom
				}
				if eds, err := discovery.SelectableEditions(dockerfile, dockerfileCustom); err == nil {
					v.Editions = eds
				}
				if v.Edition != "" {
					v.ImageMissing = !discovery.ImageExists(s.runner(), v.Edition)
				}
			}
		}
	}

	// Catalog feeds (§8): prefill sources, token-free.
	cat := catalogs.Read(s.opts.Home)
	v.ProvCatalog = cat.Providers
	v.VCSCatalog = cat.Trackers

	// In-flight op: the log pane rides the queue like the busy card's.
	v.OpID = s.queue.CurrentOpID(registry.ContainerName(ws.ContainerID))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wsSettingsTemplate.ExecuteTemplate(w, "ws-settings", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	return true
}

// trackerKindForLabel maps a stored vcs_tracking label back onto the
// catalog kind slug the vcs-credentials route carries (the inverse of
// trackerLabel; "" for none/unknown).
func trackerKindForLabel(label string) string {
	switch label {
	case "github.com":
		return catalogs.KindGitHub
	case "gitlab.com":
		return catalogs.KindGitLab
	case "own GitLab":
		return catalogs.KindGitLabSel
	case "others":
		return catalogs.KindOther
	}
	return ""
}

// wsLetter is the selector avatar's letter: the workspace name's first
// letter, uppercased.
func wsLetter(name string) string {
	if name == "" {
		return "?"
	}
	return strings.ToUpper(name[:1])
}
