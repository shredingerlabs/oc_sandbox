// Workspaces screen (#153, spec §7 Workspace → Open): the card grid of
// registered workspaces — edition icon tile (left, tinted via the
// edition hue through CSS --ed), name, settings row with icons, the
// relative last-used date from the registry, and the action rows.
// Open honors the §6 matrix: Standard always leads into web (no
// terminal buttons); Advanced keeps the single Open button honoring the
// configured start option plus the console/OpenCode/Web access rows.
// GET /workspaces (+?q=, server-side name filtering) returns the HTML
// fragment for the HTMX screen swap; cards re-render reactively from
// the SSE workspace events — no polling.
package webserver

import (
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// editionMeta is the per-edition card metadata: Lucide glyph file under
// img/editions/ and the Signal hue (prototype-webui-design.html, §5).
type editionMeta struct {
	Hue   string
	Label string
}

// editions is the hue map of the signed-off Signal mockup; unknown
// editions fall back to the base glyph and accent hue.
var editions = map[string]editionMeta{
	"swdev":    {Hue: "#2f6fed", Label: "Software Dev"},
	"web":      {Hue: "#8b5cf6", Label: "Web"},
	"embedded": {Hue: "#0ea5a4", Label: "Embedded"},
	"matlab":   {Hue: "#e8721c", Label: "MATLAB"},
	"ros2":     {Hue: "#16a34a", Label: "ROS 2"},
	"writing":  {Hue: "#d97757", Label: "Writing"},
	"latex":    {Hue: "#4f46e5", Label: "LaTeX"},
	"pkm":      {Hue: "#db2777", Label: "PKM"},
	"base":     {Hue: "#94a3b8", Label: "Base"},
}

func editionFor(name string) editionMeta {
	if m, ok := editions[name]; ok {
		return m
	}
	return editionMeta{Hue: "", Label: name}
}

// cardView is the template data of one workspace card.
type cardView struct {
	Name        string
	Icon        string // edition glyph file (base.svg fallback)
	Hue         string // CSS --ed value, "" → accent default
	Edition     string // edition label chip
	Hil         bool   // HIL chip: hil_mode recorded in the modes (any edition, §6)
	Proxy       bool   // advanced-only chips: stored truth straight from sandbox_config
	Offline     bool
	CBMUI       bool
	SearchMode  string // shown in Advanced when not "none"
	Running     bool   // registry container_status "running"
	LastUsed    string // relative date ("2h ago"), "never used" when unset
	OpenVariant string // web (Standard, always) or the configured start option (Advanced)
	Advanced    bool   // display mode: reveals the terminal access rows
	Busy        bool   // an op is queued/running for this workspace
}

// wsScreenView is the template data of the whole screen fragment.
type wsScreenView struct {
	P     string // tokenized path prefix
	Query string // the ?q= value echoed back into the search input
	Count int    // rendered card count (sidebar count pill)
	Cards []cardView
	Empty bool
	Error string // registry read failure shown instead of a dead end
}

const wsEmptyText = "No workspaces registered"

var wsTemplate = template.Must(template.New("ws").Parse(`
{{define "card"}}
<article class="card" data-ws="{{.Name}}"{{if .Hue}} style="--ed:{{.Hue}}"{{end}}>
  <div class="toprow">
    <div class="tile"><img src="img/editions/{{.Icon}}.svg" alt="" draggable="false"></div>
    <div class="body">
      <div class="name">{{.Name}}{{if .Running}}<span class="dot" data-running title="running"></span>{{end}}</div>
      <div class="sub">last used {{.LastUsed}}</div>
    </div>
    <button class="btn icon" title="workspace settings" data-act="settings">
      <svg class="ic" viewBox="0 0 24 24" aria-hidden="true"><path d="M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6Z"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1Z"/></svg>
    </button>
  </div>
  <div class="meta">
    <span class="chip" data-chip="edition">{{.Edition}}</span>
    {{if .Hil}}<span class="chip" data-chip="hil">HIL</span>{{end}}
    {{if .Advanced}}
      {{if .Proxy}}<span class="chip" data-chip="proxy">proxy</span>{{end}}
      {{if .Offline}}<span class="chip" data-chip="offline">offline</span>{{end}}
      {{if .CBMUI}}<span class="chip" data-chip="cbm">CBM</span>{{end}}
      {{if and .SearchMode (ne .SearchMode "none")}}<span class="chip" data-chip="search">{{.SearchMode}}</span>{{end}}
    {{end}}
  </div>
  {{if .Busy}}<div class="busy" data-busy>working — queued behind the running op</div>{{end}}
  <div class="acts">
    <button class="btn open" data-act="open" data-variant="{{.OpenVariant}}">Open</button>
    <button class="btn" data-act="web" title="OpenCode web — fresh pair link in a new tab">Web</button>
    {{if .Advanced}}
    <button class="btn" data-act="console" data-advanced-only title="console session in a host terminal window">Console</button>
    <button class="btn" data-act="opencode" data-advanced-only title="OpenCode TUI in a host terminal window">OpenCode</button>
    {{end}}
  </div>
  <div class="confirms">
    <button class="btn danger" data-confirm="stop">Stop…</button>
    <button class="btn danger" data-confirm="delete">Delete…</button>
  </div>
  <div class="oplog" data-oplog hidden></div>
</article>
{{end}}

{{define "ws-screen"}}
<div class="ws-screen" id="ws-screen" data-count="{{.Count}}">
  <div class="pagehead">
    <h2>Registered workspaces</h2>
    <input class="ws-search" data-role="ws-search" type="search" placeholder="Search workspaces"
           value="{{.Query}}" autocomplete="off">
  </div>
  {{if .Error}}<p class="form-error" role="alert">{{.Error}}</p>{{end}}
  {{if .Empty}}
  <p class="empty" data-empty="workspaces">No workspaces registered</p>
  {{else}}
  <div class="grid">
    {{range .Cards}}{{template "card" .}}{{end}}
  </div>
  {{end}}
</div>
{{end}}
`))

// handleWorkspacesGET serves GET /workspaces (+?q=, server-side name
// filtering, §9): the whole screen fragment — search head plus grid —
// for the HTMX screen swap.
func (s *Server) handleWorkspacesGET(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	mode := s.displayMode()

	reg := &registry.Registry{Home: s.opts.Home}
	entries, err := reg.AllOrdered()
	var cards []cardView
	errMsg := ""
	if err != nil && err != registry.ErrNotFound {
		errMsg = "registry unreadable — fix ~/.config/oc-sandbox/workspaces.json"
	}
	// MRU order comes straight from the registry (bash sort_by|reverse
	// parity); the ?q= filter keeps it.
	filtered := make([]registry.Workspace, 0, len(entries))
	for _, e := range entries {
		if q == "" || strings.Contains(strings.ToLower(e.Name), strings.ToLower(q)) {
			filtered = append(filtered, e)
		}
	}

	for _, e := range filtered {
		cards = append(cards, s.cardFor(e, mode))
	}
	v := wsScreenView{
		P:     s.pathPrefix(),
		Query: q,
		Count: len(cards),
		Cards: cards,
		Empty: errMsg == "" && len(cards) == 0, // missing registry reads as empty (first-run)
		Error: errMsg,
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := wsTemplate.ExecuteTemplate(w, "ws-screen", v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// cardFor builds one card's view: stored truth straight from the
// registry and sandbox_config.json — Standard hides terminal concepts
// (presets, not pinning); Advanced reveals the stored values.
func (s *Server) cardFor(e registry.Workspace, mode string) cardView {
	c := cardView{
		Name:        e.Name,
		Icon:        "base",
		Running:     e.ContainerStatus == "running",
		LastUsed:    relativeLastUsed(e.LastUsed, s.now()),
		OpenVariant: "web", // Standard: always web, never a terminal concept (§6)
		Advanced:    mode == config.DisplayAdvanced,
		Busy:        s.queue.Busy(registry.ContainerID(e.Path)),
	}
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(e.Path))
	if err != nil {
		// Unreadable sandbox_config must not dead-end the card: base
		// tile, no chips, web Open.
		return c
	}
	meta := editionFor(cfg.ContainerEdition)
	c.Icon = cfg.ContainerEdition
	if _, ok := editions[cfg.ContainerEdition]; !ok {
		c.Icon = "base"
	}
	c.Hue = meta.Hue
	c.Edition = meta.Label
	for _, m := range cfg.ContainerModes {
		switch m {
		case "hil_mode":
			c.Hil = true
		case "offline":
			c.Offline = true
		case "cbm_ui":
			c.CBMUI = true
		}
	}
	c.Proxy = cfg.UseProxy
	c.SearchMode = cfg.SearchMode
	if c.Advanced {
		if cfg.StartOption == "console" || cfg.StartOption == "opencode" || cfg.StartOption == "web" {
			c.OpenVariant = cfg.StartOption
		} else {
			c.OpenVariant = "web"
		}
	}
	return c
}

// displayMode reads the stored display mode per request — a mode switch
// re-renders reactively with no per-workspace state (§6).
func (s *Server) displayMode() string {
	if s.opts.Home == "" {
		return config.DisplayStandard
	}
	gc, err := config.ReadGlobalConfig(s.opts.Home)
	if err != nil {
		return config.DisplayStandard
	}
	return gc.DisplayMode
}

func (s *Server) now() time.Time {
	if s.opts.Now != nil {
		return s.opts.Now()
	}
	return time.Now()
}

// relativeLastUsed renders the registry last_used relative to now:
// minutes/hours/days ago, older dates as the plain date, unset stamps
// as "never used" — never a raw timestamp.
func relativeLastUsed(stamp string, now time.Time) string {
	if strings.TrimSpace(stamp) == "" {
		return "never used"
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return "a while ago"
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmtMinutes(int(d.Minutes()))
	case d < 24*time.Hour:
		return fmtHours(int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmtDays(int(d.Hours() / 24))
	default:
		return "on " + t.Format("2006-01-02")
	}
}

func fmtMinutes(m int) string {
	if m == 1 {
		return "1 min ago"
	}
	return strconv.Itoa(m) + " min ago"
}

func fmtHours(h int) string {
	if h == 1 {
		return "1h ago"
	}
	return strconv.Itoa(h) + "h ago"
}

func fmtDays(d int) string {
	if d == 1 {
		return "1d ago"
	}
	return strconv.Itoa(d) + "d ago"
}
