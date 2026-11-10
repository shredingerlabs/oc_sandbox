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
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/workspacesettings"
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
	if !s.renderWsSettingsScreen(w, r.PathValue("name"), "", nil) {
		http.NotFound(w, r)
	}
}

// handleWsSettingsPost serves POST /workspaces/{name}/settings (§9):
// the next-start values — edition, modes, start option, proxy, search —
// persisted into the stored sandbox_config.json as ONE atomic write
// (sandboxcfg.Rewrite, fixed-order). Only the fields the form carries
// are applied (Standard hides rows and never re-imposes: hidden rows
// post nothing); the merged write always starts from the stored truth,
// so cross-editing Standard↔Advanced in either order just works.
// Validations refuse what start.sh would hard-exit on (ADR-0026); a
// refusal or a busy queue re-renders the fragment with the message and
// writes nothing. Never live-apply: a running container is not touched.
func (s *Server) handleWsSettingsPost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ws, err := s.lookupWorkspace(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	key := registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(key) {
		s.renderWsSettingsScreen(w, name, "an operation is queued or running for "+name+" — wait for it to finish", nil)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	cfg, err := readSandboxCfg(ws.Path)
	if err != nil {
		s.renderWsSettingsScreen(w, name, "sandbox_config.json unreadable — "+err.Error(), nil)
		return
	}

	// Merge: stored truth first, posted rows on top (presets, not pinning).
	merged := sandboxcfg.Settings{
		Edition:     cfg.ContainerEdition,
		Modes:       cfg.ContainerModes,
		StartOption: cfg.StartOption,
		VCS:         cfg.VCSTracking,
		AIProvider:  cfg.AIProvider,
		UseProxy:    cfg.UseProxy,
		SearchMode:  cfg.SearchMode,
	}
	var offline, cbm bool
	if v := r.PostFormValue("edition"); v != "" {
		dockerfile, dockerfileCustom := "", ""
		if s.deps != nil {
			dockerfile, dockerfileCustom = s.deps.Dockerfile, s.deps.DockerfileCustom
		}
		eds, err := discovery.SelectableEditions(dockerfile, dockerfileCustom)
		if err != nil || !slices.Contains(eds, v) {
			s.renderWsSettingsScreen(w, name, "unknown edition — choose a listed one", nil)
			return
		}
		merged.Edition = v
	}
	if v := r.PostFormValue("start_option"); v != "" {
		if v != "console" && v != "opencode" && v != "web" {
			s.renderWsSettingsScreen(w, name, "invalid start option — console, OpenCode TUI or web", nil)
			return
		}
		merged.StartOption = v
	}
	if v := r.PostFormValue("proxy"); v != "" {
		if v != "no" && v != "yes" {
			s.renderWsSettingsScreen(w, name, "invalid proxy value", nil)
			return
		}
		merged.UseProxy = v == "yes"
	}
	if v := r.PostFormValue("search_mode"); v != "" {
		if !sandboxcfg.ValidSearchMode(v) {
			s.renderWsSettingsScreen(w, name, "invalid search mode", nil)
			return
		}
		merged.SearchMode = v
	}
	if r.PostFormValue("modes_present") == "1" { // the modes row rendered
		offline = r.PostFormValue("offline") == "on"
		cbm = r.PostFormValue("cbm_ui") == "on"
		hil := r.PostFormValue("hil") == "on"
		modes := []string{}
		if offline {
			modes = append(modes, "offline")
		}
		if cbm {
			modes = append(modes, "cbm_ui")
		}
		if hil {
			modes = append(modes, "hil_mode")
		}
		merged.Modes = modes
	}

	// Combination rules (ADR-0026, validateStartCfg parity): the refusals
	// start.sh hard-exits on never reach the stored file.
	if offline && cbm {
		s.renderWsSettingsScreen(w, name, "offline and cbm_ui are mutually exclusive — fix settings first", nil)
		return
	}
	if merged.SearchMode == sandboxcfg.SearchSearxng && (merged.UseProxy || offline) {
		s.renderWsSettingsScreen(w, name, "SearXNG needs network — it excludes the proxy and offline mode", nil)
		return
	}
	if err := sandboxcfg.Rewrite(s.opts.Home, sandboxcfg.Path(ws.Path), merged); err != nil {
		s.renderWsSettingsScreen(w, name, "sandbox_config.json could not be written: "+err.Error(), nil)
		return
	}
	// Cards carry the config chips — re-render them reactively (§9).
	s.hub.PublishWorkspace()
	s.renderWsSettingsScreen(w, name, "", nil)
}

// vcsHostRe mirrors configure-workspace.sh validate_vcs_host: a bare
// hostname (workspacesettings parity).
var vcsHostRe = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

// handleWsVCSCredentialsPost serves POST /workspaces/{name}/vcs-credentials
// (§9: {kind, host, token, replace}) — the VCS Tracking row's write. The
// tracker persists as the stored vcs_tracking label; a credential write
// runs configure-workspace.sh vcs-credentials exactly as the TUI invokes
// it (ADR-0007) as one queued op (lines stream into the screen's log
// pane). The replace-credentials confirm gates the overwrite server-side:
// replace=1 with stored credentials writes --replace; with them kept
// (replace unset) the tracker still moves while the credentials stay —
// the TUI's Keep existing, carried 1:1 (§9). github never takes
// --replace (the script refuses it — its file is overwritten).
func (s *Server) handleWsVCSCredentialsPost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ws, err := s.lookupWorkspace(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	root := ws.Path
	key := registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(key) {
		s.renderWsSettingsScreen(w, name, "an operation is queued or running for "+name+" — wait for it to finish", nil)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	kind := r.PostFormValue("kind")
	host := strings.TrimSpace(r.PostFormValue("host"))
	token := strings.TrimSpace(r.PostFormValue("token"))
	replace := r.PostFormValue("replace")
	if kind != "none" && !slices.Contains(catalogs.TrackerKinds, kind) {
		s.renderWsSettingsScreen(w, name, "unknown tracker kind", nil)
		return
	}
	if kind == catalogs.KindGitLabSel || kind == catalogs.KindOther {
		if host == "" {
			s.renderWsSettingsScreen(w, name, "Host required for "+trackerLabel(kind)+".", nil)
			return
		}
		if !vcsHostRe.MatchString(host) {
			s.renderWsSettingsScreen(w, name, "Invalid host — use a bare hostname (e.g. git.example.com).", nil)
			return
		}
	}

	label := trackerLabel(kind)
	hasCreds := kind != "" && kind != "none" &&
		workspacesettings.VCSHostFile(label, root) != "" &&
		fileExists(workspacesettings.VCSHostFile(label, root))

	// Validation: a credential write always needs a typed token (TUI
	// parity — a fresh entry has nothing to keep).
	writeCreds := token != "" && kind != "none" && (!hasCreds || replace == "1")
	if kind != "none" && token == "" && !hasCreds {
		s.renderWsSettingsScreen(w, name, "Token required for "+label+".", nil)
		return
	}
	if kind != "none" && replace == "1" && token == "" {
		s.renderWsSettingsScreen(w, name, "Token required to replace the credentials.", nil)
		return
	}

	// The tracker persists first (the stored config is the truth the
	// Standard row shows back); the credentials ride the queued op.
	stored := ""
	if cfg, err := readSandboxCfg(root); err == nil {
		stored = cfg.VCSTracking
	}
	if label != stored {
		if err := sandboxcfg.PatchString(s.opts.Home, sandboxcfg.Path(root), "vcs_tracking", label); err != nil {
			s.renderWsSettingsScreen(w, name, "vcs_tracking could not be written: "+err.Error(), nil)
			return
		}
	}
	if writeCreds {
		args := vcsCredScriptArgs(kind, token, host, root, replace == "1" && hasCreds)
		s.queue.Launch(webOp{
			Key: key,
			Run: func(emit func(string)) error {
				return runScriptStep(s.runner(), s.deps.ConfigureScript, args, emit)
			},
			Done: func(err error) { s.hub.PublishWorkspace() },
		})
	}
	s.hub.PublishWorkspace()
	s.renderWsSettingsScreen(w, name, "", nil)
}

// vcsCredScriptArgs renders the configure-workspace.sh vcs-credentials
// invocation (workspacesettings.vcsCredArgs parity, catalog-kind driven
// like the create chain's).
func vcsCredScriptArgs(kind, token, host, root string, replace bool) []string {
	var args []string
	switch kind {
	case catalogs.KindGitHub:
		args = []string{"vcs-credentials", "github", root, "--token", token}
	case catalogs.KindGitLab:
		args = []string{"vcs-credentials", "gitlab", root, "--token", token, "--host", "gitlab.com"}
	case catalogs.KindGitLabSel:
		args = []string{"vcs-credentials", "gitlab", root, "--token", token, "--host", host}
	case catalogs.KindOther:
		args = []string{"vcs-credentials", "custom", root, "--token", token, "--host", host}
	default:
		return nil
	}
	// The script refuses --replace for github: its host file is
	// overwritten with a fresh token anyway.
	if replace && kind != catalogs.KindGitHub {
		args = append(args, "--replace")
	}
	return args
}

// handleWsProvidersPost serves POST /workspaces/{name}/providers (§9:
// {add|edit|remove}) — the AI-Provider row's entry management, the TUI
// pane's flows carried 1:1 onto the queue (workspacesettings parity):
// add validates with creation semantics, refuses an empty token without
// an existing credential, and opens the id-parameterized Keep/Replace
// round-trip when a credential exists (§9 replace-credentials confirm);
// edit is token-optional (empty token merges the section only), and a
// rename moves the id — new values first, old section removed after, so
// a failure never leaves a half-renamed config half-eaten; remove is a
// plain delete behind its client popup (the last entry simply leaves
// the workspace with no providers, ADR-0023). Every step runs
// configure-workspace.sh exactly as the TUI invokes it (ADR-0007) as
// one queued op, its lines streaming into the screen's log pane.
func (s *Server) handleWsProvidersPost(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ws, err := s.lookupWorkspace(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	root := ws.Path
	key := registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(key) {
		s.renderWsSettingsScreen(w, name, "an operation is queued or running for "+name+" — wait for it to finish", nil)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	action := r.PostFormValue("action")
	decision := r.PostFormValue("decision")

	pName := r.PostFormValue("name")
	endpoint := strings.TrimSpace(r.PostFormValue("endpoint"))
	token := strings.TrimSpace(r.PostFormValue("token"))

	var steps [][]string
	switch action {
	case "add":
		if msg := workspacesettings.ValidateProviderFields(pName, endpoint, token); msg != "" {
			s.renderWsSettingsScreen(w, name, msg, nil)
			return
		}
		id := providerid.Slug(pName)
		merge := []string{"opencode-config", root, "--id", id, "--name", pName, "--endpoint", endpoint}
		idExists := workspacesettings.HasCredential(root, id)
		switch {
		case token == "":
			if !idExists {
				s.renderWsSettingsScreen(w, name, "Token required — no existing credential entry for '"+id+
					"' was found; type a token to configure this provider.", nil)
				return
			}
			steps = [][]string{merge}
		case idExists && decision == "":
			// The confirm gates the overwrite before any write.
			s.renderWsSettingsScreen(w, name, "", &provConfirmView{
				ID: id, Action: "add", Name: pName, Endpoint: endpoint, Token: token,
			})
			return
		case decision == "replace": // idExists — the confirm's OK
			steps = [][]string{
				{"ai-provider", root, "--name", pName, "--endpoint", endpoint, "--token", token, "--replace"},
				merge,
			}
		default: // fresh credential, or keep existing
			steps = [][]string{merge}
			if !idExists {
				steps = [][]string{
					{"ai-provider", root, "--name", pName, "--endpoint", endpoint, "--token", token},
					merge,
				}
			}
		}
	case "edit":
		orig := r.PostFormValue("orig")
		var found bool
		for _, e := range workspacesettings.ReadProviderEntries(root) {
			if e.ID == orig {
				found = true
				if e.Orphan {
					s.renderWsSettingsScreen(w, name, "stored credential missing — reconfigure via the add form", nil)
					return
				}
			}
		}
		if !found {
			s.renderWsSettingsScreen(w, name, "unknown provider entry '"+orig+"'", nil)
			return
		}
		if msg := workspacesettings.ValidateProviderFields(pName, endpoint, token); msg != "" {
			s.renderWsSettingsScreen(w, name, msg, nil)
			return
		}
		newID := providerid.Slug(pName)
		rename := newID != orig
		merge := []string{"opencode-config", root, "--id", newID, "--name", pName, "--endpoint", endpoint}
		if token == "" && !rename {
			steps = [][]string{merge} // token-optional: the credential stays
			break
		}
		if token == "" && rename {
			// Renaming must move the stored credential; the emptied
			// token is read back from the credential file (#111).
			token = workspacesettings.StoredTokens(root)[orig]
			if token == "" {
				s.renderWsSettingsScreen(w, name, "Token required — renaming moves the stored credential from '"+orig+
					"'; type the token and save again.", nil)
				return
			}
		}
		idExists := workspacesettings.HasCredential(root, newID)
		if idExists && decision == "" {
			s.renderWsSettingsScreen(w, name, "", &provConfirmView{
				ID: newID, Action: "edit", Orig: orig, Name: pName, Endpoint: endpoint, Token: token,
			})
			return
		}
		steps = [][]string{{"ai-provider", root, "--name", pName, "--endpoint", endpoint, "--token", token}}
		if decision == "replace" && idExists {
			steps[0] = append(steps[0], "--replace")
		}
		steps = append(steps, merge)
		if rename {
			steps = append(steps, []string{"ai-provider-remove", root, "--id", orig})
		}
	case "remove":
		orig := r.PostFormValue("orig")
		if orig == "" {
			s.renderWsSettingsScreen(w, name, "unknown provider entry", nil)
			return
		}
		steps = [][]string{{"ai-provider-remove", root, "--id", orig}}
	default:
		s.renderWsSettingsScreen(w, name, "unknown provider action", nil)
		return
	}

	s.queue.Launch(webOp{
		Key: key,
		Run: func(emit func(string)) error {
			for _, args := range steps {
				if err := runScriptStep(s.runner(), s.deps.ConfigureScript, args, emit); err != nil {
					return err
				}
			}
			return nil
		},
		Done: func(err error) { s.hub.PublishWorkspace() },
	})
	s.renderWsSettingsScreen(w, name, "", nil)
}

// handleWsRemoveFromList serves POST /workspaces/{name}/remove-from-list
// (§9) — the red confirm's action behind the popup. The registry entry
// is deleted only: files, configs and the container stay untouched (§7),
// a running container keeps running untracked. A busy queue refuses (§9).
func (s *Server) handleWsRemoveFromList(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	ws, err := s.lookupWorkspace(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	key := registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(key) {
		s.renderWsSettingsScreen(w, name, "an operation is queued or running for "+name+" — wait for it to finish", nil)
		return
	}
	if err := s.regView().RemoveWorkspace(ws.Path); err != nil {
		s.renderWsSettingsScreen(w, name, "Removal failed — the workspace list is unchanged: "+err.Error(), nil)
		return
	}
	s.hub.PublishWorkspace()
	s.renderWsSettingsEmpty(w)
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
	s.renderWsSettingsScreen(w, entries[0].Name, "", nil)
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
func (s *Server) renderWsSettingsScreen(w http.ResponseWriter, name, errMsg string, confirm *provConfirmView) bool {
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
	v.ProvConfirm = confirm
	v.VCSKind = trackerKindForLabel(v.VCSTrackingSt)
	if hostFile := workspacesettings.VCSHostFile(v.VCSTrackingSt, ws.Path); hostFile != "" {
		v.VCSHasCreds = fileExists(hostFile) // the replace confirm gates an overwrite
	}
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

	// Provider entries read from the workspace's stored files (the list's
	// single source of truth, ADR 0025) — orphaned sections render
	// read-only.
	for _, e := range workspacesettings.ReadProviderEntries(ws.Path) {
		v.Providers = append(v.Providers, provEntryView{
			ID: e.ID, Name: e.Name, Endpoint: e.Endpoint, Orphan: e.Orphan,
		})
	}

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

// runScriptStep runs one configure-workspace.sh invocation through run
// and emits its output lines (workspacesettings parity — the shared
// queue-emit shape).
func runScriptStep(run cmdrun.Runner, script string, args []string, emit func(string)) error {
	out, err := run.Output("bash", append([]string{script}, args...)...)
	for _, l := range cmdrun.Lines(out) {
		emit(l)
	}
	return err
}

// fileExists reports whether path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
