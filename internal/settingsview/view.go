// Package settingsview implements the Settings center view (ticket #80,
// spec #84): four cards under sub-menu tabs — Import project, Export
// Config, Restore Config, Uninstall — in the shared Variant C card frame
// with uniform buttons, full keyboard parity, hit rects and toast
// feedback. Long operations (uninstall.sh invocation, export write) go
// through the single Ops seam so #82 can swap in the background task
// manager; the view itself starts no goroutines.
package settingsview

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/projectsettings"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Sub-menu tab items; "Edit Project settings" first (feedback: it is the
// starting tab), the core panes follow in their order. "Edit Project
// settings" delegates its pane surface to the projectsettings view
// (ticket #108).
var tabs = []string{"Edit Project settings", "Import project", "Export Config", "Restore Config", "Uninstall"}

// Pane indexes into tabs: which pane each sub-menu position routes to.
const (
	subEdit      = 0 // full delegation to projectsettings (#108)
	subImport    = 1
	subExport    = 2
	subRestore   = 3
	subUninstall = 4
)

// View is the Settings center view. Home is $HOME; the config layout,
// registry, backups and the default install root hang off it. Tasks is
// the shared background-task manager (#82): when wired, the uninstall /
// export / restore ops run as queued tasks; without it they stay
// synchronous through the Ops seam (legacy parity).
type View struct {
	home        string
	installRoot string
	scriptPath  string // uninstall.sh; default <installRoot>/scripts/uninstall.sh
	run         cmdrun.Runner
	now         func() time.Time
	ops         Ops
	Tasks       *taskmgr.Manager

	opSeq        int // view-assigned task handles
	queuePending map[int]func(err error) tea.Cmd

	sub   int // pane index (subImport..subUninstall, subEdit delegated)
	toast string

	ps *projectsettings.View // Edit Project settings screen (#108)

	// Import pane: 0 picker, 1 summary, 2 informational prompt.
	impStage     int
	impBrokenCfg bool
	impPath      string
	impCur       int
	impRoot      string // canonical candidate root (summary/register)
	impDir       string
	impDirs      []string
	impSel       int
	impFocus     int
	impName      string
	impNameCur   int
	impCfg       sandboxcfg.Config

	// Export pane: 0 export button, 1 save-as wizard.
	expStage int
	expDest  string
	expSel   int
	expCur   int
	expFocus int

	// Restore pane.
	resBackups []string
	resSel     int
	resFocus   int

	// Uninstall pane.
	unSeen    bool
	unRunning []string
	unOptCfg  bool // remove config files (backup created first)
	unOptSyml bool // remove symlinks (script default: remove)
	unOptScut bool // remove desktop shortcuts
	unFocus   int  // 0..2 option rows, 3 confirm field, 4 button
	unConfirm string
	unCur     int

	scroll       int
	follow       shell.Follow // the focused row pulls the scroll; wheel turns it off
	focusChanged bool
	rects        []shell.Rect
	acts         []func() tea.Cmd
	pending      tea.Cmd
	confirm      shell.Confirm // shared confirmation popup (ADR-0021)
}

// New returns the Settings view for home, wiring the default Ops (real
// command runner, uninstall.sh under <home>/.oc-sandbox/scripts) and the
// real clock.
func New(home string) *View {
	installRoot := home + "/.oc-sandbox"
	v := &View{
		home:         home,
		installRoot:  installRoot,
		scriptPath:   installRoot + "/scripts/uninstall.sh",
		run:          cmdrun.ExecRunner{},
		now:          time.Now,
		unOptSyml:    true,
		queuePending: map[int]func(err error) tea.Cmd{},
		follow:       shell.NewFollow(true, false),
	}
	v.ops = DefaultOps{view: v}
	v.ps = projectsettings.New(projectsettings.Deps{
		Home:            home,
		Runner:          v.run,
		ConfigureScript: installRoot + "/scripts/configure-project.sh",
		Ops:             psLauncher{v},
	})
	return v
}

// psLauncher routes the Edit Project settings view's ops through the
// shared task manager when wired, else runs them as one batch cmd.
type psLauncher struct{ v *View }

func (l psLauncher) Launch(op taskmgr.Op) tea.Cmd {
	if l.v.Tasks != nil {
		return l.v.Tasks.Launch(op)
	}
	return func() tea.Msg {
		var lines []string
		err := op.Run(func(line string) { lines = append(lines, line) })
		return taskmgr.DoneMsg{ID: op.ID, Lines: lines, Err: err}
	}
}

// WireProjectSettingsDeps wires the Edit Project settings view's write
// path (app composition): runner, configure-project.sh and the shared
// task manager as the Ops launcher.
func (v *View) WireProjectSettingsDeps(d projectsettings.Deps) { v.ps.Deps = d }

// SubTabs implements shell.CenterView.
func (v *View) SubTabs() ([]string, int) { return tabs, v.sub }

// TextFocused implements shell.CenterView: true while a text input holds
// focus so the shell suppresses 1–4/q/esc and letters type.
func (v *View) TextFocused() bool {
	if v.sub == subEdit {
		return v.ps.TextFocused()
	}
	return v.focusedField() != nil
}

// Rects returns the interactive hit rects of the current pane as laid
// out by the last Body render (regression-tested geometry); the Edit
// Project settings tab delegates to its view.
func (v *View) Rects() []shell.Rect {
	if v.sub == subEdit {
		return v.ps.Rects()
	}
	return v.rects
}

// ConfirmOpen implements shell.ConfirmView (ADR-0021): the popup is up —
// the pane's own or the delegated Edit Project settings view's.
func (v *View) ConfirmOpen() bool {
	return v.confirm.IsOpen() || (v.sub == subEdit && v.ps.ConfirmOpen())
}

// Update implements shell.CenterView.
func (v *View) Update(msg tea.Msg) tea.Cmd {
	switch msg.(type) {
	case taskmgr.DoneMsg, taskmgr.StatusMsg, taskmgr.LineEvent:
		// task-manager events must not wipe a toast a continuation just
		// set: a fast op's Running StatusMsg is broadcast AFTER its
		// DoneMsg (the op goroutine starts at Launch), so clearing here
		// would erase completion toasts before the next frame renders.
	default:
		v.toast = ""
	}
	switch msg := msg.(type) {
	case shell.SubTabMsg:
		if msg.Index >= 0 && msg.Index < len(tabs) && msg.Index != v.sub {
			v.sub = msg.Index
			v.scroll = 0
			v.follow.Arm()
			v.enterPane()
		}
	case taskmgr.DoneMsg: // continuation of a queued op (#82)
		if cmd, ours := v.taskDone(msg); ours {
			return cmd
		}
		return v.ps.Update(msg) // foreign here may be the delegated view's op
	case tea.KeyMsg:
		if v.confirm.IsOpen() { // the popup consumes keys (ADR-0021)
			return v.confirm.Key(msg)
		}
		if v.sub == subEdit { // Edit Project settings: full delegation (#108)
			return v.ps.Update(msg)
		}
		return v.key(msg)
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if v.confirm.IsOpen() { // popup buttons & outside-cancel first
				if cmd, ok := v.confirm.Click(msg.X, msg.Y); ok {
					return cmd
				}
			}
			if v.sub == subEdit {
				return v.ps.Update(msg)
			}
			v.click(msg.X, msg.Y)
			cmd := v.pending
			v.pending = nil
			return cmd
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			if v.sub == subEdit {
				return v.ps.Update(msg)
			}
			dir := 1
			if msg.Button == tea.MouseButtonWheelUp {
				dir = -1
			}
			v.follow.Wheel(&v.scroll, dir)
		}
	}
	return nil
}

// taskDone runs a queued op's continuation; unknown ids are foreign
// tasks (other views') and ignored. The Key guard disambiguates another
// view's op that reuses this view's numeric id (every view numbers from
// its own counter).
func (v *View) taskDone(msg taskmgr.DoneMsg) (tea.Cmd, bool) {
	after, ours := v.queuePending[msg.ID]
	if !ours || (msg.Key != "" && msg.Key != "settings") {
		return nil, false
	}
	delete(v.queuePending, msg.ID)
	if msg.Err != nil {
		v.toast = msg.Err.Error()
	}
	return after(msg.Err), true
}

// enterPane refreshes the pane's live data when its tab is entered:
// the backup list (restore) and running containers (uninstall warning,
// fresh per entry, never cached across renders).
func (v *View) enterPane() {
	switch v.sub {
	case subRestore:
		v.resBackups = listBackups(v.home)
		if v.resSel >= len(v.resBackups) {
			v.resSel = 0
		}
	case subUninstall:
		v.unSeen = true
		v.unRunning = discovery.RunningContainers(v.run)
	case subEdit:
		v.ps.Reload() // fresh registry: cards reflect the current fixtures
	}
}

// Body implements shell.CenterView: the pane card pinned top-left in the
// body region (screen row 3, left padding 2).
func (v *View) Body(width, height int) string {
	var rows []string
	var hits []hitRow
	var focusRow int
	switch v.sub {
	case subImport:
		rows, hits, focusRow = v.importPane()
	case subExport:
		rows, hits, focusRow = v.exportPane()
	case subRestore:
		rows, hits, focusRow = v.restorePane()
	case subUninstall:
		rows, hits, focusRow = v.uninstallPane(width)
	default:
		return v.ps.Body(width, height) // Edit Project settings (#108)
	}
	rows = append(rows, shell.Toast(v.toast))
	if v.focusChanged {
		v.focusChanged = false
		v.follow.Arm() // the focused row pulls the scroll again
	}

	// Pane scroll at narrow heights: the focused row pulls the scroll
	// only when follow mode is on (wheel scrolling turns it off); the
	// scroll is otherwise clamped to the row count.
	avail := height - 2 // card borders
	if avail < 1 {
		avail = 1
	}
	v.follow.Pull(&v.scroll, focusRow, focusRow, len(rows), avail)
	end := min(len(rows), v.scroll+avail)

	v.rects = v.rects[:0]
	v.acts = v.acts[:0]
	for _, h := range hits {
		if h.row < v.scroll || h.row >= end {
			continue
		}
		v.rects = append(v.rects, shell.Rect{X: h.x, Y: 4 + h.row - v.scroll, W: h.w, H: 1})
		v.acts = append(v.acts, h.act)
	}
	body := shell.PadLeft(2, shell.Card(rows[v.scroll:end]))
	if v.confirm.IsOpen() { // the popup overlays the pane card (ADR-0021)
		body = v.confirm.Overlay(width, height, shell.BodyTop)
	}
	return body
}

// hitRow is an interactive row of a pane: row indexes into the pane's
// rows, x/w its hit box inside the card, act runs on click and returns
// the cmd it produces (e.g. the New Project navigation request).
type hitRow struct {
	row  int
	x, w int
	act  func() tea.Cmd
}

// cardX is the screen column of card content column 0 (left padding 2 +
// border 1 + card padding 1).
const cardX = 4

// focusedField returns the currently focused text input, if any.
func (v *View) focusedField() *field {
	switch v.sub {
	case subImport:
		if v.impStage == 0 && v.impFocus == 0 {
			return &field{&v.impPath, &v.impCur}
		}
		if v.impStage == 1 && v.impFocus == 0 {
			return &field{&v.impName, &v.impNameCur}
		}
	case subExport:
		if v.expStage == 1 && v.expFocus == 0 {
			return &field{&v.expDest, &v.expCur}
		}
	case subUninstall:
		if v.unFocus == 3 {
			return &field{&v.unConfirm, &v.unCur}
		}
	}
	return nil
}

type field struct {
	val *string
	cur *int
}

// focusCount is the number of focusable items in the current pane.
func (v *View) focusCount() int {
	switch v.sub {
	case subImport:
		switch v.impStage {
		case 0:
			return 1 + len(v.chooserRows()) + 1 // path field, chooser rows, import button
		case 1:
			return 3 // name field, register, back
		default:
			return 2 // new-project wizard, back
		}
	case subExport:
		if v.expStage == 0 {
			return 1
		}
		return 3 // dest field, save, cancel
	case subRestore:
		return len(v.resBackups) + 1 // backup rows, restore button
	default:
		return 5 // 3 option rows, confirm field, button
	}
}

func (v *View) moveFocus(d int) {
	v.applyMoveFocus(d)
	v.focusChanged = true
}

// applyMoveFocus steps the current pane's focus cursor with wraparound.
func (v *View) applyMoveFocus(d int) {
	switch v.sub {
	case subImport:
		v.impFocus = (v.impFocus + d + v.focusCount()) % v.focusCount()
	case subExport:
		if v.expStage != 0 {
			v.expFocus = (v.expFocus + d + v.focusCount()) % v.focusCount()
		}
	case subRestore:
		v.resFocus = (v.resFocus + d + v.focusCount()) % v.focusCount()
	default:
		v.unFocus = (v.unFocus + d + v.focusCount()) % v.focusCount()
	}
}

func (v *View) key(msg tea.KeyMsg) tea.Cmd {
	if f := v.focusedField(); f != nil && len(msg.Runes) == 1 {
		insertRune(f, msg.Runes[0])
		return nil
	}
	switch msg.String() {
	case "tab", "down", "j":
		v.moveFocus(1)
	case "shift+tab", "up", "k":
		v.moveFocus(-1)
	case "left":
		if f := v.focusedField(); f != nil {
			if *f.cur > 0 {
				*f.cur--
			}
		} else {
			v.sub = (v.sub + len(tabs) - 1) % len(tabs)
			v.scroll = 0
			v.follow.Arm()
			v.enterPane()
		}
	case "right":
		if f := v.focusedField(); f != nil {
			if *f.cur < len([]rune(*f.val)) {
				*f.cur++
			}
		} else {
			v.sub = (v.sub + 1) % len(tabs)
			v.scroll = 0
			v.follow.Arm()
			v.enterPane()
		}
	case "enter":
		return v.activate()
	case " ":
		if v.sub == subUninstall && v.unFocus < 3 {
			v.toggleUninstallOpt(v.unFocus)
		}
	case "backspace":
		if f := v.focusedField(); f != nil {
			deleteRune(f)
		}
	}
	return nil
}

func (v *View) click(x, y int) {
	for i, r := range v.rects {
		if r.Hit(x, y) {
			v.pending = v.acts[i]()
			v.focusChanged = false
			return
		}
	}
}

// activate runs the focused item's action (keyboard parity for clicks).
func (v *View) activate() tea.Cmd {
	switch v.sub {
	case subImport:
		return v.importActivate()
	case subExport:
		return v.exportActivate()
	case subRestore:
		return v.restoreActivate()
	case subUninstall:
		return v.uninstallActivate()
	}
	return nil // case subEdit (Edit Project settings) is fully delegated
}

func insertRune(f *field, r rune) {
	rs := []rune(*f.val)
	if *f.cur > len(rs) {
		*f.cur = len(rs)
	}
	*f.val = string(rs[:*f.cur]) + string(r) + string(rs[*f.cur:])
	*f.cur++
}

func deleteRune(f *field) {
	rs := []rune(*f.val)
	if *f.cur == 0 || *f.cur > len(rs) {
		return
	}
	*f.val = string(rs[:*f.cur-1]) + string(rs[*f.cur:])
	*f.cur--
}

// registryView returns the project registry for the view's home.
func (v *View) registryView() *registry.Registry {
	return &registry.Registry{Home: v.home, Now: v.now}
}

func (v *View) configPaths() config.Paths { return config.Paths{Home: v.home} }

// Busy implements shell.BusyView: queued settings ops count as
// background work (guarded exit, #82).
func (v *View) Busy() bool { return v.Tasks != nil && v.Tasks.Busy() }
