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
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// BodyTop is the screen row where center-view bodies start (header,
// separator, sub-menu row above it) — mouse coordinates convert here.
const BodyTop = shell.SubTabRowY + 1

// Fixed card geometry (approved record #72): 30-wide cards with a one
// column gutter; three content rows per card.
const (
	cardInnerW = 26 // content width → shell.CardWidth 30 (border + 26)
	cardRowsN  = 3
	rowStride  = shell.CardHeight + 1 // card + gap
)

// Deps injects every external surface: the data layer (home dir),
// command runner (podman / start.sh), script + Dockerfile locations,
// the long-op seam and the terminal-handoff seam.
type Deps struct {
	Home             string
	Runner           cmdrun.Runner
	StartScript      string // dist/scripts/start.sh
	BuildScript      string // dist/scripts/build-container.sh
	ConfigureScript  string // dist/scripts/configure-project.sh (#103: provider steps)
	Dockerfile       string
	DockerfileCustom string
	Ops              Launcher                                  // default DefaultLauncher
	Tasks            *taskmgr.Manager                          // shared background-task manager (#82): handoff guard + still-running indicators
	Exec             func(*exec.Cmd, tea.ExecCallback) tea.Cmd // default tea.ExecProcess
	Now              func() time.Time
}

type viewState int

const (
	stateList  viewState = iota // card grid + settings pane
	stateBuild                  // Build now / Build later / Go back prompt
	stateBusy                   // long op runs, embedded log pane
)

type opKind int

const (
	opStart opKind = iota
	opBuild
	opAI // settings-launched provider steps (#103)
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
	tf      string // settings-pane text field id ("name"/"endpoint"/"token")
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

	projects      []registry.Project
	selected      int
	gridScroll    int          // card-grid scroll (hidden card rows)
	gridFollow    shell.Follow // the selected card pulls the grid on change
	paneScrollMax int          // settings-pane scroll clamped to this (computed in layout)
	focus         int          // 0 card grid, 1 settings pane

	startSel      string       // preselected start variant of the selected card (console/opencode/web)
	startBtnRects []shell.Rect // start-button hit rects on the selected visible card

	// Custom AI provider capture (#103): selection state lives on the
	// Model (staged is rebuilt by every refresh); the token is empty on
	// load and only ever typed.
	aiTouched   bool   // the user moved the AI row off the stored value
	aiSel       bool   // Custom capture active
	aiName      string // provider display name (id source)
	aiEndpoint  string
	aiToken     string
	aiConfirm   *aiConfirmState      // Keep/Replace confirm pending
	aiSteps     [][]string           // queued configure-project.sh argument vectors
	aiSaveCfg   *sandboxcfg.Settings // whole-settings rewrite deferred to step success
	aiRunningOp bool

	staged  staged
	cursor  int          // index into the settings pane's navigable rows
	pscroll int          // settings pane scroll offset
	follow  shell.Follow // the cursor pulls the pane; wheel scrolling pauses it

	state    viewState
	buildSel int
	opTitle  string

	busyID     int
	busyKey    string // queue key of the in-flight op (id-disambiguation)
	busyKind   opKind
	pending    registry.Project
	pendingCfg sandboxcfg.Config
	variant    string // start variant of the start in flight (console/opencode/web)
	// resumeRecover queues the first-run recovery shell hook for after
	// the access handoff returns: tea.Batch does not sequence, so the
	// setup machine must not race a suspended TUI.
	resumeRecover string

	log   shell.LogPane
	dbl   shell.DblClick
	toast string

	// running is the cached live podman-ps list for the card dots. It is
	// refreshed through a background cmd (runningMsg) — never queried on
	// the event loop: a stalled podman (rootless lock contention, bridge
	// bringup) would freeze the whole TUI when View ran it per paint.
	running []string
	// reconciling guards against double click triggering the start
	// reconcile twice before the first one returns.
	reconciling bool

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
	m := &Model{deps: deps, reg: &registry.Registry{Home: deps.Home, Now: deps.Now}, follow: shell.NewFollow(true, false), gridFollow: shell.NewFollow(false, false)}
	m.follow.Arm()
	m.gridFollow.Arm()
	m.refresh()
	return m
}

// Busy implements shell.BusyView: a long operation runs (guarded exit).
func (m *Model) Busy() bool { return m.state == stateBusy }

// Activate implements shell.ActivableView: re-read the project registry
// when the view becomes active so projects created in other views (New
// Project wizard, setup flow) show up without a restart.
func (m *Model) Activate() tea.Cmd {
	m.refresh()
	return m.poll()
}

// SubTabs implements shell.CenterView — the Open Project view has no
// sub-menu tabs of its own.
func (m *Model) SubTabs() ([]string, int) { return nil, 0 }

// TextFocused implements shell.CenterView: a Custom-provider text row
// holds focus in the settings pane (#103).
func (m *Model) TextFocused() bool {
	f := m.focusedNavRow()
	return f != nil && f.tf != ""
}

// focusedNavRow returns the pane row under the settings cursor, or nil
// when the pane is unfocused / empty.
func (m *Model) focusedNavRow() *paneRow {
	if m.focus != 1 || m.cursor < 0 || m.cursor >= len(m.navRows) {
		return nil
	}
	return &m.layoutRows[m.navRows[m.cursor]]
}

// resetAICapture clears the Custom provider capture (project switch /
// reload); in-flight step state survives so a running op completion
// still lands.
func (m *Model) resetAICapture() {
	m.aiTouched, m.aiSel = false, false
	m.aiName, m.aiEndpoint, m.aiToken = "", "", ""
	m.aiConfirm = nil
}

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
	m.resetAICapture()
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
	// the configured start option is the card's preselected start variant
	if hasString(startOptions, cfg.StartOption) {
		m.startSel = cfg.StartOption
	}
}

// Update implements shell.CenterView.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return tea.Batch(m.key(msg), m.poll())
	case tea.MouseMsg:
		return tea.Batch(m.mouse(msg), m.poll())
	case tea.WindowSizeMsg:
		return m.pollRunning() // fresh card dots on every (re)render pass
	case OpDoneMsg:
		return m.opDone(msg)
	case reconcileMsg:
		return m.applyReconcile(msg)
	case runningMsg:
		m.running = msg
		return nil
	case taskmgr.LineEvent:
		if msg.ID == m.busyID && (msg.Key == "" || msg.Key == m.busyKey) { // our op only (Key disambiguates reused numeric ids)
			var cmd tea.Cmd
			m.log, cmd = m.log.Update(shell.LineMsg(msg.Text))
			return cmd
		}
		return nil
	case accessDoneMsg:
		return m.accessDone(msg)
	case shell.LineMsg, spinner.TickMsg:
		var cmd tea.Cmd
		m.log, cmd = m.log.Update(msg)
		return cmd
	default:
		return nil
	}
}

// accessDone finishes the terminal handoff: back to the list; an error
// (most likely the container vanished between states) surfaces with a
// recovery affordance (#82) — the refreshed card grid shows the stopped
// state and the card remains startable.
func (m *Model) accessDone(msg accessDoneMsg) tea.Cmd {
	m.state = stateList
	m.refresh()
	// bubbletea's RestoreTerminal (fired by the ExecProcess handoff) does
	// not re-enable mouse mode, so the terminal comes back mouse-dead:
	// `tea.EnableMouseCellMotion` re-sends the enable sequences now that
	// the terminal is ours.
	cmds := []tea.Cmd{tea.EnableMouseCellMotion, m.poll()}
	if msg.Err != nil {
		m.toast = "Container session ended: " + msg.Err.Error() +
			" — is the container gone? Start it again from its card."
	}
	if m.resumeRecover != "" { // first-run setup resumes after the session returns
		path := m.resumeRecover
		m.resumeRecover = ""
		cmds = append(cmds, func() tea.Msg { return shell.RecoverMsg{Path: path} })
	}
	return tea.Batch(cmds...) // the session continued outside; re-check live state
}

type accessDoneMsg struct{ Err error }

// runningMsg is the fresh podman ps result from a background poll.
type runningMsg []string

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
	}
	m.layout()     // refresh navigable rows from the last known size
	m.follow.Arm() // keys re-engage the pull that wheel scrolling paused
	switch m.focus {
	case 0:
		cpr := m.cardsPerRow()
		switch k {
		case "up", "k":
			m.moveSel(-cpr)
		case "down", "j":
			m.moveSel(cpr)
		case "left", "h":
			m.cyclePreselect(false)
		case "right", "l":
			m.cyclePreselect(true)
		case "tab", "shift+tab":
			if len(m.navRows) > 0 {
				m.focus = 1
			}
		case "enter":
			return m.beginStart(m.selected, m.startSel)
		}
	default:
		n := len(m.navRows)
		if n == 0 {
			if k == "tab" || k == "shift+tab" {
				m.focus = 0
			}
			return nil
		}
		row := m.focusedNavRow()
		isText := row != nil && row.tf != ""
		if isText { // movement keys still navigate; the rest types
			switch k {
			case "up", "k", "down", "j", "tab", "shift+tab", "enter",
				"left", "right":
			case "backspace":
				if v := m.aiTextVal(row.tf); v != "" {
					rs := []rune(v)
					m.setAIText(row.tf, string(rs[:len(rs)-1]))
				}
				return nil
			case " ":
				m.setAIText(row.tf, m.aiTextVal(row.tf)+" ")
				return nil
			default:
				if len(msg.Runes) > 0 {
					m.setAIText(row.tf, m.aiTextVal(row.tf)+string(msg.Runes))
					return nil
				}
			}
		}
		switch k {
		case "up", "k":
			m.cursor = (m.cursor - 1 + n) % n
		case "down", "j", "tab":
			m.cursor = (m.cursor + 1) % n
		case "shift+tab":
			m.focus = 0
		case "enter", " ":
			if isText {
				return nil
			}
			return m.activateNav(m.cursor)
		case "left", "right":
			if isText {
				return nil
			}
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

// cyclePreselect steps the card grid's preselected start variant. It
// only moves the visual preselection — the configured start option
// (settings pane) is untouched; double-click keeps starting with it.
func (m *Model) cyclePreselect(forward bool) {
	step := 1
	if !forward {
		step = -1
	}
	for i, o := range startOptions {
		if o == m.startSel {
			m.startSel = startOptions[(i+step+len(startOptions))%len(startOptions)]
			return
		}
	}
	m.startSel = startOptions[0] // unknown/empty: land on the first variant
}

func (m *Model) activateNav(i int) tea.Cmd {
	if i < 0 || i >= len(m.navRows) {
		return nil
	}
	row := m.layoutRows[m.navRows[i]]
	if row.save {
		return m.save()
	}
	if row.tf != "" {
		return nil // text rows never activate options (#103)
	}
	if row.section == "_confirm" {
		return m.resolveAIConfirm(row.value == "Replace")
	}
	if row.section != "" {
		m.applyOption(row.section, row.value)
	}
	return nil
}

// aiTextVal returns the capture value of a Custom-provider text field.
func (m *Model) aiTextVal(tf string) string {
	switch tf {
	case "name":
		return m.aiName
	case "endpoint":
		return m.aiEndpoint
	case "token":
		return m.aiToken
	}
	return ""
}

func (m *Model) setAIText(tf, v string) {
	switch tf {
	case "name":
		m.aiName = v
	case "endpoint":
		m.aiEndpoint = v
	case "token":
		m.aiToken = v
	}
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
		switch {
		case m.aiSel:
			cur = "Custom"
		case m.staged.ai == "none":
			cur = "none"
		default:
			cur = "" // legacy stored id: shown read-only, not an option
		}
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
			s.modes = withoutMode(s.modes, value)
		} else {
			// live mutual exclusion (newproject parity): offline and
			// cbm_ui can never be staged together.
			switch value {
			case "offline":
				s.modes = withoutMode(s.modes, "cbm_ui")
			case "cbm_ui":
				s.modes = withoutMode(s.modes, "offline")
			}
			s.modes = append(s.modes, value)
		}
	case "start":
		s.start = value
		m.startSel = value // settings change re-preselects the card
	case "vcs":
		s.vcs = value
	case "ai":
		if value == "Custom" && !m.aiSel {
			// first entry into the Custom capture: prefill Name /
			// Endpoint from the stored id's opencode.json section
			// (token always empty on load, #103).
			if m.selected >= 0 && m.selected < len(m.projects) {
				name, endpoint := providerPrefill(m.projects[m.selected].Path, m.staged.ai)
				m.aiName, m.aiEndpoint = name, endpoint
			}
		}
		m.aiSel = value == "Custom"
		m.aiTouched = true
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
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		dir := 1
		if msg.Button == tea.MouseButtonWheelUp {
			dir = -1
		}
		switch m.state {
		case stateList:
			m.layout()
			m.follow.Wheel(&m.pscroll, dir)
			m.pscroll = min(m.pscroll, m.paneScrollMax) // paneScrollMax is fresh from layout
			m.gridFollow.Wheel(&m.gridScroll, dir)
		}
		return nil
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
	}
	m.layout()
	n := m.dbl.Press(msg)
	for _, cr := range m.cardRects {
		if cr.rect.Hit(msg.X, msg.Y) {
			if cr.idx != m.selected {
				m.selected = cr.idx
				m.focus = 0
				m.loadStaged()
				return nil
			}
			// selected card: a button click starts with that variant,
			// double click with the configured one
			if v, ok := m.hitStartButton(msg.X, msg.Y); ok {
				return m.beginStart(cr.idx, v)
			}
			if n == 2 {
				return m.beginStart(cr.idx, m.staged.start)
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
//
// beginStart does not touch podman on the event loop: the reconcile
// (last_used bump, live status, image check) runs in a background cmd
// and the flow continues from applyReconcile. The variant picks the
// access the start resolves to (card button click, Enter preselection,
// or the configured start option); empty falls back to the config.
func (m *Model) beginStart(idx int, variant string) tea.Cmd {
	if idx < 0 || idx >= len(m.projects) {
		return nil
	}
	if m.reconciling { // double click already in flight
		return nil
	}
	p := m.projects[idx]
	m.pending = p
	m.variant = variant
	m.reconciling = true
	m.toast = ""
	m.reg.UpdateLastUsed(p.Path) // bump BEFORE anything else (file write)
	return m.reconcileCmd(p)
}

// reconcileMsg carries the reconcile result back to the model thread.
type reconcileMsg struct {
	p       registry.Project
	live    bool
	imageOK bool
	cfg     sandboxcfg.Config
	cfgErr  string
	lastErr string
}

// reconcileCmd runs the event-loop-blocking part of the start flow
// (podman ps, podman image exists, sandbox_config.json read) in a
// background goroutine — podman stalls must never freeze the UI.
func (m *Model) reconcileCmd(p registry.Project) tea.Cmd {
	deps := m.deps
	return func() tea.Msg {
		msg := reconcileMsg{p: p}
		// 2. Reconcile stored container_status against live podman ps.
		running := discovery.RunningContainers(deps.Runner)
		msg.live = hasString(running, registry.ContainerName(p.ContainerID))
		if err := m.reg.UpdateStatus(p.Path, statusOf(msg.live)); err != nil {
			msg.lastErr = "Could not update container status: " + err.Error()
			return msg
		}
		cfg, err := sandboxcfg.Read(sandboxcfg.Path(p.Path))
		if err != nil {
			msg.cfgErr = "Cannot read sandbox_config.json: " + err.Error()
			return msg
		}
		msg.cfg = cfg
		// 3. Missing edition image → Build now / Build later / Go back.
		msg.imageOK = discovery.ImageExists(deps.Runner, cfg.ContainerEdition)
		return msg
	}
}

// applyReconcile continues the start flow on the model thread.
func (m *Model) applyReconcile(msg reconcileMsg) tea.Cmd {
	m.reconciling = false
	if msg.lastErr != "" {
		m.toast = msg.lastErr
		return nil
	}
	p := msg.p
	m.refresh()
	m.selectByID(p.ContainerID)
	dots := m.poll() // fresh dots from the reconcile's own podman result
	if msg.cfgErr != "" {
		m.variant = ""
		m.toast = msg.cfgErr
		return dots
	}
	variant := m.variant // empty falls back to the configured option in access
	if msg.live {
		return tea.Batch(dots, m.access(p, msg.cfg, variant, false))
	}
	if !msg.imageOK {
		m.state = stateBuild
		m.buildSel = 0
		m.pendingCfg = msg.cfg
		return dots
	}
	return tea.Batch(dots, m.launchStart(p, msg.cfg, variant))
}

// pollRunning refreshes the cached running-container list off the event
// loop; the result lands as runningMsg.
func (m *Model) pollRunning() tea.Cmd {
	run := m.deps.Runner
	return func() tea.Msg { return runningMsg(discovery.RunningContainers(run)) }
}

// poll schedules a background refresh without blocking anything.
func (m *Model) poll() tea.Cmd { return m.pollRunning() }

func statusOf(live bool) string {
	if live {
		return "running"
	}
	return "stopped"
}

func (m *Model) launchStart(p registry.Project, cfg sandboxcfg.Config, variant string) tea.Cmd {
	if !ValidModes(cfg.ContainerModes) {
		m.toast = "offline and cbm_ui are mutually exclusive — fix settings first"
		m.state = stateList
		return nil
	}
	m.variant = variant
	m.state = stateBusy
	m.busyKind = opStart
	m.pending = p
	m.pendingCfg = cfg
	m.opTitle = "Starting container"
	m.log = shell.NewLogPane().Run(true)
	m.busyID++
	m.busyKey = p.ContainerID
	args := StartArgs(p.Path, cfg.ContainerEdition, p.ContainerID, cfg.ContainerModes, variant, cfg.UseProxy)
	run, script := m.deps.Runner, m.deps.StartScript
	op := Op{
		ID:    m.busyID,
		Key:   p.ContainerID, // per-project queue bucket (#82)
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
		m.busyKey = m.pending.ContainerID
		ed, pid, run, script := m.pendingCfg.ContainerEdition, m.pending.ContainerID, m.deps.Runner, m.deps.BuildScript
		op := Op{
			ID:    m.busyID,
			Key:   pid, // per-project queue bucket (#82)
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
	default: // Go back — cancel-on-navigate (#82): drop the queued build op
		if m.deps.Tasks != nil {
			m.deps.Tasks.CancelQueued(m.pending.ContainerID)
		}
		m.state = stateList
		return nil
	}
}

func (m *Model) opDone(msg OpDoneMsg) tea.Cmd {
	if msg.ID != m.busyID {
		return nil
	}
	if d, ok := any(msg).(taskmgr.DoneMsg); ok && d.Key != "" && d.Key != m.busyKey {
		return nil // foreign view's op reusing this view's numeric id
	}
	if m.busyKind == opAI { // settings-run provider steps stay in the pane
		return m.aiDone(msg)
	}
	if m.state != stateBusy {
		return nil
	}
	// Streaming transport already fed the pane; batch launchers flush here.
	if len(m.log.Lines()) == 0 {
		for _, l := range msg.Lines {
			m.log, _ = m.log.Update(shell.LineMsg(l))
		}
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
			m.state = stateList
			// First-run setup resume (#81): on a start with
			// setup_complete still false the shell's setup hook takes
			// over after the access lands (machine runs the pending
			// stages; StageStart reuses the running container).
			return tea.Batch(m.poll(), m.access(p, m.pendingCfg, m.variant, !m.pendingCfg.SetupComplete))
		}
		m.variant = ""
		m.toast = "Failed to start container"
		m.state = stateList
		return m.poll()
	default: // opBuild
		if msg.Err == nil {
			// Image check off the event loop: re-reconcile; it launches
			// the start when the image is really there.
			m.toast = "Image built"
			return m.reconcileCmd(m.pending)
		}
		m.variant = ""
		m.toast = "Build failed — image still unavailable"
		m.state = stateList
		return nil
	}
}

// access resolves the chosen start variant directly (card flow, no
// access prompt): console/OpenCode via terminal handoff, Web as the
// published-URL toast (the server must have been started with web).
// An empty variant falls back to the configured start option. The
// first-run recovery shell hook fires with the access — after a
// handoff (path queued for accessDone), otherwise inline.
func (m *Model) access(p registry.Project, cfg sandboxcfg.Config, variant string, firstRun bool) tea.Cmd {
	if variant == "" {
		variant = cfg.StartOption
	}
	if variant == "opencode" || variant == "console" {
		// Handoff while background tasks run is guarded (#82):
		// tea.ExecProcess suspends the whole UI; queuing would
		// reconcile with the still-running container ops — the guard
		// keeps them visible instead.
		if m.hasOtherWork() {
			m.toast = "Background tasks are running — wait for them, then retry"
			return nil
		}
		m.pending = p
		m.pendingCfg = cfg
		if firstRun && !cfg.SetupComplete {
			m.resumeRecover = p.Path
		}
		arg := "bash"
		if variant == "opencode" {
			arg = "opencode"
		}
		cmd := exec.Command("podman", "exec", "-it", "--user", "dev", registry.ContainerName(p.ContainerID), arg)
		return m.deps.Exec(cmd, func(err error) tea.Msg { return accessDoneMsg{Err: err} })
	}
	// web: show the published URL (never open a browser)
	port := discovery.WebPort(m.deps.Runner, registry.ContainerName(p.ContainerID))
	if port != "" {
		m.toast = "OpenCode Web: http://" + port
	} else {
		m.toast = "OpenCode Web is not published for this project"
	}
	if firstRun && !cfg.SetupComplete { // no handoff here — recover right away
		return func() tea.Msg { return shell.RecoverMsg{Path: p.Path} }
	}
	return nil
}

// hasOtherWork reports whether the shared task manager runs work OTHER
// than this view's own (guard for the terminal handoff — nil manager or
// an idle key passes through).
func (m *Model) hasOtherWork() bool {
	return m.deps.Tasks != nil && m.deps.Tasks.Busy()
}

// --- change-settings surface (ADR 0018, #63) ---

func (m *Model) save() tea.Cmd {
	if m.aiRunningOp || m.aiConfirm != nil {
		m.toast = "AI provider configuration is running — wait for it to finish"
		return nil
	}
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
	// ONE whole-config atomic rewrite; applies on next start. The AI
	// value defaults to the stored id (barely-read rows keep the
	// recorded provider untouched).
	settings := sandboxcfg.Settings{
		Edition:     s.edition,
		Modes:       s.modes,
		StartOption: s.start,
		VCS:         s.vcs,
		AIProvider:  s.ai,
		UseProxy:    s.proxy,
	}
	if m.aiTouched && m.aiSel { // Custom capture with the wizard's save semantics (#103)
		return m.saveCustom(p)
	}
	if m.aiTouched {
		settings.AIProvider = "none"
	}
	return m.applySettings(p.Path, settings)
}

// saveCustom translates the Custom capture into save semantics: empty
// token re-merges only when the id already has credentials (otherwise
// refused); a typed token writes both steps, gated by Keep/Replace
// when the credential already exists. The whole-config rewrite is
// deferred until every step passed (a failed write leaves the stored
// config and the setup flag untouched).
func (m *Model) saveCustom(p registry.Project) tea.Cmd {
	if !providerid.ValidName(m.aiName) {
		m.toast = "Name must not be empty and needs at least one letter or digit."
		return nil
	}
	if !providerid.ValidEndpoint(m.aiEndpoint) {
		m.toast = "Endpoint must be an http:// or https:// URL without whitespace (e.g. https://api.example.com/v1)."
		return nil
	}
	if m.aiToken != "" && !providerid.ValidToken(m.aiToken) {
		m.toast = "Token must not be empty or contain whitespace/newlines."
		return nil
	}
	root := p.Path
	id := providerid.Slug(m.aiName)
	idExists := authJSONHasID(root, id)
	merge := []string{"opencode-config", root, "--id", id,
		"--name", m.aiName, "--endpoint", m.aiEndpoint}
	steps := [][]string{}
	if m.aiToken == "" {
		if !idExists {
			m.toast = "Token required — no existing credential entry for '" + id +
				"' was found in auth.json; type a token to configure this provider."
			return nil
		}
		steps = append(steps, merge) // re-merge only
	} else {
		steps = append(steps, []string{"ai-provider", root, "--name", m.aiName,
			"--endpoint", m.aiEndpoint, "--token", m.aiToken})
		steps = append(steps, merge)
		if idExists { // credential exists: the Keep/Replace confirm gates it
			m.aiConfirm = &aiConfirmState{id: id}
			return nil
		}
	}
	return m.queueAI(root, id, steps)
}

// applySettings performs the immediate whole-config rewrite (no
// credential steps pending).
func (m *Model) applySettings(root string, settings sandboxcfg.Settings) tea.Cmd {
	if err := sandboxcfg.Rewrite(m.deps.Home, sandboxcfg.Path(root), settings); err != nil {
		m.toast = "Could not save settings: " + err.Error()
		return nil
	}
	m.loadStaged()
	m.toast = "Settings saved — apply on next start"
	return nil
}

// aiConfirmState carries the pending Keep/Replace decision.
type aiConfirmState struct {
	id string // provider id with an existing auth.json entry
}

// resolveAIConfirm applies the confirm: Keep existing skips the token
// write (opencode.json merge only); Replace writes both steps with
// --replace.
func (m *Model) resolveAIConfirm(replace bool) tea.Cmd {
	root := m.projects[m.selected].Path
	id := providerid.Slug(m.aiName)
	steps := [][]string{{"opencode-config", root, "--id", id,
		"--name", m.aiName, "--endpoint", m.aiEndpoint}}
	if replace {
		steps = [][]string{{"ai-provider", root, "--name", m.aiName,
			"--endpoint", m.aiEndpoint, "--token", m.aiToken, "--replace"}, steps[0]}
	}
	m.aiConfirm = nil
	return m.queueAI(root, id, steps)
}

// queueAI launches the first configure-project.sh step through the
// existing op queue (same Ops seam and per-project key as start/build);
// the step continuation chains until the queue empties, then applies
// the deferred whole-config rewrite.
func (m *Model) queueAI(root, id string, steps [][]string) tea.Cmd {
	m.aiSteps = steps
	m.aiSaveCfg = &sandboxcfg.Settings{
		Edition:     m.staged.edition,
		Modes:       m.staged.modes,
		StartOption: m.staged.start,
		VCS:         m.staged.vcs,
		AIProvider:  id,
		UseProxy:    m.staged.proxy,
	}
	var p registry.Project
	if m.selected >= 0 && m.selected < len(m.projects) {
		p = m.projects[m.selected]
	}
	return m.launchAIStep(p)
}

// launchAIStep runs one step through the op queue and returns the done
// continuation.
func (m *Model) launchAIStep(p registry.Project) tea.Cmd {
	args := m.aiSteps[0]
	m.aiRunningOp = true
	m.busyID++
	m.busyKey = p.ContainerID
	m.busyKind = opAI
	run := m.deps.Runner
	script := m.deps.ConfigureScript
	m.toast = "Configuring AI provider…"
	op := Op{
		ID:    m.busyID,
		Key:   p.ContainerID, // per-project queue bucket (#82)
		Title: "Configuring AI provider",
		Run: func(line func(string)) error {
			out, err := run.Output("bash", append([]string{script}, args...)...)
			for _, l := range cmdrun.Lines(out) {
				line(l)
			}
			return err
		},
	}
	return tea.Batch(m.deps.Ops.Launch(op))
}

// aiDone processes an op-queue completion of a settings-run provider
// step: on failure the toast surfaces it and nothing was rewritten
// (the setup flag and the stored config stay stale-checked); on
// success the next queued step launches, and the last one applies the
// deferred rewrite.
func (m *Model) aiDone(msg OpDoneMsg) tea.Cmd {
	m.aiRunningOp = false
	if msg.Err != nil {
		m.aiSteps, m.aiSaveCfg = nil, nil
		m.toast = "AI provider setup failed: " + msg.Err.Error() +
			" — no settings were written, fix and save again."
		m.loadStaged() // re-sync the pane with disk truth (stale-check parity)
		return nil
	}
	var p registry.Project
	if m.selected >= 0 && m.selected < len(m.projects) {
		p = m.projects[m.selected]
	}
	root := p.Path
	m.aiSteps = m.aiSteps[1:]
	if len(m.aiSteps) > 0 {
		return m.launchAIStep(p)
	}
	cfg := m.aiSaveCfg
	if cfg == nil {
		return nil
	}
	set := *cfg
	m.aiSaveCfg = nil
	return m.applySettings(root, set)
}

// authJSONHasID reports whether the project-local auth.json already has
// a credential entry under the provider id (id-parameterized
// replacement-confirm check, spec #100).
func authJSONHasID(root, id string) bool {
	data, err := os.ReadFile(root + "/.opencode_data/auth.json")
	if err != nil {
		return false
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(data, &doc) != nil {
		return false
	}
	_, has := doc[id]
	return has
}

// providerPrefill reads the stored id's provider section in the
// project-local opencode.json: the display name and baseURL prefill
// Name/Endpoint when the user opens the Custom capture.
func providerPrefill(root, storedID string) (name, endpoint string) {
	if storedID == "" || storedID == "none" {
		return "", ""
	}
	data, err := os.ReadFile(root + "/.opencode_config/opencode.json")
	if err != nil {
		return "", ""
	}
	var doc struct {
		Provider map[string]struct {
			Name    string `json:"name"`
			Options struct {
				BaseURL string `json:"baseURL"`
			} `json:"options"`
		} `json:"provider"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return "", ""
	}
	if p, ok := doc.Provider[storedID]; ok {
		return p.Name, p.Options.BaseURL
	}
	return "", ""
}

// --- layout ---

func (m *Model) cardsPerRow() int {
	return max(1, (leftWidth(m.width)-2)/shell.CardStride)
}

// paneWidth / leftWidth size the two panes: settings pane clamped to
// 36..48 columns, the rest of the width goes to the wrapping card grid.
func paneWidth(width int) int { return clamp(width/2, 36, 48) }
func leftWidth(width int) int { return max(shell.CardWidth, width-paneWidth(width)-1) }

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
	m.paneScrollMax = max(0, len(rows)-m.height)
	for i, r := range rows {
		if r.section != "" || r.save || r.tf != "" {
			m.navRows = append(m.navRows, i)
		}
	}
	if len(m.navRows) > 0 {
		m.cursor = clamp(m.cursor, 0, len(m.navRows)-1)
		focused := m.navRows[m.cursor]
		m.follow.Pull(&m.pscroll, m.cursor, focused, len(rows), m.height) // the focus pull only on a cursor change
		m.navRects = make([]shell.Rect, len(m.navRows))
		for i, rowIdx := range m.navRows {
			if rowIdx >= m.pscroll && rowIdx < m.pscroll+m.height {
				m.navRects[i] = shell.Rect{X: paneX, Y: BodyTop + rowIdx - m.pscroll, W: paneW, H: 1}
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
	add(shell.StyleMutedAlt.Render(divider(min(paneW-2, 30)))) // separate the pane from the options above
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
	// AI provider row (provider-neutral, #103): only none/Custom are
	// offered; a stored id not in the option list (legacy gwdg-saia or
	// an older custom id) presents read-only with a reconfigure hint —
	// the stored-edition-vanished pattern.
	if aiLegacy := s.ai != "" && s.ai != "none"; aiLegacy {
		add(shell.StyleWarn.Render(truncate("stored provider '"+s.ai+
			"' no longer offered", paneW-2)))
		add(shell.StyleMutedAlt.Render(truncate(
			"reconfigure via Custom or switch to none", paneW-2)))
	}
	addSection("AI provider", "ai", aiOptions, nil, func(o string) bool {
		if o == "Custom" {
			return m.aiSel
		}
		return !m.aiSel && (s.ai == "none" || s.ai == "")
	})
	if m.aiSel { // Custom capture block: Name / Endpoint / Token in order
		add(shell.StyleFieldLabel.Render("Custom provider"))
		addTF := func(label, tf, val string) {
			rows = append(rows, paneRow{
				text: shell.StyleFieldLabel.Render(label + ": " + shell.StyleCurrentVal.Render(truncate(val, paneW-10))),
				tf:   tf})
		}
		addTF("Name", "name", m.aiName)
		addTF("Endpoint", "endpoint", m.aiEndpoint)
		addTF("Token", "token", maskedToken(m.aiToken))
	}
	if m.aiConfirm != nil { // Keep/Replace gating on an existing credential
		add(shell.StyleWarn.Render(truncate("Credentials for '"+m.aiConfirm.id+
			"' exist in auth.json — Keep existing keeps the stored token.", paneW-2)))
		for _, o := range []string{"Keep existing", "Replace"} {
			rows = append(rows, paneRow{text: shell.StyleMutedAlt.Render(shell.SymOff + " " + o),
				section: "_confirm", value: o})
		}
	}
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
		if i < m.pscroll || i >= m.pscroll+height {
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
				// The block lands at paneX (left pane is leftWidth wide,
				// then the indent prefix); the rect must be screen-space.
				m.promptRects = append(m.promptRects, shell.Rect{
					X: leftWidth(m.width) + indent, Y: BodyTop + len(lines), W: min(paneW, shell.Width(bl)), H: 1,
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
	m.startBtnRects = nil
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
		// the selected row pulls the scroll only on a selection change
		m.gridFollow.Pull(&m.gridScroll, m.selected, selRow, totalRows, visRows)
		running := m.running // cached (background-polled); never queried inline
		idxs := []int{}
		for r := m.gridScroll; r < totalRows && r-m.gridScroll < visRows; r++ {
			for c := 0; c < cpr; c++ {
				idx := r*cpr + c
				if idx >= len(m.projects) {
					break
				}
				idxs = append(idxs, idx)
			}
		}
		gridRects := shell.CardRects(1, BodyTop, len(idxs), cpr, 1)
		var grid []string
		for k, idx := range idxs {
			p := m.projects[idx]
			rows := m.cardRows(p, hasString(running, registry.ContainerName(p.ContainerID)))
			frame := shell.Card // selected card keeps the accent border
			if idx != m.selected {
				frame = shell.CardMuted // unselected: muted frame (build view)
			}
			card := strings.Split(frame(rows), "\n")
			c, r := k%cpr, k/cpr
			baseY := r * rowStride
			grid = pasteLines(grid, 1+c*shell.CardStride, baseY, card)
			m.cardRects = append(m.cardRects, cardRect{rect: gridRects[k], idx: idx})
			if idx == m.selected {
				m.recordStartButtons(gridRects[k])
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
	var name string
	sym, st := shell.SymOff, shell.StyleStatusStopped
	if running {
		sym, st = shell.SymOn, shell.StyleStatusRunning
	}
	if m.deps.Tasks != nil {
		if title, ok := m.deps.Tasks.RunningTitle(p.ContainerID); ok {
			// still-running indicator for the owning project's op (#82)
			name = truncate(p.Name+" ◌ "+title, cardInnerW-2)
		}
	}
	if name == "" {
		// Leave 2 cells for the status symbol + space so the row fits
		// cardInnerW and the card frame measures exactly CardWidth.
		name = truncate(p.Name, cardInnerW-2)
	}
	rows := []string{
		st.Render(sym) + " " + name,
		truncate(p.Path, cardInnerW),
	}
	if m.selected == indexOfProject(m.projects, p.ContainerID) {
		for i, r := range rows {
			rows[i] = shell.StyleSelectedRow.Render(padTo(cardInnerW, r))
		}
		rows = append(rows,
			shell.StyleMutedAlt.Render(padTo(cardInnerW, divider(24))),
			padTo(cardInnerW, startButtonRow(m.startSel, true)))
	} else {
		for i, r := range rows {
			rows[i] = padTo(cardInnerW, r)
		}
		rows = append(rows,
			shell.StyleMutedAlt.Render(padTo(cardInnerW, divider(24))),
			padTo(cardInnerW, startButtonRow(m.staged.start, false)))
	}
	return rows
}

// startButtons are the card start buttons in startOptions order.
var startButtons = []string{"Console", "OpenCode", "Web"}

// start-button styles: the preselected variant is accent-filled, the
// others idle on the selected card and muted on unselected cards.
var (
	startBtnSel  = lipgloss.NewStyle().Background(shell.ColAccent).Foreground(lipgloss.Color("0")).Bold(true)
	startBtnIdle = lipgloss.NewStyle().Foreground(shell.ColCardFg)
)

// startButtonRow renders the card's start buttons with 2-space gaps:
// Console  OpenCode  Web. pre selects the filled button (a start
// variant name); only the selected card fills one.
func startButtonRow(pre string, activeCard bool) string {
	parts := make([]string, len(startButtons))
	pad := ""
	for i, lbl := range startButtons {
		s := shell.StyleMutedAlt.Render(lbl)
		if activeCard {
			s = startBtnIdle.Render(lbl)
			if startOptions[i] == pre {
				s = startBtnSel.Render(lbl)
			}
		}
		parts[i] = pad + s
		pad = "  "
	}
	return strings.Join(parts, "")
}

// recordStartButtons pins the start-button hit rects on the selected
// card's button row (content row 4, two cells in from the border):
// label order and 2-space gaps mirror startButtonRow exactly.
func (m *Model) recordStartButtons(rc shell.Rect) {
	x, y, w := rc.X+2, rc.Y+4, 0
	m.startBtnRects = nil
	for _, lbl := range startButtons {
		rect := shell.Rect{X: x + w, Y: y, W: shell.Width(lbl), H: 1}
		m.startBtnRects = append(m.startBtnRects, rect)
		w += rect.W + 2
	}
}

// hitStartButton resolves a click on the selected card's button row to
// its start variant.
func (m *Model) hitStartButton(x, y int) (string, bool) {
	if len(m.startBtnRects) == 0 {
		return "", false
	}
	for i, r := range m.startBtnRects {
		if r.Hit(x, y) {
			return startOptions[i], true
		}
	}
	return "", false
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
	// MaxWidth clips wide runes that bypass Width's minimum, so rows
	// never exceed w and the card frame always wraps its content.
	return lipgloss.NewStyle().Width(w).MaxWidth(w).Render(s)
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

// maskedToken renders a capture token without echoing the secret:
// one bullet per character, or the muted empty marker.
func maskedToken(token string) string {
	if token == "" {
		return "…"
	}
	return strings.Repeat("•", len([]rune(token)))
}
