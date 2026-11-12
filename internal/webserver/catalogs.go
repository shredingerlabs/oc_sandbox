// Catalogs screens (spec §8/#155): Settings → VCS Tracker and
// AI-Provider manage the global token-free catalogs in catalogs.json —
// the third general config file (atomic writes with backup rotation
// ride config.AtomicWrite via catalogs.WriteFile). Endpoints return
// HTML fragments for HTMX swap (§9); the action verbs are
// POST /catalogs/{trackers,providers} with action=add|edit|remove.
// Entries are unique by slugified display name, kept in insertion
// order; "none" stays the empty default and is never a catalog entry;
// an edit or remove never cascades into workspaces created from the
// entry (template model, ADR-0031).
package webserver

import (
	"html/template"
	"net/http"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
)

// catalogsTemplate renders the catalogs fragments. P carries the
// tokenized path prefix so hx-post targets stay absolute and correct
// wherever the user is in the shell.
var catalogsTemplate = template.Must(template.New("catalogs").
	Funcs(iconFuncs).
	Parse(`
{{define "trackers"}}
{{template "trackers-body" .}}
{{end}}
{{define "providers"}}
{{template "providers-page-body" .}}
{{end}}

{{define "trackers-page"}}
<div class="pagehead"><div><h2>VCS Tracker</h2>
<p class="desc">Reusable tracker entries — starting points picked at workspace creation. Entries hold no tokens and never cascade into existing workspaces.</p></div></div>
{{template "trackers-body" .}}
{{end}}

{{define "providers-page"}}
<div class="pagehead"><div><h2>AI-Provider</h2>
<p class="desc">Reusable provider entries — OpenAI-compatible endpoints picked at workspace creation. Entries hold no tokens and never cascade into existing workspaces.</p></div></div>
{{template "providers-page-body" .}}
{{end}}

{{define "trackers-body"}}
<section class="catalog" id="catalogs-trackers" data-catalog="trackers">
  <h2>VCS Tracker</h2>
  <p class="catalog-hint">Reusable starting points for New Workspace — entries hold no tokens and never cascade into existing workspaces.</p>
  {{if .Error}}<p class="form-error" role="alert">{{.Error}}</p>{{end}}
  {{if .Entries}}
  <div class="plist">
    {{range .Entries}}
    <div class="entry" data-name="{{.Name}}">
      <div class="row">
        <div class="ic">{{icon "git-branch"}}</div>
        <div class="rl">
          <div class="t"><strong class="entry-name">{{.Name}}</strong> <span class="badge">{{.Kind}}</span>{{if .Host}} <span class="entry-host">{{.Host}}</span>{{end}}</div>
          {{if or .GitName .GitEmail}}<div class="s">git: {{.GitName}} &lt;{{.GitEmail}}&gt;</div>{{end}}
        </div>
        <div class="rc">
          <form class="entry-remove" hx-post="{{$.P}}catalogs/trackers" hx-target="#catalogs-trackers" hx-swap="outerHTML"
                data-confirm-popup data-confirm-title="Remove tracker {{.Name}}?"
                data-confirm-question="Removes the catalog entry only — existing workspaces keep their settings."
                data-confirm-label="Remove">
            <input type="hidden" name="action" value="remove">
            <input type="hidden" name="orig" value="{{.Name}}">
            <button type="submit" class="btn danger">Remove</button>
          </form>
        </div>
      </div>
      <details class="entry-edit">
        <summary>Edit</summary>
        <form hx-post="{{$.P}}catalogs/trackers" hx-target="#catalogs-trackers" hx-swap="outerHTML">
          <input type="hidden" name="action" value="edit">
          <input type="hidden" name="orig" value="{{.Name}}">
          <label>Display name <input name="name" value="{{.Name}}"></label>
          <label>Git name <input name="git_name" value="{{.GitName}}"></label>
          <label>Git e-mail <input name="git_email" value="{{.GitEmail}}"></label>
          <label>Tracker kind
            <select name="kind">
              {{range $.Kinds}}<option value="{{.}}" {{if eq . $.Sel.Kind}}selected{{end}}>{{.}}</option>{{end}}
            </select>
          </label>
          <label data-field="host"{{if or (eq .Kind "github") (eq .Kind "gitlab")}} hidden{{end}}>Host <input name="host" value="{{.Host}}" placeholder="gitlab.example.com"></label>
          <button type="submit" class="btn">Save</button>
        </form>
      </details>
    </div>
    {{end}}
  </div>
  {{else}}
  <p class="empty" data-empty="trackers">No tracker entries yet — “None” is the default.</p>
  {{end}}
  <form class="add-form" hx-post="{{$.P}}catalogs/trackers" hx-target="#catalogs-trackers" hx-swap="outerHTML">
    <input type="hidden" name="action" value="add">
    <h3>Add tracker</h3>
    <label>Display name <input name="name"></label>
    <label>Git name <input name="git_name"></label>
    <label>Git e-mail <input name="git_email"></label>
    <label>Tracker kind
      <select name="kind">
        {{range $.Kinds}}<option value="{{.}}">{{.}}</option>{{end}}
      </select>
    </label>
    <label data-field="host" hidden>Host <input name="host" placeholder="gitlab.example.com"> <span class="field-hint">required for own GitLab / others</span></label>
    <button type="submit" class="btn">Add tracker</button>
  </form>
</section>
{{end}}

{{define "providers-page-body"}}
<section class="catalog" id="catalogs-providers" data-catalog="providers">
  <h2>AI-Provider</h2>
  <p class="catalog-hint">Reusable starting points for New Workspace — entries hold no tokens and never cascade into existing workspaces.</p>
  {{if .Error}}<p class="form-error" role="alert">{{.Error}}</p>{{end}}
  {{if .Entries}}
  <div class="plist">
    {{range .Entries}}
    <div class="entry" data-name="{{.Name}}">
      <div class="row">
        <div class="ic">{{icon "bot"}}</div>
        <div class="rl">
          <div class="t"><strong class="entry-name">{{.Name}}</strong></div>
          <div class="s">{{.Endpoint}}</div>
        </div>
        <div class="rc">
          <form class="entry-remove" hx-post="{{$.P}}catalogs/providers" hx-target="#catalogs-providers" hx-swap="outerHTML"
                data-confirm-popup data-confirm-title="Remove provider {{.Name}}?"
                data-confirm-question="Removes the catalog entry only — existing workspaces keep their settings."
                data-confirm-label="Remove">
            <input type="hidden" name="action" value="remove">
            <input type="hidden" name="orig" value="{{.Name}}">
            <button type="submit" class="btn danger">Remove</button>
          </form>
        </div>
      </div>
      <details class="entry-edit">
        <summary>Edit</summary>
        <form hx-post="{{$.P}}catalogs/providers" hx-target="#catalogs-providers" hx-swap="outerHTML">
          <input type="hidden" name="action" value="edit">
          <input type="hidden" name="orig" value="{{.Name}}">
          <div class="field-row">
            <label>Display name <input name="name" value="{{.Name}}"></label>
            <label>Endpoint <input name="endpoint" value="{{.Endpoint}}" placeholder="https://api.example.com/v1"></label>
          </div>
          <button type="submit" class="btn">Save</button>
        </form>
      </details>
    </div>
    {{end}}
  </div>
  {{else}}
  <p class="empty" data-empty="providers">No provider entries yet — “None” is the default.</p>
  {{end}}
  <form class="add-form" hx-post="{{$.P}}catalogs/providers" hx-target="#catalogs-providers" hx-swap="outerHTML">
    <input type="hidden" name="action" value="add">
    <h3>Add provider</h3>
    <div class="field-row">
      <label>Display name <input name="name"></label>
      <label>Endpoint <input name="endpoint" placeholder="https://api.example.com/v1"> <span class="field-hint">OpenAI-compatible endpoint</span></label>
    </div>
    <button type="submit" class="btn">Add provider</button>
  </form>
</section>
{{end}}
`))

// trackersView is the template data for the trackers section.
type trackersView struct {
	P       string // tokenized path prefix ("/t/<token>/")
	Error   string // validation/write error shown above the list
	Entries []catalogs.Tracker
	Kinds   []string // selectable tracker kinds (script-level slugs)
	Sel     catalogs.Tracker
}

// providersView is the template data for the providers section.
type providersView struct {
	P       string
	Error   string
	Entries []catalogs.Provider
}

// handleTrackersPage serves GET /catalogs/trackers — the split
// Settings page the sidebar menu shows: the trackers section alone,
// wrapped in a pagehead. The provider page lives at /catalogs/providers
// (the former combined screen's routes and rendering split the same
// way; nothing else moved).
func (s *Server) handleTrackersPage(w http.ResponseWriter, r *http.Request) {
	d := catalogs.Read(s.opts.Home)
	tv := trackersView{P: s.pathPrefix(), Entries: d.Trackers, Kinds: catalogs.TrackerKinds, Sel: catalogs.Tracker{Kind: catalogs.KindGitHub}}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := catalogsTemplate.ExecuteTemplate(w, "trackers-page", tv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleProvidersPage serves GET /catalogs/providers — the Settings →
// AI-Provider page the sidebar menu shows.
func (s *Server) handleProvidersPage(w http.ResponseWriter, r *http.Request) {
	d := catalogs.Read(s.opts.Home)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := catalogsTemplate.ExecuteTemplate(w, "providers-page", providersView{P: s.pathPrefix(), Entries: d.Providers}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// pathPrefix is the tokenized absolute prefix for fragment URLs.
func (s *Server) pathPrefix() string { return "/t/" + s.token + "/" }

// renderTrackers writes the trackers section fragment (the swap target
// of every tracker action).
func (s *Server) renderTrackers(w http.ResponseWriter, d catalogs.Data, errMsg string) {
	tv := trackersView{P: s.pathPrefix(), Error: errMsg, Entries: d.Trackers, Kinds: catalogs.TrackerKinds, Sel: catalogs.Tracker{Kind: catalogs.KindGitHub}}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := catalogsTemplate.ExecuteTemplate(w, "trackers", tv); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// renderProviders writes the providers section fragment.
func (s *Server) renderProviders(w http.ResponseWriter, d catalogs.Data, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := catalogsTemplate.ExecuteTemplate(w, "providers", providersView{P: s.pathPrefix(), Error: errMsg, Entries: d.Providers}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// handleTrackersPOST serves POST /catalogs/trackers with
// action=add|edit|remove (form fields per §8: display name, git
// name/mail, tracker kind, bare host).
func (s *Server) handleTrackersPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	orig := r.PostFormValue("orig")
	t := catalogs.Tracker{
		Name:     r.PostFormValue("name"),
		GitName:  r.PostFormValue("git_name"),
		GitEmail: r.PostFormValue("git_email"),
		Kind:     r.PostFormValue("kind"),
		Host:     r.PostFormValue("host"),
	}
	d := catalogs.Read(s.opts.Home)
	switch r.PostFormValue("action") {
	case "add":
		if err := catalogs.ValidateTracker(t); err != nil {
			s.renderTrackers(w, d, err.Error())
			return
		}
		if err := d.AddTracker(t); err != nil {
			s.renderTrackers(w, d, err.Error())
			return
		}
	case "edit":
		if err := d.EditTracker(orig, t); err != nil {
			s.renderTrackers(w, d, err.Error())
			return
		}
	case "remove":
		if err := d.RemoveTracker(orig); err != nil {
			s.renderTrackers(w, d, err.Error())
			return
		}
	default:
		s.renderTrackers(w, d, "unknown action")
		return
	}
	if err := catalogs.WriteFile(s.opts.Home, d); err != nil {
		s.renderTrackers(w, d, err.Error())
		return
	}
	s.renderTrackers(w, d, "")
}

// handleProvidersPOST serves POST /catalogs/providers with
// action=add|edit|remove (display name + OpenAI-compatible endpoint).
func (s *Server) handleProvidersPOST(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	orig := r.PostFormValue("orig")
	p := catalogs.Provider{
		Name:     r.PostFormValue("name"),
		Endpoint: r.PostFormValue("endpoint"),
	}
	d := catalogs.Read(s.opts.Home)
	switch r.PostFormValue("action") {
	case "add":
		if err := catalogs.ValidateProvider(p); err != nil {
			s.renderProviders(w, d, err.Error())
			return
		}
		if err := d.AddProvider(p); err != nil {
			s.renderProviders(w, d, err.Error())
			return
		}
	case "edit":
		if err := d.EditProvider(orig, p); err != nil {
			s.renderProviders(w, d, err.Error())
			return
		}
	case "remove":
		if err := d.RemoveProvider(orig); err != nil {
			s.renderProviders(w, d, err.Error())
			return
		}
	default:
		s.renderProviders(w, d, "unknown action")
		return
	}
	if err := catalogs.WriteFile(s.opts.Home, d); err != nil {
		s.renderProviders(w, d, err.Error())
		return
	}
	s.renderProviders(w, d, "")
}
