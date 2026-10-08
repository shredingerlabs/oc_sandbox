// Settings center-view, third prototype iteration (issue #71): left menu
// (Import existing project / Export+Restore Sandbox Config / Uninstall),
// right pane per menu item. Import = path entry + folder chooser; export =
// export button + save-as wizard; restore = file chooser; uninstall =
// option rows + typed "uninstall" confirmation before the executable.
// THROWAWAY prototype.
package protoapp

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const setMenuW = 26

var settingsMenuItems = []string{"Import existing project", "Export/Restore Sandbox Config", "Uninstall"}

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
	menu int // 0 import, 1 export/restore, 2 uninstall
	sub  int // export/restore sub-pane: 0 export, 1 restore

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

	menuRects []rect
	impRects  [3]rect // path, chooser box, button
	foldRects []rect
	subRects  [2]rect
	expRects  [3]rect // dest, save, cancel
	resRects  [2]rect // chooser box, button
	backRects []rect
	unRects   [5]rect // 3 opts, confirm, button
}

func newSettings71() *settings71 {
	return &settings71{impPath: "/home/dev/work/", impCur: len("/home/dev/work/"), expDest: pathBase + "/sandbox-config.json", expCur: len(pathBase + "/sandbox-config.json")}
}

func (s *settings71) Name() string { return "settings 71" }

// focus count per pane
func (s *settings71) focusCount() int {
	switch s.menu {
	case 0:
		return 3
	case 1:
		if s.sub == 0 {
			if s.expStage == 0 {
				return 1
			}
			return 3
		}
		return 2
	default:
		return 5
	}
}

func (s *settings71) moveFocus(d int) {
	n := s.focusCount()
	switch s.menu {
	case 0:
		s.impFocus = (s.impFocus + d + n) % n
	case 1:
		if s.sub == 0 {
			if s.expStage == 0 {
				return
			}
			s.expFocus = (s.expFocus + d + n) % n
		} else {
			s.resFocus = (s.resFocus + d + n) % n
		}
	default:
		s.unFocus = (s.unFocus + d + n) % n
	}
}

func (s *settings71) focusedField() int {
	switch s.menu {
	case 0:
		if s.impFocus == 0 {
			return 0
		}
	case 1:
		if s.sub == 0 && s.expStage == 1 && s.expFocus == 0 {
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
			s.menu = (s.menu + 2) % 3
		}
	case "right":
		if f >= 0 {
			v, c := s.fieldVal(f)
			if *c < len([]rune(*v)) {
				*c++
			}
		} else {
			s.menu = (s.menu + 1) % 3
		}
	case "enter":
		s.activate()
	case " ":
		if s.menu == 2 && s.unFocus < 3 {
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
	switch s.menu {
	case 0:
		switch s.impFocus {
		case 2:
			s.doImport()
		}
	case 1:
		if s.sub == 0 {
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
		} else {
			if s.resFocus == 1 {
				s.toast = "restore " + fakeBackups[s.resSel] + " (fake)"
			}
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
	if strings.TrimSpace(s.unConfirm) != "uninstall" {
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
	for i, r := range s.menuRects {
		if r.hit(x, y) {
			s.menu = i
			return
		}
	}
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
		s.impCur = min(s.impCur, len([]rune(s.impPath)))
		if s.impCur == 0 {
			s.impCur = len([]rune(s.impPath))
		}
		return
	}
	if s.impRects[1].hit(x, y) {
		s.impFocus = 1
		return
	}
	if s.impRects[2].hit(x, y) {
		s.doImport()
		return
	}
	if s.menu == 1 {
		for i, r := range s.subRects {
			if r.hit(x, y) {
				s.sub = i
				return
			}
		}
		if s.sub == 0 {
			if s.expStage == 1 {
				if s.expRects[0].hit(x, y) {
					s.expFocus = 0
					return
				}
				if s.expRects[1].hit(x, y) {
					s.toast = "export → " + s.expDest + " (fake)"
					s.expStage = 0
					return
				}
				if s.expRects[2].hit(x, y) {
					s.expStage = 0
					return
				}
			} else if s.expRects[1].hit(x, y) {
				s.expStage = 1
				s.expFocus = 0
				return
			}
		} else {
			for i, r := range s.backRects {
				if r.hit(x, y) {
					s.resSel = i
					s.resFocus = 0
					return
				}
			}
			if s.resRects[1].hit(x, y) {
				s.toast = "restore " + fakeBackups[s.resSel] + " (fake)"
				return
			}
		}
	}
	if s.menu == 2 {
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
}

func (s *settings71) menuItemLines(i int) []string {
	if i == 1 {
		return []string{"Export/Restore", "Sandbox Config"}
	}
	return []string{settingsMenuItems[i]}
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
	s.menuRects = nil
	s.foldRects = nil
	s.backRects = nil

	// menu column lines with per-item spans
	var menuLines []string
	spans := [][2]int{} // start,end line per item
	for i := range settingsMenuItems {
		lines := s.menuItemLines(i)
		spans = append(spans, [2]int{len(menuLines), len(menuLines) + len(lines) - 1})
		for _, l := range lines {
			st := lipgloss.NewStyle().Width(setMenuW).Padding(0, 1)
			if i == s.menu {
				st = st.Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true)
			} else {
				st = st.Foreground(colCardFg)
			}
			menuLines = append(menuLines, st.Render(l))
		}
	}
	menuTitle := styleFieldLabel.Render("Settings")
	menuBlock := append([]string{menuTitle, ""}, menuLines...)

	// right pane rows
	var right []string
	title := []string{styleFieldLabel.Render(settingsMenuItems[s.menu]), ""}
	switch s.menu {
	case 0:
		right = append(right, title...)
		right = append(right, s.importPane()...)
	case 1:
		right = append(right, title...)
		if s.sub == 0 {
			right = append(right, s.exportPane()...)
		} else {
			right = append(right, s.restorePane()...)
		}
	default:
		right = append(right, title...)
		right = append(right, s.uninstallPane()...)
	}

	// assemble rows: menu col + gap + right col
	n := max(len(menuBlock), len(right))
	var rows []string
	for i := 0; i < n; i++ {
		var ml, rl string
		if i < len(menuBlock) {
			ml = menuBlock[i]
		} else {
			ml = strings.Repeat(" ", setMenuW)
		}
		if i < len(right) {
			rl = right[i]
		}
		rows = append(rows, ml+"  "+rl)
	}
	// menu hit rects (body row i → screen y = 1+i)
	for _, sp := range spans {
		s.menuRects = append(s.menuRects, rect{x: 1, y: 3 + sp[0], w: setMenuW, h: sp[1] - sp[0] + 1})
	}
	// toast row always reserved so stored rects stay stable
	rows = append(rows, styleWarn.Render(s.toast))
	content := lipgloss.NewStyle().Width(width).Height(bodyH).MaxHeight(bodyH).PaddingLeft(1).
		Render(strings.Join(rows, "\n"))
	return shell(width, height, 3, content, h, k)
}

// paneX is the screen x of right-pane content column 0.
func paneX() int { return 1 + setMenuW + 2 }

func (s *settings71) importPane() []string {
	rows := []string{
		styleMutedAlt.Render("Registers an existing folder as a project"),
		styleMutedAlt.Render("(strict structure check, nothing is moved)."),
		"",
		styleFieldLabel.Render("Project folder path"),
	}
	focused := s.impFocus == 0
	rows = append(rows, "  "+textField(s.impPath, s.impCur, focused))
	s.impRects[0] = rect{x: paneX() + 2, y: 7, w: max(30, lipgloss.Width(s.impPath)+2), h: 1}
	rows = append(rows, "", styleFieldLabel.Render("Folders"))
	for i, f := range fakeFolders {
		rows = append(rows, chooserRow(f, i == s.impSel))
		s.foldRects = append(s.foldRects, rect{x: paneX(), y: 10 + i, w: 60, h: 1})
	}
	btn := button71("Import Project", s.impFocus == 2)
	rows = append(rows, "", "  "+btn)
	s.impRects[1] = rect{x: paneX(), y: 10, w: 60, h: len(fakeFolders)}
	s.impRects[2] = rect{x: paneX() + 2, y: 15, w: lipgloss.Width(btn), h: 1}
	return rows
}

func (s *settings71) exportPane() []string {
	// sub-tabs Export | Restore, aligned like the Container tabs
	xTabs := lipgloss.Width(styleHeaderTitle.Render("oc-sandbox"))
	l, r := "Export", "Restore"
	st := lipgloss.NewStyle().Padding(0, 1)
	sel := st.Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true)
	if s.sub == 0 {
		l = sel.Render(l)
		r = st.Render(r)
	} else {
		l = st.Render(l)
		r = sel.Render(r)
	}
	tabs := strings.Repeat(" ", xTabs-1) + l + r
	s.subRects[0] = rect{x: xTabs, y: 3, w: lipgloss.Width("Export") + 2, h: 1}
	s.subRects[1] = rect{x: xTabs + lipgloss.Width("Export") + 2, y: 3, w: lipgloss.Width("Restore") + 2, h: 1}
	rows := []string{tabs, ""}
	if s.expStage == 0 {
		rows = append(rows,
			styleMutedAlt.Render("Copies the sandbox_config.json of a project"),
			styleMutedAlt.Render("to a destination you pick (save-as)."),
			"",
			"  "+button("Export Config…", false))
		s.expRects[1] = rect{x: paneX() + 2, y: 8, w: lipgloss.Width("Export Config…") + 4, h: 1}
		return rows
	}
	rows = append(rows,
		styleMutedAlt.Render("Save-as: choose where the config copy goes."),
		"",
		styleFieldLabel.Render("Destination file"),
	)
	focused := s.expFocus == 0
	rows = append(rows, "  "+textField(s.expDest, s.expCur, focused))
	s.expRects[0] = rect{x: paneX() + 2, y: 8, w: max(30, lipgloss.Width(s.expDest)+2), h: 1}
	save := button71("Save", s.expFocus == 1)
	cancel := button71("Cancel", s.expFocus == 2)
	rows = append(rows, "", "  "+save+"  "+cancel)
	s.expRects[1] = rect{x: paneX() + 2, y: 10, w: lipgloss.Width(save), h: 1}
	s.expRects[2] = rect{x: paneX() + 2 + lipgloss.Width(save) + 2, y: 10, w: lipgloss.Width(cancel), h: 1}
	return rows
}

func (s *settings71) restorePane() []string {
	xTabs := lipgloss.Width(styleHeaderTitle.Render("oc-sandbox"))
	l, r := "Export", "Restore"
	st := lipgloss.NewStyle().Padding(0, 1)
	sel := st.Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true)
	if s.sub == 0 {
		l = sel.Render(l)
		r = st.Render(r)
	} else {
		l = st.Render(l)
		r = sel.Render(r)
	}
	tabs := strings.Repeat(" ", xTabs-1) + l + r
	s.subRects[0] = rect{x: xTabs, y: 3, w: lipgloss.Width("Export") + 2, h: 1}
	s.subRects[1] = rect{x: xTabs + lipgloss.Width("Export") + 2, y: 3, w: lipgloss.Width("Restore") + 2, h: 1}
	rows := []string{tabs, "",
		styleMutedAlt.Render("Pick a saved config backup to restore it over"),
		styleMutedAlt.Render("the current sandbox_config.json."),
		"",
		styleFieldLabel.Render("Config backups"),
	}
	for i, b := range fakeBackups {
		rows = append(rows, chooserRow(b, i == s.resSel))
		s.backRects = append(s.backRects, rect{x: paneX(), y: 9 + i, w: 60, h: 1})
	}
	btn := button71("Restore", s.resFocus == 1)
	rows = append(rows, "", "  "+btn)
	s.resRects[0] = rect{x: paneX(), y: 9, w: 60, h: len(fakeBackups)}
	s.resRects[1] = rect{x: paneX() + 2, y: 14, w: lipgloss.Width(btn), h: 1}
	return rows
}

func (s *settings71) uninstallPane() []string {
	rows := []string{
		styleWarn.Render("This removes the oc-sandbox installation."),
		styleMutedAlt.Render("Running containers must be stopped first"),
		styleMutedAlt.Render("(see their stop commands in the Container view)."),
		"",
		styleFieldLabel.Render("Options"),
	}
	labels := []string{"Create config backup", "Remove symlinks", "Remove desktop shortcuts"}
	for i, l := range labels {
		rows = append(rows, optionRow(l, s.unOpts[i], s.unFocus == i))
		s.unRects[i] = rect{x: paneX(), y: 8 + i, w: 60, h: 1}
	}
	rows = append(rows, "", styleFieldLabel.Render("Type \"uninstall\" to confirm"))
	focused := s.unFocus == 3
	rows = append(rows, "  "+textField(s.unConfirm, s.unCur, focused))
	s.unRects[3] = rect{x: paneX() + 2, y: 13, w: max(20, lipgloss.Width(s.unConfirm)+2), h: 1}
	armed := strings.TrimSpace(s.unConfirm) == "uninstall"
	var btn string
	if armed {
		btn = button71("Uninstall", s.unFocus == 4)
	} else {
		btn = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Padding(0, 1).
			Render("[ Uninstall ]")
	}
	rows = append(rows, "", "  "+btn)
	s.unRects[4] = rect{x: paneX() + 2, y: 15, w: lipgloss.Width("Uninstall") + 4, h: 1}
	if !armed {
		s.toast = ""
	}
	return rows
}
