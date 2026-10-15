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
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Sub-menu tab items (approved layout record #72).
var tabs = []string{"Import project", "Export Config", "Restore Config", "Uninstall"}

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

	sub   int // 0 import, 1 export, 2 restore, 3 uninstall
	toast string

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
	return v
}

// SubTabs implements shell.CenterView.
func (v *View) SubTabs() ([]string, int) { return tabs, v.sub }

// TextFocused implements shell.CenterView: true while a text input holds
// focus so the shell suppresses 1–4/q/esc and letters type.
func (v *View) TextFocused() bool { return v.focusedField() != nil }

// Rects returns the interactive hit rects of the current pane as laid
// out by the last Body render (regression-tested geometry).
func (v *View) Rects() []shell.Rect { return v.rects }

// ConfirmOpen implements shell.ConfirmView (ADR-0021): the popup is up.
func (v *View) ConfirmOpen() bool { return v.confirm.IsOpen() }

// Update implements shell.CenterView.
func (v *View) Update(msg tea.Msg) tea.Cmd {
	v.toast = ""
	switch msg := msg.(type) {
	case shell.SubTabMsg:
		if msg.Index >= 0 && msg.Index < len(tabs) && msg.Index != v.sub {
			v.sub = msg.Index
			v.scroll = 0
			v.follow.Arm()
			v.enterPane()
		}
	case taskmgr.DoneMsg: // continuation of a queued op (#82)
		return v.taskDone(msg)
	case tea.KeyMsg:
		if v.confirm.IsOpen() { // the popup consumes keys (ADR-0021)
			return v.confirm.Key(msg)
		}
		return v.key(msg)
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if v.confirm.IsOpen() { // popup buttons & outside-cancel first
				if cmd, ok := v.confirm.Click(msg.X, msg.Y); ok {
					return cmd
				}
			}
			v.click(msg.X, msg.Y)
			cmd := v.pending
			v.pending = nil
			return cmd
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
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
func (v *View) taskDone(msg taskmgr.DoneMsg) tea.Cmd {
	after, ours := v.queuePending[msg.ID]
	if !ours || (msg.Key != "" && msg.Key != "settings") {
		return nil
	}
	delete(v.queuePending, msg.ID)
	if msg.Err != nil {
		v.toast = msg.Err.Error()
	}
	return after(msg.Err)
}

// enterPane refreshes the pane's live data when its tab is entered:
// the backup list (restore) and running containers (uninstall warning,
// fresh per entry, never cached across renders).
func (v *View) enterPane() {
	switch v.sub {
	case 2:
		v.resBackups = listBackups(v.home)
		if v.resSel >= len(v.resBackups) {
			v.resSel = 0
		}
	case 3:
		v.unSeen = true
		v.unRunning = discovery.RunningContainers(v.run)
	}
}

// Body implements shell.CenterView: the pane card pinned top-left in the
// body region (screen row 3, left padding 2).
func (v *View) Body(width, height int) string {
	var rows []string
	var hits []hitRow
	var focusRow int
	switch v.sub {
	case 0:
		rows, hits, focusRow = v.importPane()
	case 1:
		rows, hits, focusRow = v.exportPane()
	case 2:
		rows, hits, focusRow = v.restorePane()
	default:
		rows, hits, focusRow = v.uninstallPane(width)
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
	case 0:
		if v.impStage == 0 && v.impFocus == 0 {
			return &field{&v.impPath, &v.impCur}
		}
		if v.impStage == 1 && v.impFocus == 0 {
			return &field{&v.impName, &v.impNameCur}
		}
	case 1:
		if v.expStage == 1 && v.expFocus == 0 {
			return &field{&v.expDest, &v.expCur}
		}
	case 3:
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
	case 0:
		switch v.impStage {
		case 0:
			return 1 + len(v.chooserRows()) + 1 // path field, chooser rows, import button
		case 1:
			return 3 // name field, register, back
		default:
			return 2 // new-project wizard, back
		}
	case 1:
		if v.expStage == 0 {
			return 1
		}
		return 3 // dest field, save, cancel
	case 2:
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
	case 0:
		v.impFocus = (v.impFocus + d + v.focusCount()) % v.focusCount()
	case 1:
		if v.expStage != 0 {
			v.expFocus = (v.expFocus + d + v.focusCount()) % v.focusCount()
		}
	case 2:
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
		if v.sub == 3 && v.unFocus < 3 {
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
	case 0:
		return v.importActivate()
	case 1:
		return v.exportActivate()
	case 2:
		return v.restoreActivate()
	default:
		return v.uninstallActivate()
	}
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
