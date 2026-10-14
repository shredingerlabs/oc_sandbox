// Package containerview is the Container center view (#79): Build and
// Stop sub-tabs on the app shell (#76). Build offers the live edition
// registry (ADR 0019) plus the "all" pseudo-option as educational cards
// and streams build-container.sh output into the embedded log pane —
// with no config writes (data-map §3). Stop lists the live running
// containers as a checklist and stops them via podman stop, writing
// container_status="stopped" only after the container is verified gone.
// Both long operations launch through the Launcher seam so the
// background-task manager (#82) can swap it.
//
// Use *Model as the shell.CenterView: the shell keeps its views for the
// whole run and only polls them, so Update mutates the model in place.
package containerview

import (
	"fmt"
	"math"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// bodyTop is the screen row where the shell places Body output (header
// 0, separator 1, sub-tabs 2); the view pins content at x=1 (its own
// left padding, like the approved prototype).
const (
	bodyTop = 3
	padX    = 1
)

// Op is one long operation: emit reports an output line, the returned
// error reports failure.
type Op func(emit func(string)) error

// OpDoneMsg reports completion of a long op with its buffered output
// lines. The SyncLauncher placeholder delivers them in one message; the
// real per-line streaming (shell.LineMsg) lands with the background-task
// manager (#82).
type OpDoneMsg struct {
	Err   error
	Lines []string
}

// Launcher is the thin seam every long operation goes through: it turns
// an Op into a tea.Cmd that reports progress and completion. #82's
// background-task manager swaps this for real async streaming; the view
// only ever calls launch(op).
type Launcher func(Op) tea.Cmd

// SyncLauncher is the pre-#82 placeholder launcher: the returned
// tea.Cmd runs the op to completion and reports all output lines in the
// final OpDoneMsg.
func SyncLauncher(op Op) tea.Cmd {
	return func() tea.Msg {
		var lines []string
		err := op(func(s string) { lines = append(lines, s) })
		return OpDoneMsg{Err: err, Lines: lines}
	}
}

// RunningMsg carries a fresh running-container list (podman ps) back
// from a refresh cmd.
type RunningMsg []string

// Sub-tab indices.
const (
	TabBuild = 0
	TabStop  = 1
)

var subTabItems = []string{"Build Container", "Stop Container"}

// descriptions are the educational one-liners per known edition; unknown
// stages get the generic line.
var descriptions = map[string]string{
	"all":      "builds every edition plus the egress proxy",
	"base":     "ubuntu foundation every other edition builds on",
	"web":      "base with the OpenCode Web UI",
	"embedded": "base plus embedded toolchains",
	"swdev":    "C/C++, Java, Rust, Go development",
	"matlab":   "GNU Octave plus the Python scientific stack",
	"ros2":     "ROS 2 Jazzy robotics stack",
	"writing":  "LibreOffice and pandoc writing tools",
}

const defaultDescription = "container edition from the Dockerfile stage"

// checklistFixedRows is the row budget of the stop checklist that is
// fixed regardless of the container count: select-all row, blank row,
// 3 button lines, toast.
const checklistFixedRows = 6

func description(edition string) string {
	if d, ok := descriptions[edition]; ok {
		return d
	}
	return defaultDescription
}

// Model implements shell.CenterView with the Build and Stop sub-tabs.
// All podman / script calls go through the injected runner; the
// registry receives verified container_status updates only. Queue is
// the shared background-task manager (#82): when wired, every op runs
// through it (one op at a time per key, streaming lines); without it
// the view falls back to the injected Launch function (SyncLauncher).
type Model struct {
	Run              cmdrun.Runner
	Registry         *registry.Registry
	Dockerfile       string
	DockerfileCustom string
	BuildScript      string
	Launch           Launcher
	Queue            *taskmgr.Manager

	opID         int    // view-assigned handle echoed in DoneMsg/LineEvent filters
	opKey        string // queue key of the in-flight op (id-disambiguation)
	sub          int
	buildSel     int
	stopSel      int
	stopScroll   int          // hidden head rows of the stop checklist
	stopFollow   shell.Follow // wheel + selection-follow of the stop checklist
	editions     []string
	discoveryErr string
	running      []string
	selection    map[string]bool
	log          shell.LogPane
	opKind       string // "" idle, "build" / "stop" while an op runs
	toast        string
	dbl          shell.DblClick
	confirm      shell.Confirm // shared confirmation popup (ADR-0021)

	// Hit rects, stored at render time in screen space (regression
	// tests pin them).
	cardRects []shell.Rect
	buildBtn  shell.Rect
	selAll    shell.Rect
	rowRects  []shell.Rect
	stopBtn   shell.Rect

	// stopIdx maps stop-tab rowRects to indices into m.running (the
	// checklist scrolls off head rows).
	stopIdx []int
}

// New returns the Container view: live edition discovery from the
// Dockerfiles, build-container.sh as the build entry point, and the
// registry that receives verified container_status updates. launch may
// be nil (SyncLauncher placeholder).
func New(run cmdrun.Runner, reg *registry.Registry, dockerfile, dockerfileCustom, buildScript string, launch Launcher) *Model {
	return &Model{
		Run:              run,
		Registry:         reg,
		Dockerfile:       dockerfile,
		DockerfileCustom: dockerfileCustom,
		BuildScript:      buildScript,
		Launch:           launch,
		selection:        map[string]bool{},
		stopFollow:       shell.NewFollow(false, false),
		log:              shell.NewLogPane(),
	}
}

// Busy reports whether a long operation is running — the guarded-exit
// query the root polls before quitting.
func (m *Model) Busy() bool { return m.opKind != "" }

// SubTabs implements shell.CenterView.
func (m *Model) SubTabs() ([]string, int) { return subTabItems, m.sub }

// TextFocused implements shell.CenterView (no text inputs here).
func (m *Model) TextFocused() bool { return false }

// ConfirmOpen implements shell.ConfirmView (ADR-0021): the build popup.
func (m *Model) ConfirmOpen() bool { return m.confirm.IsOpen() }

// options returns the buildable editions incl. the "all" pseudo-option
// (bash build-wizard parity).
func (m *Model) options() []string {
	return append([]string{"all"}, m.editions...)
}

// refreshEditions re-runs live edition discovery (fresh per call, data
// map §3); the error is shown loudly on the Build tab.
func (m *Model) refreshEditions() {
	eds, err := discovery.Editions(m.Dockerfile, m.DockerfileCustom)
	if err != nil {
		m.discoveryErr = err.Error()
		return
	}
	m.discoveryErr = ""
	m.editions = eds
	if n := len(m.editions) + 1; m.buildSel >= n {
		m.buildSel = n - 1
	}
}

// refreshRunning returns the cmd that polls the live running containers.
func (m *Model) refreshRunning() tea.Cmd {
	return func() tea.Msg { return RunningMsg(discovery.RunningContainers(m.Run)) }
}

// Update implements shell.CenterView.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case shell.SubTabMsg:
		if msg.Index < 0 || msg.Index >= len(subTabItems) {
			return nil
		}
		if msg.Index != m.sub {
			// cancel-on-navigate (#82): switching this view's tab drops the
			// queued (not running) build ops of the edition keys
			if m.Queue != nil {
				for _, opt := range m.options() {
					m.Queue.CancelQueued("cv-build:" + opt)
				}
			}
		}
		m.sub = msg.Index
		if m.sub == TabStop {
			m.stopScroll = 0
			return m.refreshRunning()
		}
		return nil
	case RunningMsg:
		m.running = msg
		for name := range m.selection {
			if !slices.Contains(msg, name) {
				delete(m.selection, name)
			}
		}
		if m.stopSel >= len(msg) {
			m.stopSel = max(0, len(msg)-1)
		}
		m.stopFollow.Arm() // keep the selection visible in the new list
		m.stopFollow.Force()
		return nil
	case OpDoneMsg:
		return m.opDone(msg)
	case taskmgr.DoneMsg:
		if msg.ID == m.opID && m.opKind != "" && (msg.Key == "" || msg.Key == m.opKey) { // this view's op only (Key disambiguates reused numeric ids)
			return m.opDone(OpDoneMsg{Err: msg.Err, Lines: msg.Lines})
		}
		return nil
	case taskmgr.LineEvent:
		if msg.ID == m.opID && m.opKind != "" && (msg.Key == "" || msg.Key == m.opKey) {
			var cmd tea.Cmd
			m.log, cmd = m.log.Update(shell.LineMsg(msg.Text))
			return cmd
		}
		return nil
	case tea.KeyMsg:
		if m.confirm.IsOpen() { // the popup consumes keys (ADR-0021)
			return m.confirm.Key(msg)
		}
		if m.opKind != "" {
			m.log, _ = m.log.Update(msg) // log scrolling while the op runs
			return nil
		}
		m.refreshEditions()
		m.toast = ""
		return m.key(msg)
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if m.confirm.IsOpen() { // popup buttons & outside-cancel first
				if cmd, ok := m.confirm.Click(msg.X, msg.Y); ok {
					return cmd
				}
			}
			return m.click(msg)
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			if m.opKind != "" {
				var cmd tea.Cmd
				m.log, cmd = m.log.Update(msg)
				return cmd
			}
			if m.sub == TabStop && len(m.running) > 0 {
				dir := 1
				if msg.Button == tea.MouseButtonWheelUp {
					dir = -1
				}
				m.stopFollow.Wheel(&m.stopScroll, dir)
			}
			return nil
		}
		m.log, _ = m.log.Update(msg)
		return m.log.Spin()
	default:
		var cmd tea.Cmd
		m.log, cmd = m.log.Update(msg)
		return cmd
	}
}

// key handles keyboard parity: tab/shift+tab and left/right switch the
// sub-tabs, up/down move the selection, enter activates (build / stop),
// space toggles the focused container.
func (m *Model) key(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "tab", "right", "l":
		m.sub = (m.sub + 1) % len(subTabItems)
		if m.sub == TabStop {
			return m.refreshRunning()
		}
	case "shift+tab", "left", "h":
		m.sub = (m.sub - 1 + len(subTabItems)) % len(subTabItems)
		if m.sub == TabStop {
			return m.refreshRunning()
		}
	case "up", "k":
		if m.sub == TabBuild {
			if opts := m.options(); len(opts) > 0 {
				m.buildSel = (m.buildSel - 1 + len(opts)) % len(opts)
			}
		} else if m.stopSel > 0 {
			m.stopSel--
			m.stopFollow.Arm()
		}
	case "down", "j":
		if m.sub == TabBuild {
			if opts := m.options(); len(opts) > 0 {
				m.buildSel = (m.buildSel + 1) % len(opts)
			}
		} else if m.stopSel < len(m.running)-1 {
			m.stopSel++
			m.stopFollow.Arm()
		}
	case " ":
		if m.sub == TabStop && m.stopSel < len(m.running) {
			name := m.running[m.stopSel]
			m.selection[name] = !m.selection[name]
		}
	case "enter":
		if m.sub == TabBuild {
			opts := m.options()
			if len(opts) == 0 {
				return nil
			}
			return m.startBuild(opts[m.buildSel])
		}
		return m.startStop()
	}
	return nil
}

// click handles left-button presses against the render-time hit rects:
// a single click selects a card, a double click on the selected card (or
// the Build button) starts the build; the Stop tab toggles checklist
// rows, select-all, and the Stop button.
func (m *Model) click(msg tea.MouseMsg) tea.Cmd {
	n := m.dbl.Press(msg)
	if m.opKind == "" { // a running op's toast ("building XYZ…") stays
		m.toast = ""
	}
	if m.sub == TabBuild {
		for i, r := range m.cardRects {
			if r.Hit(msg.X, msg.Y) {
				if n == 2 && m.buildSel == i {
					return m.startBuild(m.options()[i])
				}
				m.buildSel = i
				return nil
			}
		}
		if m.buildBtn.Hit(msg.X, msg.Y) {
			opts := m.options()
			if len(opts) == 0 {
				return nil
			}
			return m.startBuild(opts[m.buildSel])
		}
		return nil
	}
	if m.selAll.Hit(msg.X, msg.Y) {
		m.toggleAll()
		return nil
	}
	for i, r := range m.rowRects {
		if r.Hit(msg.X, msg.Y) {
			idx := i
			if i < len(m.stopIdx) {
				idx = m.stopIdx[i]
			}
			name := m.running[idx]
			m.selection[name] = !m.selection[name]
			return nil
		}
	}
	if m.stopBtn.Hit(msg.X, msg.Y) {
		return m.startStop()
	}
	return nil
}

func (m *Model) toggleAll() {
	if m.allSelected() {
		m.selection = map[string]bool{}
		return
	}
	for _, name := range m.running {
		m.selection[name] = true
	}
}

func (m *Model) allSelected() bool {
	if len(m.running) == 0 {
		return false
	}
	for _, name := range m.running {
		if !m.selection[name] {
			return false
		}
	}
	return true
}

func (m *Model) selected() []string {
	var out []string
	for _, name := range m.running {
		if m.selection[name] {
			out = append(out, name)
		}
	}
	return out
}

// toastLine renders the reserved toast row. While an op runs, the
// spinner rides the same row behind "building XYZ…" (user feedback: the
// spinner must not sit low on the log-pane head, and the yellow text
// stays visible until the job is done — clicks never clear it then).
func (m *Model) toastLine() string {
	if m.toast == "" {
		return ""
	}
	if m.opKind != "" {
		if frame := m.log.SpinFrame(); frame != "" {
			return frame + " " + m.toast
		}
	}
	return m.toast
}

// startBuild opens the confirmation popup (ADR-0021) for a launch when
// nothing runs (idle fires only); the popup confirm calls launchBuild.
// A running op — this view's or a queued one — skips the popup and
// toasts, keeping the existing busy priority.
func (m *Model) startBuild(edition string) tea.Cmd {
	if m.opKind != "" || (m.Queue != nil && m.Queue.Busy()) {
		return m.launchBuild(edition)
	}
	m.confirm.Open("Build the "+edition+" image?", "Build", "Cancel",
		[]string{"edition: " + edition}, false, func() tea.Cmd { return m.launchBuild(edition) })
	return nil
}

// launchBuild runs the build op through the Launcher seam (one op at a
// time; no config writes).
func (m *Model) launchBuild(edition string) tea.Cmd {
	if m.opKind != "" {
		m.toast = "operation already in progress"
		return nil
	}
	m.opKind = "build"
	m.log = shell.NewLogPane().HideHead() // the spinner rides the toast row
	m.toast = "building " + edition + "…"
	run, script := m.Run, m.BuildScript
	return m.launch("building "+edition, "cv-build:"+edition, func(emit func(string)) error {
		out, err := run.Output("bash", script, edition)
		for _, l := range cmdrun.Lines(out) {
			emit(l)
		}
		if err != nil {
			return fmt.Errorf("build failed: %s", strings.TrimSpace(out))
		}
		return nil
	})
}

// startStop launches the stop op for the selected containers.
func (m *Model) startStop() tea.Cmd {
	if m.opKind != "" {
		m.toast = "operation already in progress"
		return nil
	}
	names := m.selected()
	if len(names) == 0 {
		m.toast = "select containers to stop"
		return nil
	}
	m.opKind = "stop"
	m.log = shell.NewLogPane().HideHead() // spinner rides the toast row
	m.toast = fmt.Sprintf("stopping %d container(s)…", len(names))
	run, reg := m.Run, m.Registry
	return m.launch(fmt.Sprintf("stopping %d container(s)", len(names)), "cv-stop", func(emit func(string)) error {
		for _, name := range names {
			out, err := run.Output("podman", "stop", name)
			for _, l := range cmdrun.Lines(out) {
				emit(l)
			}
			if err != nil {
				return fmt.Errorf("podman stop %s failed: %s", name, strings.TrimSpace(out))
			}
		}
		// container_status="stopped" only after verified gone (data map
		// §3): reconcile against a fresh podman ps before any write.
		remaining := discovery.RunningContainers(run)
		for _, name := range names {
			if slices.Contains(remaining, name) {
				return fmt.Errorf("%s is still running", name)
			}
		}
		if reg == nil {
			return nil
		}
		for _, name := range names {
			id := strings.TrimPrefix(name, registry.ContainerName(""))
			p, err := reg.ByID(id)
			if err != nil {
				continue // not a registry project
			}
			if err := reg.UpdateStatus(p.Path, "stopped"); err != nil {
				return err
			}
		}
		return nil
	})
}

// launch turns the op into a tea.Cmd: through the shared background-task
// manager (#82) when wired (queued per key, lines stream), else through
// the injected Launcher func ((SyncLauncher fallback).
func (m *Model) launch(title, key string, op Op) tea.Cmd {
	m.opID++
	m.opKey = key
	var cmd tea.Cmd
	if m.Queue != nil {
		cmd = m.Queue.Launch(taskmgr.Op{
			ID:    m.opID,
			Key:   key,
			Title: title,
			Run:   op,
		})
	} else {
		launch := m.Launch
		if launch == nil {
			launch = SyncLauncher
		}
		cmd = launch(op)
	}
	m.log = m.log.Run(true)
	if spin := m.log.Spin(); spin != nil {
		return tea.Batch(cmd, spin)
	}
	return cmd
}

// opDone finalizes a finished op: stream the output into the log pane,
// stop the spinner, toast the result, and refresh the running list
// after a stop.
func (m *Model) opDone(msg OpDoneMsg) tea.Cmd {
	if m.opKind == "" {
		return nil
	}
	// Streaming transport already fed the pane; batch launchers flush here.
	if len(m.log.Lines()) == 0 {
		for _, l := range msg.Lines {
			m.log, _ = m.log.Update(shell.LineMsg(l))
		}
	}
	m.log = m.log.Run(false)
	kind := m.opKind
	m.opKind = ""
	m.toast = ""
	switch kind {
	case "build":
		if msg.Err != nil {
			m.toast = msg.Err.Error()
		} else {
			m.toast = "Build successful"
		}
	case "stop":
		if msg.Err != nil {
			m.toast = msg.Err.Error()
		} else {
			m.toast = "Containers stopped"
		}
		return m.refreshRunning()
	}
	return nil
}

// Body implements shell.CenterView. Edition discovery runs fresh on
// every render (data map §3); hit rects are stored in screen space as
// they are laid out.
func (m *Model) Body(width, height int) string {
	m.refreshEditions()
	m.cardRects = nil
	m.rowRects = nil
	m.stopIdx = nil
	m.selAll = shell.Rect{}
	m.stopBtn = shell.Rect{}
	m.buildBtn = shell.Rect{}

	var lines []string
	if m.sub == TabBuild {
		lines = m.buildBody(width, height)
	} else {
		lines = m.stopBody(width, height)
	}
	body := shell.PadLeft(padX, strings.Join(lines, "\n"))
	if m.confirm.IsOpen() { // the popup overlays the cards (ADR-0021)
		body = m.confirm.Overlay(width, height, shell.BodyTop)
	}
	return body
}

// buildBody renders the two-column edition cards, the reserved toast
// row, the Build button, and the embedded log pane below.
func (m *Model) buildBody(width, height int) []string {
	opts := m.options()
	var lines []string
	if m.discoveryErr != "" {
		lines = append(lines, shell.Toast(m.discoveryErr), "")
	} else {
		cols := 2
		if padX+2*shell.CardStride > width {
			cols = 1
		}
		cards := make([]string, len(opts))
		for i, opt := range opts {
			busy := m.Queue != nil && m.Queue.BusyKey("cv-build:"+opt)
			cards[i] = editionCard(opt, busy, i == m.buildSel)
		}
		rows := int(math.Ceil(float64(len(cards)) / float64(cols)))
		m.cardRects = shell.CardRects(padX, bodyTop, len(opts), cols, 0)
		for r := 0; r < rows; r++ {
			end := min((r+1)*cols, len(cards))
			lines = append(lines, lipgloss.JoinHorizontal(lipgloss.Top, cards[r*cols:end]...))
		}
		lines = append(lines, shell.Toast(m.toastLine()))
	}
	btn := shell.Button("Build", false)
	m.buildBtn = shell.Rect{X: width - shell.Width(btn), Y: bodyTop + len(lines), W: shell.Width(btn), H: 1}
	lines = append(lines, shell.AlignRight(width-padX, btn))
	if rest := height - len(lines); rest > 1 {
		m.log = m.log.SetSize(width-2*padX, rest)
		lines = append(lines, m.log.View())
	}
	return lines
}

// editionCard renders one educational edition card (name + one-line
// description), accent-bordered when selected; busy marks a queued or
// running op for that edition's queue key (still-running indicator #82).
func editionCard(name string, busy, selected bool) string {
	st := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(shell.ColBorder).
		Padding(0, 1).
		Width(shell.CardWidth - 2).Height(shell.CardHeight - 2).MaxHeight(shell.CardHeight)
	if selected {
		st = st.BorderForeground(shell.ColAccent)
	}
	if busy {
		name += "  " + shell.StyleStatusRunning.Render("◌ running")
	}
	inner := shell.StyleFieldLabel.Render(name) + "\n" + shell.StyleMutedAlt.Render(description(name))
	return st.Render(inner)
}

// stopBody renders the running-container checklist card (select-all,
// rows, Stop button, reserved toast row) and the log pane below. When
// the checklist exceeds the card height it scrolls: head rows are
// dropped and the recorded rects shift up by the scroll offset.
func (m *Model) stopBody(width, height int) []string {
	avail := height - 2 // card borders
	visible := max(1, avail-checklistFixedRows)
	m.stopFollow.Pull(&m.stopScroll, m.stopSel, m.stopSel, len(m.running), visible)

	var inner []string
	lineY := bodyTop + 1 // first row inside the card border
	if len(m.running) == 0 {
		inner = append(inner, shell.StyleMutedAlt.Render("no containers running"))
		lineY++
	} else {
		box := "☐"
		if m.allSelected() {
			box = "☒"
		}
		inner = append(inner, box+" select all")
		m.selAll = shell.Rect{X: padX + 2, Y: lineY, W: 2 + shell.Width("select all"), H: 1}
		lineY++
		for i := m.stopScroll; i < len(m.running) && i < m.stopScroll+visible; i++ {
			name := m.running[i]
			box := "☐"
			if m.selection[name] {
				box = "☒"
			}
			row := box + " " + name + "  " + shell.StyleStatusRunning.Render(shell.SymOn+" running")
			if i == m.stopSel {
				row = shell.StyleSelectedRow.Render(row)
			}
			inner = append(inner, row)
			m.rowRects = append(m.rowRects, shell.Rect{
				X: padX + 2, Y: lineY, W: 2 + shell.Width(name) + 2 + shell.Width(shell.SymOn+" running"), H: 1,
			})
			m.stopIdx = append(m.stopIdx, i)
			lineY++
		}
		inner = append(inner, "")
		lineY++
	}
	btnBlock := shell.ButtonLines("Stop", false)
	for _, l := range btnBlock {
		inner = append(inner, l)
		if strings.Contains(l, "Stop") {
			m.stopBtn = shell.Rect{X: padX + 2, Y: lineY, W: shell.Width(l) - 2, H: 1} // minus borders
		}
		lineY++
	}
	inner = append(inner, shell.Toast(m.toastLine()))
	card := shell.Card(inner)
	lines := []string{card}
	if rest := height - lipgloss.Height(card); rest > 1 {
		m.log = m.log.SetSize(width-2*padX, rest)
		lines = append(lines, m.log.View())
	}
	return lines
}
