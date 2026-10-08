// Settings center-view, iteration 2 of feedback (issue #71): sub-menu
// under the header menu (Import project / Export Config / Restore Config /
// Uninstall) over a separator line, options in a New Project (Variant C)
// style card. Import = path entry + folder chooser; export = export button
// + save-as wizard; restore = file chooser; uninstall = option rows +
// typed "uninstall" confirmation before the executable. THROWAWAY prototype.
package protoapp

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var settingsTabs = []string{"Import project", "Export Config", "Restore Config", "Uninstall"}

// fake folder chooser entries: one is a complete sandbox project root,
// the rest are ordinary folders (strict structure check rejects them).
var fakeFolders = []string{
	"/home/dev/oc-sandbox/swdev-core",
	"/home/dev/work/api-server",
	"/home/dev/work/notes",
	"/home/dev/tmp/scratch",
}

// fake config backups for the restore file chooser.
var fakeBackups = []string{
	"sandbox-config-2026-10-07T0914.json",
	"sandbox-config-2026-10-06T1830.json",
	"sandbox-config-2026-10-02T1105.json",
	"sandbox-config-latest.json",
}

type settings71 struct {
	sub      int // 0 import, 1 export, 2 restore, 3 uninstall
	impPath  string
	impCur   int
	impSel   int
	impFocus int // 0 path, 1 chooser, 2 import button

	expStage int // 0 export button, 1 save-as wizard
	expDest  string
	expCur   int
	expFocus int // 0 dest field, 1 save, 2 cancel

	resSel   int
	resFocus int // 0 chooser, 1 restore button

	unOpts    [3]bool // backup, remove symlinks, remove shortcuts
	unFocus   int     // 0..2 opts, 3 confirm field, 4 uninstall button
	unConfirm string
	unCur     int
	toast     string

	tabRects  [4]rect
	impRects  [2]rect // path, button
	foldRects []rect
	impArea   rect
	expRects  [3]rect // dest, save, cancel
	resRects  [2]rect // chooser area, button
	backRects []rect
	unRects   [5]rect // 3 opts, confirm, button
	boxX      int
}

func newSettings71() *settings71 {
	return &settings71{
		impPath: "/home/dev/work/", impCur: len("/home/dev/work/"),
		expDest: pathBase + "/sandbox-config.json", expCur: len(pathBase + "/sandbox-config.json"),
	}
}

func (s *settings71) Name() string { return "settings 71" }

func (s *settings71) focusCount() int {
	switch s.sub {
	case 0:
		return 3
	case 1:
		if s.expStage == 0 {
			return 1
		}
		return 3
	case 2:
		return 2
	default:
		return 5
	}
}

// focusVal returns pointer to the focused per-pane focus index.
func (s *settings71) moveFocus(d int) {
	n := s.focusCount()
	switch s.sub {
	case 0:
		s.impFocus = (s.impFocus + d + n) % n
	case 1:
		if s.expStage != 0 {
			s.expFocus = (s.expFocus + d + n) % n
		}
	case 2:
		s.resFocus = (s.resFocus + d + n) % n
	default:
		s.unFocus = (s.unFocus + d + n) % n
	}
}

// focusedField returns 0 (import path), 1 (export dest), 2 (uninstall
// confirm) when the focus sits on a text field, -1 otherwise.
func (s *settings71) focusedField() int {
	switch s.sub {
	case 0:
		if s.impFocus == 0 {
			return 0
		}
	case 1:
		if s.expStage == 1 && s.expFocus == 0 {
			return 1
		}
	default:
		if s.unFocus == 3 {
			return 2
		}
	}
	return -1
}

func (s *settings71) fieldVal(f int) (*string, *int) {
	switch f {
	case 0:
		return &s.impPath, &s.impCur
	case 1:
		return &s.expDest, &s.expCur
	default:
		return &s.unConfirm, &s.unCur
	}
}

func (s *settings71) Update(msg tea.Msg, width, height int) {
	s.toast = ""
	switch msg := msg.(type) {
	case tea.KeyMsg:
		s.key(msg)
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			s.mouse(msg.X, msg.Y)
		}
	}
}

func (s *settings71) key(msg tea.KeyMsg) {
	f := s.focusedField()
	// printable runes always go to the focused text field (typing
	// "uninstall" contains h/j/k/l, which double as navigation keys)
	if f >= 0 && len(msg.Runes) == 1 {
		v, c := s.fieldVal(f)
		*v = insertRune(*v, *c, msg.Runes[0])
		*c++
		return
	}
	switch msg.String() {
	case "tab", "down", "j":
		s.moveFocus(1)
	case "shift+tab", "up", "k":
		s.moveFocus(-1)
	case "left":
		if f >= 0 {
			_, c := s.fieldVal(f)
			if *c > 0 {
				*c--
			}
		} else {
			s.sub = (s.sub + 3) % 4
		}
	case "right":
		if f >= 0 {
			v, c := s.fieldVal(f)
			if *c < len([]rune(*v)) {
				*c++
			}
		} else {
			s.sub = (s.sub + 1) % 4
		}
	case "enter":
		s.activate()
	case " ":
		if s.sub == 3 && s.unFocus < 3 {
			s.unOpts[s.unFocus] = !s.unOpts[s.unFocus]
		}
	case "backspace":
		if f >= 0 {
			v, c := s.fieldVal(f)
			*v, *c = deleteRune(*v, *c)
		}
	}
}

func (s *settings71) activate() {
	switch s.sub {
	case 0:
		if s.impFocus == 2 {
			s.doImport()
		}
	case 1:
		if s.expStage == 0 {
			s.expStage = 1
			s.expFocus = 0
			return
		}
		switch s.expFocus {
		case 1:
			s.toast = "export → " + s.expDest + " (fake)"
			s.expStage = 0
		case 2:
			s.expStage = 0
		}
	case 2:
		if s.resFocus == 1 {
			s.toast = "restore " + fakeBackups[s.resSel] + " (fake)"
		}
	default:
		switch s.unFocus {
		case 0, 1, 2:
			s.unOpts[s.unFocus] = !s.unOpts[s.unFocus]
		case 3, 4:
			s.runUninstall()
		}
	}
}

func (s *settings71) doImport() {
	p := strings.TrimRight(s.impPath, "/")
	for _, name := range fakeProjects() {
		if p == name.Path {
			s.toast = "registered " + name.Name + " — sandbox_config.json ok (fake)"
			return
		}
	}
	s.toast = "not a sandbox project root → New Project wizard (fake)"
}

func (s *settings71) runUninstall() {
	if !strings.EqualFold(strings.TrimSpace(s.unConfirm), "uninstall") {
		s.toast = "type \"uninstall\" to confirm"
		return
	}
	var flags []string
	if s.unOpts[0] {
		flags = append(flags, "--backup")
	}
	if s.unOpts[1] {
		flags = append(flags, "--remove-symlinks")
	}
	if s.unOpts[2] {
		flags = append(flags, "--remove-shortcuts")
	}
	s.toast = "uninstall.sh " + strings.Join(flags, " ") + " --force (fake)"
}

func (s *settings71) mouse(x, y int) {
	for i, r := range s.tabRects {
		if r.hit(x, y) {
			s.sub = i
			return
		}
	}
	// import pane
	for i, r := range s.foldRects {
		if r.hit(x, y) {
			s.impSel = i
			s.impPath = fakeFolders[i]
			s.impCur = len(s.impPath)
			return
		}
	}
	if s.impRects[0].hit(x, y) {
		s.impFocus = 0
		if s.impCur == 0 {
			s.impCur = len([]rune(s.impPath))
		}
		return
	}
	if s.impArea.hit(x, y) {
		s.impFocus = 1
		return
	}
	if s.impRects[1].hit(x, y) {
		s.doImport()
		return
	}
	// export pane
	if s.expRects[0].hit(x, y) {
		s.expFocus = 0
		return
	}
	if s.expRects[1].hit(x, y) {
		if s.expStage == 0 {
			s.expStage = 1
			s.expFocus = 0
		} else {
			s.toast = "export → " + s.expDest + " (fake)"
			s.expStage = 0
		}
		return
	}
	if s.expStage == 1 && s.expRects[2].hit(x, y) {
		s.expStage = 0
		return
	}
	// restore pane
	for i, r := range s.backRects {
		if r.hit(x, y) {
			s.resSel = i
			s.resFocus = 0
			return
		}
	}
	if s.sub == 2 {
		if s.resRects[0].hit(x, y) {
			s.resFocus = 0
			return
		}
		if s.resRects[1].hit(x, y) {
			s.toast = "restore " + fakeBackups[s.resSel] + " (fake)"
			return
		}
	}
	// uninstall pane
	for i := 0; i < 3; i++ {
		if s.unRects[i].hit(x, y) {
			s.unOpts[i] = !s.unOpts[i]
			s.unFocus = i
			return
		}
	}
	if s.unRects[3].hit(x, y) {
		s.unFocus = 3
		return
	}
	if s.unRects[4].hit(x, y) {
		s.runUninstall()
		return
	}
}

func textField(val string, cursor int, focused bool) string {
	if !focused {
		if val == "" {
			return styleMutedAlt.Render("…")
		}
		return styleCurrentVal.Render(val)
	}
	rs := []rune(val)
	if cursor > len(rs) {
		cursor = len(rs)
	}
	return styleSelectedRow.Render(string(rs[:cursor]) + "▌" + string(rs[cursor:]))
}

// button71 is a single-line button (row-assembled panes need one line
// per row; the bordered button() renders three).
func button71(label string, focused bool) string {
	st := lipgloss.NewStyle().Padding(0, 1)
	if focused {
		st = st.Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true)
	} else {
		st = st.Foreground(colCardFg)
	}
	return st.Render("[ " + label + " ]")
}

func chooserRow(label string, selected bool) string {
	if selected {
		return styleSelectedRow.Render(" ▶ " + label)
	}
	return "   " + styleMutedAlt.Render(label)
}

func optionRow(label string, on bool, focused bool) string {
	box := "☐"
	if on {
		box = "☒"
	}
	line := " " + box + " " + label
	if focused {
		return styleSelectedRow.Render(line)
	}
	if on {
		return styleCurrentVal.Render(line)
	}
	return styleMutedAlt.Render(line)
}

func (s *settings71) View(width, height int, h help.Model, k keyMap) string {
	bodyH := height - 2
	s.foldRects = nil
	s.backRects = nil

	// sub-menu under the header menu, over a separator line
	xTabs := lipgloss.Width(styleHeaderTitle.Render("oc-sandbox"))
	tabs, offs := subTabs(settingsTabs, s.sub)
	tabsRow := strings.Repeat(" ", xTabs-1) + tabs
	for i, o := range offs {
		s.tabRects[i] = rect{x: xTabs + o, y: 2, w: lipgloss.Width(settingsTabs[i]) + 2, h: 1}
	}

	// card (Variant C frame): rows inside a bordered box as wide as the
	// widest content, pinned top-left under the tabs
	var inner []string
	switch s.sub {
	case 0:
		inner = s.importPane()
	case 1:
		inner = s.exportPane()
	case 2:
		inner = s.restorePane()
	default:
		inner = s.uninstallPane()
	}
	maxw := 0
	for _, r := range inner {
		maxw = maxInt(maxw, lipgloss.Width(r))
	}
	boxW := maxw + 4
	s.boxX = 1
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).
		Width(boxW).MaxHeight(bodyH - 2).PaddingLeft(1).PaddingRight(1).
		Render(strings.Join(inner, "\n"))
	rows := []string{separatorRow(width), tabsRow, "", box, styleWarn.Render(s.toast)}
	content := lipgloss.NewStyle().Width(width).Height(bodyH).MaxHeight(bodyH).PaddingLeft(1).
		Render(strings.Join(rows, "\n"))
	return shell(width, height, 3, content, h, k)
}

// cardY maps an inner row index to its screen y (separator row 1, tabs
// row 2, box top border row 4, first inner row row 5).
func cardY(local int) int { return 5 + local }

// cardX is the screen x of card content column 0 (box border + padding).
func cardX() int { return 3 }

func (s *settings71) importPane() []string {
	rows := []string{
		styleMutedAlt.Render("Registers an existing folder as a project"),
		styleMutedAlt.Render("(strict structure check, nothing is moved)."),
		"",
		styleFieldLabel.Render("Project folder path"),
	}
	focused := s.impFocus == 0
	rows = append(rows, "  "+textField(s.impPath, s.impCur, focused))
	s.impRects[0] = rect{x: cardX() + 2, y: cardY(4), w: max(30, lipgloss.Width(s.impPath)+2), h: 1}
	rows = append(rows, "", styleFieldLabel.Render("Folders"))
	areaY := cardY(len(rows))
	for i, f := range fakeFolders {
		rows = append(rows, chooserRow(f, i == s.impSel))
		s.foldRects = append(s.foldRects, rect{x: cardX(), y: cardY(7 + i), w: 60, h: 1})
	}
	btn := button71("Import Project", s.impFocus == 2)
	rows = append(rows, "", "  "+btn)
	s.impArea = rect{x: cardX(), y: areaY, w: 60, h: len(fakeFolders)}
	s.impRects[1] = rect{x: cardX() + 2, y: cardY(12), w: lipgloss.Width(btn), h: 1}
	return rows
}

func (s *settings71) exportPane() []string {
	if s.expStage == 0 {
		btn := button71("Export Config…", false)
		s.expRects[1] = rect{x: cardX() + 2, y: cardY(3), w: lipgloss.Width(btn), h: 1}
		return []string{
			styleMutedAlt.Render("Copies the sandbox_config.json of a project"),
			styleMutedAlt.Render("to a destination you pick (save-as)."),
			"",
			"  " + btn,
		}
	}
	rows := []string{
		styleMutedAlt.Render("Save-as: choose where the config copy goes."),
		"",
		styleFieldLabel.Render("Destination file"),
	}
	focused := s.expFocus == 0
	rows = append(rows, "  "+textField(s.expDest, s.expCur, focused))
	s.expRects[0] = rect{x: cardX() + 2, y: cardY(3), w: max(30, lipgloss.Width(s.expDest)+2), h: 1}
	save := button71("Save", s.expFocus == 1)
	cancel := button71("Cancel", s.expFocus == 2)
	rows = append(rows, "", "  "+save+"  "+cancel)
	s.expRects[1] = rect{x: cardX() + 2, y: cardY(5), w: lipgloss.Width(save), h: 1}
	s.expRects[2] = rect{x: cardX() + 2 + lipgloss.Width(save) + 2, y: cardY(5), w: lipgloss.Width(cancel), h: 1}
	return rows
}

func (s *settings71) restorePane() []string {
	rows := []string{
		styleMutedAlt.Render("Pick a saved config backup to restore it over"),
		styleMutedAlt.Render("the current sandbox_config.json."),
		"",
		styleFieldLabel.Render("Config backups"),
	}
	areaY := cardY(len(rows))
	for i, b := range fakeBackups {
		rows = append(rows, chooserRow(b, i == s.resSel))
		s.backRects = append(s.backRects, rect{x: cardX(), y: cardY(4 + i), w: 60, h: 1})
	}
	btn := button71("Restore", s.resFocus == 1)
	rows = append(rows, "", "  "+btn)
	s.resRects[0] = rect{x: cardX(), y: areaY, w: 60, h: len(fakeBackups)}
	s.resRects[1] = rect{x: cardX() + 2, y: cardY(9), w: lipgloss.Width(btn), h: 1}
	return rows
}

func (s *settings71) uninstallPane() []string {
	rows := []string{
		styleWarn.Render("This removes the oc-sandbox installation."),
		styleMutedAlt.Render("Running containers must be stopped first"),
		styleMutedAlt.Render("(stop them in the Container view)."),
		"",
		styleFieldLabel.Render("Options"),
	}
	labels := []string{"Create config backup", "Remove symlinks", "Remove desktop shortcuts"}
	for i, l := range labels {
		rows = append(rows, optionRow(l, s.unOpts[i], s.unFocus == i))
		s.unRects[i] = rect{x: cardX(), y: cardY(5 + i), w: 60, h: 1}
	}
	rows = append(rows, "", styleFieldLabel.Render("Type \"uninstall\" to confirm"))
	focused := s.unFocus == 3
	rows = append(rows, "  "+textField(s.unConfirm, s.unCur, focused))
	s.unRects[3] = rect{x: cardX() + 2, y: cardY(10), w: max(20, lipgloss.Width(s.unConfirm)+2), h: 1}
	armed := strings.EqualFold(strings.TrimSpace(s.unConfirm), "uninstall")
	var btn string
	if armed {
		btn = button71("Uninstall", s.unFocus == 4)
	} else {
		btn = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Padding(0, 1).
			Render("[ Uninstall ]")
	}
	rows = append(rows, "", "  "+btn)
	s.unRects[4] = rect{x: cardX() + 2, y: cardY(12), w: lipgloss.Width("Uninstall") + 4, h: 1}
	return rows
}
