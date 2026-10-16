// Package projectsettings implements the Settings center's "Edit Project
// settings" sub-menu tab (ticket #108, spec #106): a variant C frame with
// the registered projects as cards in the card grid idiom on the left —
// the open project preselected, each card carrying "vcs" and "AI
// Provider" buttons instead of the start-variant buttons (this screen
// never starts anything) — and a right pane that starts empty ("select a
// card, then vcs or AI Provider") and shows the selected project's
// placeholder on a button click. Card/button switches reset the pane to
// the empty state. Pure navigation: pane content lands in follow-up
// tickets. Full keyboard/mouse parity with the other Settings tabs:
// hit rects, wheel scrolling of the card grid, and toasts ride the same
// idioms as the Open Project card grid.
package projectsettings

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Tab item (Settings center's sub-menu row; the tab itself lives in the
// settings view, which delegates its pane surface to this view).
var tabs = []string{"Edit Project settings"}

const (
	cardInnerW = 26                   // content width → shell.CardWidth 30
	rowStride  = shell.CardHeight + 1 // card + gap
	hintText   = "select a card, then vcs or AI Provider"
)

// cardButtons are the per-card actions; no start-variant buttons — this
// screen never starts anything.
var cardButtons = []string{"vcs", "AI Provider"}

// paneKinds of the right pane: placeholder, VCS, AI provider.
const (
	paneNone = iota
	paneVCS
	paneAI
)

// View is the Edit Project settings screen.
type View struct {
	home            string
	run             cmdrun.Runner
	configureScript string
	ops             taskmgr.Launcher
	opSeq           int // view-assigned op handles
	saveID          int // the in-flight VCS save op (0 = none)
	projects        []registry.Project
	sel             int // selected card (0 = the open project on load)
	pane            int // paneNone / paneVCS / paneAI
	vcs             *vcsState
	confirm         shell.Confirm // VCS replace-credentials popup (ADR-0021)
	toast           string
	width           int
	gridScroll      int
	gridFollow      shell.Follow // wheel turns it off; selection pulls again

	cardRects   []shell.Rect // one hit rect per rendered card
	buttonRects []shell.Rect // vcs / AI Provider on the selected card
	paneRects   []shell.Rect // AI pane interactive rows (fields, Add, Save, confirm, entries)
	paneIdx     []int        // focus index per pane rect
	paneEnts    []int        // editable-entry list index per pane rect (-1 = not an entry)
	saveCmd     tea.Cmd      // cmd a pane click produced (save launch)
	pending     func()

	lastCmd tea.Cmd // cmd produced by the last key (enter-activated flows)

	// Focus: 0 = card grid, >=1 = pane item (pi* indices while the AI
	// pane is open). Tab cycles; a text field in focus suppresses the
	// shell's own keys.
	focus int

	// AI provider pane (#110): the add form fields, the pending
	// Keep/Replace confirm, the queued write steps and the run flag.
	// #111 adds the entry edit form (aiEdit) and whether an edit save is
	// queued (aiEditQueued); #112 adds the provider id whose removal op
	// is in flight (removing).
	Deps         Deps // production wiring (runner / configure-project.sh / Ops)
	aiName       field
	aiEndpoint   field
	aiToken      field
	aiEdit       *aiEditState
	aiEditQueued bool
	aiConfirm    *aiConfirmState
	aiSteps      [][]string
	aiRunning    bool
	aiErr        string
	aiRename     bool   // the running/failed queue is a rename (#111)
	removing     string // provider id whose removal op is in flight (#112)
	opID         int
}

// Deps wires the view's write paths: the command runner and the
// configure-project.sh location the VCS pane saves through, and the ops
// launcher (the single goroutine escape hatch; nil → batch default).
type Deps struct {
	Home            string
	Runner          cmdrun.Runner
	ConfigureScript string
	Ops             taskmgr.Launcher
}

// New returns the Edit Project settings view for home, preloaded from the
// project registry with the open project (last used) preselected.
func New(d Deps) *View {
	v := &View{
		home:            d.Home,
		run:             d.Runner,
		configureScript: d.ConfigureScript,
		ops:             d.Ops,
		Deps:            d,
		gridFollow:      shell.NewFollow(true, false),
	}
	v.Reload()
	return v
}

// TabName is the sub-menu tab this view belongs to.
func TabName() string { return tabs[0] }

// Reload refreshes the project cards from the registry (the Settings
// view calls it whenever the tab is entered).
func (v *View) Reload() {
	projs, err := (&registry.Registry{Home: v.home}).AllOrdered()
	if err != nil {
		v.projects = nil
	} else {
		v.projects = projs
	}
	v.sel = clamp(v.sel, 0, len(v.projects)-1)
	v.pane = paneNone
	v.resetPane()
	v.vcs = nil
	v.gridScroll = 0
}

// SubTabs implements shell.CenterView (standalone composition).
func (v *View) SubTabs() ([]string, int) { return tabs, 0 }

// TextFocused implements shell.CenterView: true while a VCS pane text
// field or an AI add-form field holds focus, so the shell suppresses
// 1–4/q/esc and letters type.
func (v *View) TextFocused() bool {
	if v.pane == paneVCS && v.vcs != nil {
		fs := v.vcs.fields()
		return v.vcs.focus >= 1 && v.vcs.focus <= len(fs)
	}
	return v.pane == paneAI && v.focus >= piName && v.focus <= piToken && v.aiConfirm == nil ||
		v.pane == paneAI && v.focus >= piEditName && v.focus <= piEditToken && v.aiConfirm == nil
}

// ConfirmOpen implements shell.ConfirmView (ADR-0021): the VCS
// replace-credentials popup is up.
func (v *View) ConfirmOpen() bool { return v.confirm.IsOpen() }

// Rects returns the interactive hit rects of the selected card's buttons
// as laid out by the last Body render (regression-tested geometry); the
// open AI pane appends its field/button/confirm rects.
func (v *View) Rects() []shell.Rect {
	out := make([]shell.Rect, 0, len(v.buttonRects)+len(v.paneRects))
	out = append(out, v.buttonRects...)
	out = append(out, v.paneRects...)
	return out
}

// Update implements shell.CenterView.
func (v *View) Update(msg tea.Msg) tea.Cmd {
	v.toast = ""
	v.lastCmd = nil
	switch msg := msg.(type) {
	case taskmgr.DoneMsg:
		// Route by op id: the VCS save's own id first, else the AI
		// pane's running queue.
		if msg.ID == v.saveID && v.vcs != nil {
			v.vcsDone(msg)
			return nil
		}
		if v.aiRunning {
			return v.aiDone(msg)
		}
	case tea.KeyMsg:
		if v.confirm.IsOpen() { // the popup consumes keys (ADR-0021)
			was := true
			v.confirm.Key(msg)
			return v.afterConfirm(was)
		}
		if v.vcsKey(msg) {
			return nil
		}
		v.key(msg)
		return v.lastCmd
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if v.confirm.IsOpen() { // popup buttons & outside-cancel first
				if cmd, ok := v.confirm.Click(msg.X, msg.Y); ok {
					if ac := v.afterConfirm(true); ac != nil {
						cmd = tea.Batch(cmd, ac)
					}
					return cmd
				}
				return nil
			}
			v.click(msg.X, msg.Y)
			cmd := v.saveCmd
			v.saveCmd = nil
			if cmd == nil {
				cmd = v.lastCmd
			}
			return cmd
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			dir := 1
			if msg.Button == tea.MouseButtonWheelUp {
				dir = -1
			}
			v.gridFollow.Wheel(&v.gridScroll, dir)
		}
	}
	return nil
}

// afterConfirm runs the post-popup continuation: Replace already returned
// its apply cmd; every other dismissal (Keep existing / esc / outside)
// counts as "keep existing" — the rest of the staged save applies without
// the credential step.
func (v *View) afterConfirm(was bool) tea.Cmd {
	if v.confirm.IsOpen() || !was || v.vcs == nil || v.vcs.replaced {
		return nil
	}
	v.vcs.skipCreds = true
	return v.applySave()
}

// Toast sets feedback for the next render (parity with the other
// Settings tabs; follow-up panes write here).
func (v *View) Toast(text string) { v.toast = text }

func (v *View) key(msg tea.KeyMsg) {
	switch msg.String() {
	case "tab":
		v.tabFocus(1)
		return
	case "shift+tab":
		v.tabFocus(-1)
		return
	case "esc":
		if v.aiConfirm != nil {
			v.aiConfirm = nil
			return
		}
		if v.aiEdit != nil {
			v.aiEdit = nil
			v.focus = 0
			return
		}
		if v.focus != 0 {
			v.focus = 0
		}
		return
	}
	if v.pane == paneAI && v.focus != 0 {
		v.paneKey(msg)
		return
	}
	cpr := v.cardsPerRow()
	switch msg.String() {
	case "up", "k":
		v.moveSel(-cpr)
	case "down", "j":
		v.moveSel(cpr)
	case "left", "h":
		v.moveSel(-1)
	case "right", "l":
		v.moveSel(1)
	}
}

// tabFocus cycles the focus between the card grid and the open AI
// pane's items (grid → name → endpoint → token → Add → confirm rows →
// grid); with the confirm open only the confirm rows cycle.
func (v *View) tabFocus(dir int) {
	if v.pane != paneAI {
		return
	}
	var lo, hi int
	if v.aiConfirm != nil {
		lo, hi = piKeep, piReplace
	} else if v.aiEdit != nil {
		lo, hi = piEditName, piEditSave
	} else {
		lo, hi = piName, piAdd
		// #112: the shown entries' Remove buttons close the cycle.
		if n := v.entryCount(); n > 0 {
			hi = piRemove + n - 1
		}
		if v.focus > hi {
			v.focus = piAdd // stale confirm/Remove indices snap back
		}
	}
	if v.focus < lo || v.focus > hi {
		if dir > 0 {
			v.focus = lo
		}
		return
	}
	n := v.focus + dir
	if n < lo {
		v.focus = 0 // back to the grid
		return
	}
	if n > hi {
		if v.aiConfirm != nil {
			return // the confirm traps the focus while it is up
		}
		v.focus = 0
		return
	}
	v.focus = n
}

// paneKey handles keys while an AI pane item holds focus: typing edits
// the focused field; enter activates the Add button or a confirm row.
func (v *View) paneKey(msg tea.KeyMsg) {
	switch msg.String() {
	case "enter":
		switch v.focus {
		case piAdd:
			v.lastCmd = v.addProvider()
		case piEditSave:
			v.lastCmd = v.saveProviderEdit()
		case piKeep:
			v.lastCmd = v.resolveAIConfirm(false)
		case piReplace:
			v.lastCmd = v.resolveAIConfirm(true)
		default:
			v.lastCmd = v.removeAt(v.focus) // #112: Remove button focus indices
		}
		return
	case "backspace":
		if f := v.focusedField(); f != nil {
			f.backspace()
		}
		return
	case "left":
		if f := v.focusedField(); f != nil {
			f.cur--
		}
		return
	case "right":
		if f := v.focusedField(); f != nil {
			f.cur++
		}
		return
	}
	rs := msg.Runes
	if f := v.focusedField(); f != nil && len(rs) > 0 {
		f.insert(string(rs))
	}
}

// focusedField returns the field under the focus cursor (nil on the
// Add button / confirm rows).
func (v *View) focusedField() *field {
	switch v.focus {
	case piName:
		return &v.aiName
	case piEndpoint:
		return &v.aiEndpoint
	case piToken:
		return &v.aiToken
	case piEditName:
		return &v.aiEdit.name
	case piEditEndpoint:
		return &v.aiEdit.endpoint
	case piEditToken:
		return &v.aiEdit.token
	}
	return nil
}

// moveSel moves the card selection; a card switch resets the right pane
// to the empty hint (the pane always belongs to the selected project).
func (v *View) moveSel(d int) {
	if len(v.projects) == 0 || d == 0 {
		return
	}
	n := v.sel + d
	if n < 0 || n >= len(v.projects) {
		return
	}
	if n != v.sel {
		v.sel = n
		v.pane = paneNone
		v.resetPane()
		v.vcs = nil
	}
}

func (v *View) selectCard(i int) {
	if i != v.sel {
		v.sel = i
		v.pane = paneNone // card switch resets the pane
		v.resetPane()
		v.vcs = nil
	}
}

func (v *View) click(x, y int) {
	// VCS pane items (tracker tokens, field values, save) first: they
	// never overlap the card grid.
	if p := v.vcs; p != nil && v.pane == paneVCS {
		for i, r := range p.trackerRects {
			if r.Hit(x, y) {
				p.setTracker(trackers[i])
				return
			}
		}
		fs := p.fields()
		for i, r := range p.fieldRects {
			if r.Hit(x, y) {
				p.focus = i + 1
				return
			}
		}
		if p.saveRect.Hit(x, y) {
			p.focus = len(fs) + 1
			v.vcsSave()
			return
		}
	}
	// The open AI pane's interactive rows take precedence; entry rows
	// open the entry edit form, everything else routes by focus index.
	if v.pane == paneAI {
		for i, r := range v.paneRects {
			if r.Hit(x, y) {
				idx, ent := v.paneIdx[i], v.paneEnts[i]
				if ent >= 0 {
					entries := readEntries(v.projects[v.sel].Path)
					if ent < len(entries) {
						v.openEntryEdit(entries[ent])
					}
					return
				}
				if idx < 0 { // orphan entries are read-only: recorded, no-op
					return
				}
				v.focus = idx
				v.paneClick(idx)
				return
			}
		}
	}
	for i, r := range v.cardRects {
		if !r.Hit(x, y) {
			continue
		}
		if i != v.sel {
			v.selectCard(i)
			return
		}
		// selected card: a button click selects the card and opens its pane
		for j, br := range v.buttonRects {
			if br.Hit(x, y) {
				v.pending = nil
				if j == 0 {
					if v.sel >= 0 && v.sel < len(v.projects) {
						v.openVCS(v.projects[v.sel])
					} else {
						v.pane = paneVCS
					}
				} else {
					v.pane = paneAI
					v.vcs = nil
					v.resetPane()
				}
				return
			}
		}
		return
	}
	if v.pending != nil {
		v.pending()
		v.pending = nil
	}
}

// --- layout ---

func (v *View) cardsPerRow() int {
	return max(1, (leftWidth(v.width)-2)/shell.CardStride)
}

// paneWidth / leftWidth size the two panes like the Open Project screen:
// right pane clamped to 44..48 columns (the empty-pane hint reads on one
// line), the rest goes to the card grid.
func paneWidth(width int) int { return clamp(width/2, 44, 48) }
func leftWidth(width int) int { return max(shell.CardWidth, width-paneWidth(width)-1) }

func clamp(v, lo, hi int) int {
	if hi < lo {
		hi = lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func truncate(s string, w int) string {
	rs := []rune(s)
	if len(rs) <= w {
		return s
	}
	return string(rs[:w])
}

// Body implements shell.CenterView: the card grid on the left, the pane
// card (variant C frame) on the right.
func (v *View) Body(width, height int) string {
	v.width = width
	leftW := leftWidth(width)
	left := v.leftPane(leftW, height)
	right := v.paneBody(paneWidth(width), height)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	if v.confirm.IsOpen() { // the popup overlays the body (ADR-0021)
		body = v.confirm.Overlay(width, height, shell.BodyTop)
	}
	return body
}

// leftPane renders the wrapping card grid (or the empty-registry hint)
// with the pinned toast row last; card and button hit rects are recorded
// here.
func (v *View) leftPane(w, h int) string {
	v.cardRects = v.cardRects[:0]
	v.buttonRects = v.buttonRects[:0]
	var lines []string
	if len(v.projects) == 0 {
		card := shell.Card([]string{
			padTo(cardInnerW, "No projects registered"),
			padTo(cardInnerW, "Create one first:"),
			padTo(cardInnerW, "menu 2 — New Project"),
		})
		lines = strings.Split(card, "\n")
	} else {
		cpr := v.cardsPerRow()
		totalRows := (len(v.projects) + cpr - 1) / cpr
		visRows := max(1, (h-1)/rowStride)
		selRow := v.sel / cpr
		// the selected row pulls the scroll only on a selection change
		v.gridFollow.Pull(&v.gridScroll, v.sel, selRow, totalRows, visRows)
		idxs := []int{}
		for r := v.gridScroll; r < totalRows && r-v.gridScroll < visRows; r++ {
			for c := 0; c < cpr; c++ {
				idx := r*cpr + c
				if idx >= len(v.projects) {
					break
				}
				idxs = append(idxs, idx)
			}
		}
		gridRects := shell.CardRects(1, shell.BodyTop, len(idxs), cpr, 1)
		var grid []string
		for k, idx := range idxs {
			rows := v.cardRows(idx)
			frame := shell.Card // selected card keeps the accent border
			if idx != v.sel {
				frame = shell.CardMuted // unselected: muted frame
			}
			card := strings.Split(frame(rows), "\n")
			c, r := k%cpr, k/cpr
			baseY := r * rowStride
			grid = pasteLines(grid, 1+c*shell.CardStride, baseY, card)
			v.cardRects = append(v.cardRects, gridRects[k])
			if idx == v.sel {
				v.recordButtons(gridRects[k])
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
	lines = append(lines, shell.Toast(v.toast))
	return padLines(lines, 0, w, h)
}

// cardRows builds one project card: name + path rows, and on the selected
// card the divider plus the vcs / AI Provider button row.
func (v *View) cardRows(idx int) []string {
	p := v.projects[idx]
	rows := []string{
		padTo(cardInnerW, truncate(p.Name, cardInnerW)),
		padTo(cardInnerW, truncate(p.Path, cardInnerW)),
	}
	if idx == v.sel {
		for i, r := range rows {
			rows[i] = shell.StyleSelectedRow.Render(padTo(cardInnerW, r))
		}
		rows = append(rows,
			shell.StyleMutedAlt.Render(padTo(cardInnerW, divider(24))),
			padTo(cardInnerW, v.buttonRow()))
	} else {
		rows = append(rows,
			shell.StyleMutedAlt.Render(padTo(cardInnerW, divider(24))),
			padTo(cardInnerW, mutedButtonRow()))
	}
	return rows
}

// buttonRow renders the selected card's action buttons with 2-space
// gaps: vcs  AI Provider. Label order and gaps mirror recordButtons.
func (v *View) buttonRow() string {
	styled := make([]string, len(cardButtons))
	for i, lbl := range cardButtons {
		styled[i] = lipgloss.NewStyle().Foreground(shell.ColCardFg).Render(lbl)
	}
	return joinWide(styled)
}

// mutedButtonRow renders an unselected card's buttons (muted: a click
// merely selects the card).
func mutedButtonRow() string {
	styled := make([]string, len(cardButtons))
	for i, lbl := range cardButtons {
		styled[i] = shell.StyleMutedAlt.Render(lbl)
	}
	return joinWide(styled)
}

func joinWide(parts []string) string {
	pad := ""
	for i := range parts {
		parts[i] = pad + parts[i]
		pad = "  "
	}
	return strings.Join(parts, "")
}

// recordButtons pins the button hit rects on the selected card's button
// row (content row 4, two cells in from the border).
func (v *View) recordButtons(rc shell.Rect) {
	x, y, w := rc.X+2, rc.Y+4, 0
	for _, lbl := range cardButtons {
		rect := shell.Rect{X: x + w, Y: y, W: shell.Width(lbl), H: 1}
		v.buttonRects = append(v.buttonRects, rect)
		w += rect.W + 2
	}
}

// paneBody renders the right pane card (variant C frame): the empty
// hint, the VCS pane (staged fields + save), or the AI provider pane
// (add form + entry list, hit rects recorded).
func (v *View) paneBody(paneW, height int) string {
	var rows []string
	cw := max(1, paneW-4) // content width: the card frame adds padding+border (cw+4)
	switch {
	case v.pane == paneVCS && v.vcs != nil:
		rows = v.vcsRows(cw, v.paneContentX(paneW), shell.BodyTop+1)
	case v.pane == paneVCS:
		rows = append(rows, shell.StyleFieldLabel.Render("VCS — "+v.paneProject()))
	case v.pane == paneAI:
		aiRows := v.aiPaneRows(paneW)
		v.aiPaneRects(aiRows, leftWidth(v.width)+4, shell.BodyTop+1)
		card := make([]string, len(aiRows))
		for i, r := range aiRows {
			card[i] = r.text // rows are pre-clipped to the pane width
		}
		for len(card) < max(0, height-2) {
			card = append(card, "")
		}
		card = card[:max(0, height-2)]
		var lines []string
		for _, l := range strings.Split(shell.Card(card), "\n") {
			lines = append(lines, " "+l)
		}
		return padLines(lines, 1, paneW, height)
	default:
		rows = append(rows, shell.StyleMutedAlt.Render(hintText))
	}
	// Wrap the pane content to its column width (MaxWidth would clip the
	// long hint).
	wrap := lipgloss.NewStyle().Width(cw)
	var lines []string
	var card []string
	for _, r := range rows {
		card = append(card, wrap.Render(r))
	}
	for len(card) < max(0, height-2) {
		card = append(card, "")
	}
	card = card[:max(0, height-2)]
	// The pane card sits at the pane's column 0 (padLines prefixes nothing
	// here), so a cw-wide card plus its frame always fits paneW.
	for _, l := range strings.Split(shell.Card(card), "\n") {
		lines = append(lines, l)
	}
	return padLines(lines, 0, paneW, height)
}

// paneContentX is the screen column of the pane card's content column 0:
// the left pane plus the card border and its padding.
func (v *View) paneContentX(paneW int) int {
	return leftWidth(v.width) + 2
}

func (v *View) paneProject() string {
	if v.sel >= 0 && v.sel < len(v.projects) {
		return v.projects[v.sel].Name
	}
	return ""
}

// --- small helpers (card-grid idioms shared with the Open Project grid) ---

func divider(n int) string { return strings.Repeat("─", n) }

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
