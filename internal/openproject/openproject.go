// Package openproject implements the Open Project center view (#77,
// spec #84): a wrapping grid of 30-wide project cards (live ●/○ podman
// state) on the left, a settings pane on the right that doubles as the
// change-settings surface (#63, ADR 0018), the start flow of data-map
// §3 (last_used bump first, live status reconciliation, start.sh arg
// rebuild, status write only on verified success), the Build now / Build
// later / Go back prompt for missing edition images, and console /
// OpenCode access via terminal handoff. Long operations leave the view
// through the single Launcher seam (#82 swaps it); the view never
// spawns goroutines.
package openproject

import (
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// BodyTop is the screen row where center-view bodies start (header,
// separator, sub-menu row above it) — mouse coordinates convert here.
const BodyTop = shell.SubTabRowY + 1

// Fixed card geometry (approved record #72): 30-wide cards with a one
// column gutter; two content rows per card.
const (
	cardInnerW = 26 // content width → card total width 30
	cardTotalW = 30
	cardStride = 31 // card + gutter
	cardRowsN  = 2
	cardH      = 2 + cardRowsN // border + rows
	rowStride  = cardH + 1     // card + gap
)

// Deps injects every external surface: the data layer (home dir),
// command runner (podman / start.sh), script + Dockerfile locations,
// the long-op seam and the terminal-handoff seam.
type Deps struct {
	Home             string
	Runner           cmdrun.Runner
	StartScript      string // dist/scripts/start.sh
	BuildScript      string // dist/scripts/build-container.sh
	Dockerfile       string
	DockerfileCustom string
	Ops              Launcher                                  // default DefaultLauncher
	Exec             func(*exec.Cmd, tea.ExecCallback) tea.Cmd // default tea.ExecProcess
	Now              func() time.Time
}

type viewState int

const (
	stateList   viewState = iota // card grid + settings pane
	stateBuild                   // Build now / Build later / Go back prompt
	stateAccess                  // running-container access prompt
	stateBusy                    // long op runs, embedded log pane
)

type opKind int

const (
	opStart opKind = iota
	opBuild
)

type staged struct {
	loaded      bool
	name        string
	edition     string
	modes       []string
	start       string
	vcs         string
	ai          string
	proxy       bool
	editionGone bool   // stored edition vanished from live discovery
	cfgErr      string // sandbox_config.json unreadable
}

// paneRow is one rendered settings-pane row; section != "" marks a
// clickable/selectable option row, save marks the Save button hit row.
type paneRow struct {
	text    string
	section string
	value   string
	save    bool
}

type cardRect struct {
	rect shell.Rect
	idx  int
}

// Model is the Open Project center view; it implements shell.CenterView
// and is plugged into the shell as the first view (shell.New).
type Model struct {
	deps Deps
	reg  *registry.Registry

	projects   []registry.Project
	selected   int
	gridScroll int
	focus      int // 0 card grid, 1 settings pane

	staged staged
	cursor int // index into the settings pane's navigable rows
	scroll int

	state      viewState
	buildSel   int
	accessSel  int
	accessOpts []string
	opTitle    string

	busyID     int
	busyKind   opKind
	pending    registry.Project
	pendingCfg sandboxcfg.Config

	log   shell.LogPane
	dbl   shell.DblClick
	toast string

	layoutRows []paneRow

	width, height int
	cardRects     []cardRect
	navRows       []int // rows[i] index of navigable row i
	navRects      []shell.Rect
	promptRects   []shell.Rect
}

// New returns the view wired to deps, with the registry loaded.
func New(deps Deps) *Model {
	if deps.Ops == nil {
		deps.Ops = DefaultLauncher{}
	}
	if deps.Exec == nil {
		deps.Exec = tea.ExecProcess
	}
	m := &Model{deps: deps, reg: &registry.Registry{Home: deps.Home, Now: deps.Now}}
	m.refresh()
	return m
}

// SubTabs implements shell.CenterView — the Open Project view has no
// sub-menu tabs of its own.
func (m *Model) SubTabs() ([]string, int) { return nil, 0 }

// TextFocused implements shell.CenterView — no text inputs here.
func (m *Model) TextFocused() bool { return false }

// CardRects returns the current card hit rects (screen coordinates) so
// regression tests can pin them.
func (m *Model) CardRects() []shell.Rect {
	out := make([]shell.Rect, len(m.cardRects))
	for i, r := range m.cardRects {
		out[i] = r.rect
	}
	return out
}

// NavRects returns the settings pane's navigable hit rects (screen
// coordinates).
func (m *Model) NavRects() []shell.Rect { return m.navRects }

func (m *Model) refresh() {
	projs, err := m.reg.AllOrdered()
	if err != nil {
		m.projects = nil
	} else {
		m.projects = projs
	}
	if m.selected >= len(m.projects) {
		m.selected = len(m.projects) - 1
	}
	if m.selected < 0 {
		m.selected = 0
	}
	m.loadStaged()
}

func (m *Model) selectByID(containerID string) {
	for i, p := range m.projects {
		if p.ContainerID == containerID {
			m.selected = i
			break
		}
	}
	m.loadStaged()
}

func (m *Model) loadStaged() {
	m.staged = staged{}
	if m.selected < 0 || m.selected >= len(m.projects) {
		return
	}
	p := m.projects[m.selected]
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(p.Path))
	if err != nil {
		m.staged.cfgErr = err.Error()
		return
	}
	editions, edErr := discovery.Editions(m.deps.Dockerfile, m.deps.DockerfileCustom)
	m.staged = staged{
		loaded:      true,
		name:        p.Name,
		edition:     cfg.ContainerEdition,
		modes:       cfg.ContainerModes,
		start:       cfg.StartOption,
		vcs:         cfg.VCSTracking,
		ai:          cfg.AIProvider,
		proxy:       cfg.UseProxy,
		editionGone: edErr != nil || !hasString(editions, cfg.ContainerEdition),
	}
}

// Update implements shell.CenterView.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.key(msg)
	case tea.MouseMsg:
		return m.mouse(msg)
	case OpDoneMsg:
		return m.opDone(msg)
	case accessDoneMsg:
		m.state = stateList
		m.refresh()
		return nil
	case shell.LineMsg, spinner.TickMsg:
		var cmd tea.Cmd
		m.log, cmd = m.log.Update(msg)
		return cmd
	default:
		return nil
	}
}

type accessDoneMsg struct{}

func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	k := msg.String()
	switch m.state {
	case stateBusy:
		var cmd tea.Cmd
		m.log, cmd = m.log.Update(msg)
		return cmd
	case stateBuild:
		switch k {
		case "up", "k":
			m.buildSel = max(0, m.buildSel-1)
		case "down", "j":
			m.buildSel = min(len(buildPromptOptions)-1, m.buildSel+1)
		case "enter", " ":
			return m.buildPick(m.buildSel)
		}
		return nil
	case stateAccess:
		switch k {
		case "up", "k":
			m.accessSel = max(0, m.accessSel-1)
		case "down", "j":
			m.accessSel = min(len(m.accessOpts)-1, m.accessSel+1)
		case "enter", " ":
			return m.accessPick(m.accessSel)
		}
		return nil
	}
	m.layout() // refresh navigable rows from the last known size
	switch m.focus {
	case 0:
		cpr := m.cardsPerRow()
		switch k {
		case "up", "k":
			m.moveSel(-cpr)
		case "down", "j":
			m.moveSel(cpr)
		case "left", "h":
			m.moveSel(-1)
		case "right", "l":
			m.moveSel(1)
		case "tab", "shift+tab":
			if len(m.navRows) > 0 {
				m.focus = 1
			}
		case "enter":
			return m.beginStart(m.selected)
		}
	default:
		n := len(m.navRows)
		if n == 0 {
			if k == "tab" || k == "shift+tab" {
				m.focus = 0
			}
			return nil
		}
		switch k {
		case "up", "k":
			m.cursor = (m.cursor - 1 + n) % n
		case "down", "j", "tab":
			m.cursor = (m.cursor + 1) % n
		case "shift+tab":
			m.focus = 0
		case "enter", " ":
			return m.activateNav(m.cursor)
		case "left", "right":
			m.cycleOption(k == "right")
		}
	}
	return nil
}

func (m *Model) moveSel(d int) {
	if len(m.projects) == 0 {
		return
	}
	m.selected = clamp(m.selected+d, 0, len(m.projects)-1)
	m.loadStaged()
}

func (m *Model) activateNav(i int) tea.Cmd {
	if i < 0 || i >= len(m.navRows) {
		return nil
	}
	row := m.layoutRows[m.navRows[i]]
	if row.save {
		return m.save()
	}
	if row.section != "" {
		m.applyOption(row.section, row.value)
	}
	return nil
}

// cycleOption steps a single-choice field to the next/previous option in
// its section (keyboard parity for the ○ alternatives).
func (m *Model) cycleOption(forward bool) {
	if m.cursor >= len(m.navRows) {
		return
	}
	row := m.layoutRows[m.navRows[m.cursor]]
	if row.section == "" || row.section == "modes" {
		return
	}
	var opts []string
	switch row.section {
	case "edition":
		editions, err := discovery.Editions(m.deps.Dockerfile, m.deps.DockerfileCustom)
		if err != nil {
			return
		}
		opts = editions
	case "start":
		opts = startOptions
	case "vcs":
		opts = vcsOptions
	case "ai":
		opts = aiOptions
	case "proxy":
		opts = []string{"no", "yes"}
	}
	var cur string
	switch row.section {
	case "edition":
		cur = m.staged.edition
	case "start":
		cur = m.staged.start
	case "vcs":
		cur = m.staged.vcs
	case "ai":
		cur = m.staged.ai
	case "proxy":
		if m.staged.proxy {
			cur = "yes"
		} else {
			cur = "no"
		}
	}
	for i, o := range opts {
		if o == cur {
			step := 1
			if !forward {
				step = -1
			}
			m.applyOption(row.section, opts[(i+step+len(opts))%len(opts)])
			return
		}
	}
}

func (m *Model) applyOption(section, value string) {
	s := &m.staged
	if !s.loaded {
		return
	}
	switch section {
	case "edition":
		s.edition = value
		s.editionGone = false
	case "modes":
		if hasMode(s.modes, value) {
			var out []string
			for _, mo := range s.modes {
				if mo != value {
					out = append(out, mo)
				}
			}
			s.modes = out
		} else {
			s.modes = append(s.modes, value)
		}
	case "start":
		s.start = value
	case "vcs":
		s.vcs = value
	case "ai":
		s.ai = value
	case "proxy":
		s.proxy = value == "yes"
	}
}

func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if m.state == stateBusy {
		var cmd tea.Cmd
		m.log, cmd = m.log.Update(msg)
		return cmd
	}
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	y := msg.Y - BodyTop
	if y < 0 || y >= m.height {
		return nil
	}
	switch m.state {
	case stateBuild:
		for i, r := range m.promptRects {
			if r.Hit(msg.X, msg.Y) {
				return m.buildPick(i)
			}
		}
		return nil
	case stateAccess:
		for i, r := range m.promptRects {
			if r.Hit(msg.X, msg.Y) {
				return m.accessPick(i)
			}
		}
		return nil
	}
	m.layout()
	n := m.dbl.Press(msg)
	for _, cr := range m.cardRects {
		if cr.rect.Hit(msg.X, msg.Y) {
			if n == 2 && cr.idx == m.selected {
				return m.beginStart(cr.idx)
			}
			if cr.idx != m.selected {
				m.selected = cr.idx
				m.focus = 0
				m.loadStaged()
			}
			return nil
		}
	}
	for i, r := range m.navRects {
		if r.Hit(msg.X, msg.Y) {
			m.focus = 1
			m.cursor = i
			return m.activateNav(i)
		}
	}
	return nil
}

// --- start flow (data-map §3) ---

func (m *Model) beginStart(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.projects) {
		return nil
	}
	p := m.projects[idx]
	m.toast = ""
	// 1. Bump last_used FIRST — recency moves even if the start fails.
	if err := m.reg.UpdateLastUsed(p.Path); err != nil {
		m.toast = "Could not update last_used: " + err.Error()
		return nil
	}
	m.refresh()
	m.selectByID(p.ContainerID)
	// 2. Reconcile stored container_status against live podman ps.
	running := discovery.RunningContainers(m.deps.Runner)
	live := hasString(running, registry.ContainerName(p.ContainerID))
	status := "stopped"
	if live {
		status = "running"
	}
	if err := m.reg.UpdateStatus(p.Path, status); err != nil {
		m.toast = "Could not update container status: " + err.Error()
		return nil
	}
	m.refresh()
	m.selectByID(p.ContainerID)
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(p.Path))
	if err != nil {
		m.toast = "Cannot read sandbox_config.json: " + err.Error()
		return nil
	}
	if live {
		m.enterAccess(p, cfg)
		return nil
	}
	// 3. Missing edition image → Build now / Build later / Go back.
	if !discovery.ImageExists(m.deps.Runner, cfg.ContainerEdition) {
		m.state = stateBuild
		m.buildSel = 0
		m.pending = p
		m.pendingCfg = cfg
		return nil
	}
	return m.launchStart(p, cfg)
}

func (m *Model) launchStart(p registry.Project, cfg sandboxcfg.Config) tea.Cmd {
	if !ValidModes(cfg.ContainerModes) {
		m.toast = "offline and cbm_ui are mutually exclusive — fix settings first"
		m.state = stateList
		return nil
	}
	m.state = stateBusy
	m.busyKind = opStart
	m.pending = p
	m.pendingCfg = cfg
	m.opTitle = "Starting container"
	m.log = shell.NewLogPane().Run(true)
	m.busyID++
	args := StartArgs(p.Path, cfg.ContainerEdition, p.ContainerID, cfg.ContainerModes, cfg.StartOption, cfg.UseProxy)
	run, script := m.deps.Runner, m.deps.StartScript
	op := Op{
		ID:    m.busyID,
		Title: m.opTitle,
		Run: func(line func(string)) error {
			out, err := run.Output("bash", append([]string{script}, args...)...)
			for _, l := range cmdrun.Lines(out) {
				line(l)
			}
			return err
		},
	}
	return tea.Batch(m.deps.Ops.Launch(op), m.log.Spin())
}

var buildPromptOptions = []string{"Build now", "Build later", "Go back"}

func (m *Model) buildPick(i int) tea.Cmd {
	switch i {
	case 0: // Build now
		m.state = stateBusy
		m.busyKind = opBuild
		m.opTitle = "Building image " + m.pendingCfg.ContainerEdition
		m.log = shell.NewLogPane().Run(true)
		m.busyID++
		ed, run, script := m.pendingCfg.ContainerEdition, m.deps.Runner, m.deps.BuildScript
		op := Op{
			ID:    m.busyID,
			Title: m.opTitle,
			Run: func(line func(string)) error {
				out, err := run.Output("bash", script, ed)
				for _, l := range cmdrun.Lines(out) {
					line(l)
				}
				return err
			},
		}
		return tea.Batch(m.deps.Ops.Launch(op), m.log.Spin())
	case 1: // Build later — deferred build, stays stopped
		m.toast = "Build later — project stays stopped until the image is built"
		m.state = stateList
		return nil
	default: // Go back
		m.state = stateList
		return nil
	}
}

func (m *Model) opDone(msg OpDoneMsg) tea.Cmd {
	if msg.ID != m.busyID || m.state != stateBusy {
		return nil
	}
	for _, l := range msg.Lines {
		m.log, _ = m.log.Update(shell.LineMsg(l))
	}
	m.log = m.log.Run(false)
	m.refresh()
	switch m.busyKind {
	case opStart:
		p := m.pending
		if msg.Err == nil {
			// container_status = running only on start.sh success.
			if err := m.reg.UpdateStatus(p.Path, "running"); err != nil {
				m.toast = "Could not update container status: " + err.Error()
			} else {
				m.toast = "Container started"
			}
			m.refresh()
			m.selectByID(p.ContainerID)
			m.enterAccess(p, m.pendingCfg)
			return nil
		}
		m.toast = "Failed to start container"
		m.state = stateList
		return nil
	default: // opBuild
		if msg.Err == nil && discovery.ImageExists(m.deps.Runner, m.pendingCfg.ContainerEdition) {
			m.toast = "Image built"
			return m.launchStart(m.pending, m.pendingCfg)
		}
		m.toast = "Build failed — image still unavailable"
		m.state = stateList
		return nil
	}
}

func (m *Model) enterAccess(p registry.Project, cfg sandboxcfg.Config) {
	m.state = stateAccess
	m.accessSel = 0
	m.pending = p
	m.pendingCfg = cfg
	switch cfg.StartOption {
	case "opencode":
		m.accessOpts = []string{"OpenCode (configured)", "Console (bash)"}
	case "web":
		m.accessOpts = []string{"Show Web URL", "Console (bash)"}
	default:
		m.accessOpts = []string{"Console (bash)"}
	}
}

func (m *Model) accessPick(i int) tea.Cmd {
	if i < 0 || i >= len(m.accessOpts) {
		return nil
	}
	label := m.accessOpts[i]
	name := registry.ContainerName(m.pending.ContainerID)
	if label == "Show Web URL" {
		port := discovery.WebPort(m.deps.Runner, name)
		if port != "" {
			m.toast = "OpenCode Web: http://" + port
		} else {
			m.toast = "OpenCode Web is not published for this project"
		}
		m.state = stateList
		return nil
	}
	arg := "bash"
	if label == "OpenCode (configured)" {
		arg = "opencode"
	}
	cmd := exec.Command("podman", "exec", "-it", "--user", "dev", name, arg)
	return m.deps.Exec(cmd, func(error) tea.Msg { return accessDoneMsg{} })
}

// --- change-settings surface (ADR 0018, #63) ---

func (m *Model) save() tea.Cmd {
	s := m.staged
	if !s.loaded {
		return nil
	}
	if s.editionGone {
		m.toast = "Stored edition is no longer available — choose another edition first"
		return nil
	}
	if !ValidModes(s.modes) {
		m.toast = "offline and cbm_ui are mutually exclusive"
		return nil
	}
	p := m.projects[m.selected]
	// ONE whole-config atomic rewrite; applies on next start.
	err := sandboxcfg.Rewrite(m.deps.Home, sandboxcfg.Path(p.Path), sandboxcfg.Settings{
		Edition:     s.edition,
		Modes:       s.modes,
		StartOption: s.start,
		VCS:         s.vcs,
		AIProvider:  s.ai,
		UseProxy:    s.proxy,
	})
	if err != nil {
		m.toast = "Could not save settings: " + err.Error()
		return nil
	}
	m.loadStaged()
	m.toast = "Settings saved — apply on next start"
	return nil
}

// --- layout ---

func (m *Model) cardsPerRow() int {
	return max(1, (leftWidth(m.width)-2)/cardStride)
}

// paneWidth / leftWidth size the two panes: settings pane clamped to
// 36..48 columns, the rest of the width goes to the wrapping card grid.
func paneWidth(width int) int { return clamp(width/2, 36, 48) }
func leftWidth(width int) int { return max(cardTotalW, width-paneWidth(width)-1) }

// layout builds the settings-pane rows and navigable hit rects from the
// last known size, so keyboard activation resolves a cursor to a row
// without a fresh render.
func (m *Model) layout() []paneRow {
	if m.width <= 0 || m.height <= 0 {
		m.navRows, m.navRects = nil, nil
		return nil
	}
	m.navRows, m.navRects, m.promptRects = nil, nil, nil
	if m.state != stateList {
		return nil
	}
	leftW := leftWidth(m.width)
	paneX, paneW := leftW+1, paneWidth(m.width)
	rows := m.settingsRows(paneW)
	m.layoutRows = rows
	for i, r := range rows {
		if r.section != "" || r.save {
			m.navRows = append(m.navRows, i)
		}
	}
	if len(m.navRows) > 0 {
		m.cursor = clamp(m.cursor, 0, len(m.navRows)-1)
		focused := m.navRows[m.cursor]
		if m.scroll > len(rows)-m.height {
			m.scroll = max(0, len(rows)-m.height)
		}
		if m.scroll < 0 {
			m.scroll = 0
		}
		for focused < m.scroll {
			m.scroll--
		}
		for focused > m.scroll+m.height-1 {
			m.scroll++
		}
		m.navRects = make([]shell.Rect, len(m.navRows))
		for i, rowIdx := range m.navRows {
			if rowIdx >= m.scroll && rowIdx < m.scroll+m.height {
				m.navRects[i] = shell.Rect{X: paneX, Y: BodyTop + rowIdx - m.scroll, W: paneW, H: 1}
			}
		}
	}
	return rows
}

// settingsRows builds the settings pane: label above options, divider
// rules, ● current / ○ muted alternatives, Save button last.
func (m *Model) settingsRows(paneW int) []paneRow {
	rows := []paneRow{}
	add := func(text string) { rows = append(rows, paneRow{text: text}) }
	s := m.staged
	if !s.loaded {
		if s.cfgErr != "" {
			add(shell.StyleWarn.Render("Cannot read sandbox_config.json"))
			add(shell.StyleWarn.Render(truncate(s.cfgErr, paneW-2)))
			add(shell.StyleMutedAlt.Render("Use the failure-recovery path to repair it."))
		} else {
			add(shell.StyleMutedAlt.Render("No project selected"))
		}
		return rows
	}
	add(shell.StyleFieldLabel.Render("Project Settings — " + s.name))
	editions, edErr := discovery.Editions(m.deps.Dockerfile, m.deps.DockerfileCustom)
	modes, modeErr := discovery.Modes(m.deps.Runner, m.deps.StartScript)
	if s.editionGone {
		add(shell.StyleWarn.Render("stored edition '" + truncate(s.edition, 24) + "' no longer available — choose another"))
	}
	addSection := func(label, section string, opts []string, err error, cur func(string) bool) {
		rows = append(rows, paneRow{text: shell.StyleFieldLabel.Render(label)})
		rows = append(rows, paneRow{text: shell.StyleMutedAlt.Render(divider(min(paneW-2, 30)))})
		if err != nil {
			rows = append(rows, paneRow{text: shell.StyleMutedAlt.Render("(unavailable: " + truncate(err.Error(), paneW-16) + ")")})
			return
		}
		for _, o := range opts {
			var text string
			if cur(o) {
				text = shell.StyleCurrentVal.Render(shell.SymOn + " " + o)
			} else {
				text = shell.StyleMutedAlt.Render(shell.SymOff + " " + o)
			}
			rows = append(rows, paneRow{text: text, section: section, value: o})
		}
	}
	addSection("Edition", "edition", editions, edErr, func(o string) bool { return s.edition == o })
	addSection("Modes", "modes", modes, modeErr, func(o string) bool { return hasMode(s.modes, o) })
	addSection("Start option", "start", startOptions, nil, func(o string) bool { return s.start == o })
	addSection("VCS tracking", "vcs", vcsOptions, nil, func(o string) bool { return s.vcs == o })
	addSection("AI provider", "ai", aiOptions, nil, func(o string) bool { return s.ai == o })
	addSection("Proxy", "proxy", []string{"no", "yes"}, nil, func(o string) bool { return (o == "yes") == s.proxy })
	add("")
	for _, bl := range shell.ButtonLines("Save Settings", false) {
		rows = append(rows, paneRow{text: bl, save: true})
	}
	return rows
}

// Body implements shell.CenterView.
func (m *Model) Body(width, height int) string {
	m.width, m.height = width, height
	leftW := leftWidth(width)
	paneX, paneW := leftW+1, paneWidth(width)
	var right string
	switch m.state {
	case stateBusy:
		m.log = m.log.SetSize(paneW, height-1)
		indent := paneX - leftW
		right = padLines([]string{shell.StyleFieldLabel.Render(m.opTitle), m.log.View()}, indent, paneW, height)
	case stateBuild:
		indent := paneX - leftW
		right = m.promptPane(indent, paneW, height,
			[]string{
				shell.StyleWarn.Render("Image not found"),
				shell.StyleMutedAlt.Render("opencode-sandbox-" + m.pendingCfg.ContainerEdition + " is not built yet."),
				"",
			},
			buildPromptOptions, m.buildSel)
	case stateAccess:
		head := []string{
			shell.StyleFieldLabel.Render("Container running — " + m.pending.Name),
		}
		if !m.pendingCfg.SetupComplete {
			head = append(head, shell.StyleWarn.Render("First-run setup pending"))
		}
		head = append(head, "")
		indent := paneX - leftW
		right = m.promptPane(indent, paneW, height, head, m.accessOpts, m.accessSel)
	default:
		rows := m.layout()
		indent := paneX - leftW
		right = m.renderSettings(rows, indent, paneW, height)
	}
	left := m.leftPane(leftW, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}

// renderSettings renders the laid-out rows with the scroll window and
// the focused-row highlight, recording hit rects.
func (m *Model) renderSettings(rows []paneRow, indent, paneW, height int) string {
	focused := -1
	if m.focus == 1 && len(m.navRows) > 0 && m.cursor < len(m.navRows) {
		focused = m.navRows[m.cursor]
	}
	var lines []string
	for i, r := range rows {
		if i < m.scroll || i >= m.scroll+height {
			continue
		}
		text := r.text
		if i == focused {
			text = shell.StyleSelectedRow.Render(text)
		}
		lines = append(lines, text)
	}
	return padLines(lines, indent, paneW, height)
}

// promptPane renders a titled vertical button stack (build prompt /
// access prompt), recording hit rects on the button middle lines.
func (m *Model) promptPane(indent, paneW, height int, head []string, opts []string, sel int) string {
	lines := append([]string(nil), head...)
	m.promptRects = nil
	for i, o := range opts {
		for j, bl := range shell.ButtonLines(o, i == sel) {
			if j == 1 {
				m.promptRects = append(m.promptRects, shell.Rect{
					X: paneWidth(m.width) + indent + 1, Y: BodyTop + len(lines), W: min(paneW, shell.Width(bl)), H: 1,
				})
			}
			lines = append(lines, bl)
		}
	}
	return padLines(lines, indent, paneW, height)
}

// leftPane renders the wrapping card grid (or the empty-registry hint)
// with the pinned toast row last; card hit rects are recorded here.
func (m *Model) leftPane(w, h int) string {
	m.cardRects = nil
	var lines []string
	if len(m.projects) == 0 {
		card := shell.Card([]string{
			padTo(cardInnerW, "No projects registered"),
			padTo(cardInnerW, "Create one first:"),
			padTo(cardInnerW, "menu 2 — New Project"),
		})
		lines = strings.Split(card, "\n")
	} else {
		cpr := m.cardsPerRow()
		totalRows := (len(m.projects) + cpr - 1) / cpr
		visRows := max(1, (h-1)/rowStride)
		selRow := m.selected / cpr
		if selRow < m.gridScroll {
			m.gridScroll = selRow
		}
		if selRow >= m.gridScroll+visRows {
			m.gridScroll = selRow - visRows + 1
		}
		running := discovery.RunningContainers(m.deps.Runner)
		var grid []string
		for r := m.gridScroll; r < totalRows && r-m.gridScroll < visRows; r++ {
			for c := 0; c < cpr; c++ {
				idx := r*cpr + c
				if idx >= len(m.projects) {
					break
				}
				card := strings.Split(shell.Card(m.cardRows(m.projects[idx], hasString(running, registry.ContainerName(m.projects[idx].ContainerID)))), "\n")
				baseY := (r - m.gridScroll) * rowStride
				grid = pasteLines(grid, 1+c*cardStride, baseY, card)
				m.cardRects = append(m.cardRects, cardRect{
					rect: shell.Rect{X: 1 + c*cardStride, Y: BodyTop + baseY, W: cardTotalW, H: cardH},
					idx:  idx,
				})
			}
		}
		lines = grid
	}
	for len(lines) < h-1 {
		lines = append(lines, "")
	}
	if len(lines) > h-1 {
		lines = lines[:h-1]
	}
	lines = append(lines, shell.Toast(m.toast))
	return padLines(lines, 0, w, h)
}

func (m *Model) cardRows(p registry.Project, running bool) []string {
	sym, st := shell.SymOff, shell.StyleStatusStopped
	if running {
		sym, st = shell.SymOn, shell.StyleStatusRunning
	}
	rows := []string{
		st.Render(sym) + " " + p.Name,
		truncate(p.Path, cardInnerW),
	}
	if m.selected == indexOfProject(m.projects, p.ContainerID) {
		for i, r := range rows {
			rows[i] = shell.StyleSelectedRow.Render(padTo(cardInnerW, r))
		}
	} else {
		for i, r := range rows {
			rows[i] = padTo(cardInnerW, r)
		}
	}
	return rows
}

func indexOfProject(projects []registry.Project, containerID string) int {
	for i, p := range projects {
		if p.ContainerID == containerID {
			return i
		}
	}
	return -1
}

// --- small helpers ---

func hasString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func padTo(w int, s string) string {
	return lipgloss.NewStyle().Width(w).Render(s)
}

// padLines pads each line to paneW (prefixing paneX columns), pads the
// block to exactly h lines, and trims overflow.
func padLines(lines []string, paneX, paneW, h int) string {
	if len(lines) > h {
		lines = lines[:h]
	}
	prefix := strings.Repeat(" ", paneX)
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = prefix + lipgloss.NewStyle().Width(paneW).MaxWidth(paneW).Render(lines[i])
		} else {
			out[i] = prefix + strings.Repeat(" ", paneW)
		}
	}
	return strings.Join(out, "\n")
}

// pasteLines pastes a multi-line block into a growing line buffer at
// (col, row), padding earlier lines as needed.
func pasteLines(dst []string, col, row int, block []string) []string {
	for i, bl := range block {
		y := row + i
		for len(dst) <= y {
			dst = append(dst, "")
		}
		pad := col - shell.Width(dst[y])
		if pad > 0 {
			dst[y] += strings.Repeat(" ", pad)
		}
		dst[y] += bl
	}
	return dst
}
