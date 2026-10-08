// Design A ("A spec"): the ticket, literal. New Project = left menu
// (Blank / From URL) + right matching field rows, prefilled standard
// values, path auto-updates, Create button at the bottom. Container =
// left menu (Build Container / Stop Container); build lists all
// editions selectable + Build button; stop lists running containers
// with select-all + Stop button. THROWAWAY prototype (issue #70).
package protoapp

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type designA struct {
	np       *npState
	ct       *ctState
	ctPane   int // 0 build, 1 stop
	buildSel int // focused edition index
	npBtn    rect
	buildBtn rect
	stopBtn  rect
	selAllY  int
}

func newDesignA() *designA {
	np := newNPState()
	np.fields = npFieldList(np)
	return &designA{np: np, ct: newCTState()}
}

func (d *designA) Name() string { return "A spec" }

func npFieldList(s *npState) []int {
	f := []int{fName, fPath}
	if s.source == 1 {
		f = append(f, fURL)
	}
	return append(f, fEdition, fModes, fStart, fAI, fVCS, fProxy, fCreate)
}

func (d *designA) refresh() { d.np.fields = npFieldList(d.np) }

func (d *designA) UpdateNP(msg tea.Msg, width, height int) {
	s := d.np
	s.toast = ""
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down", "j":
			s.focus = (s.focus + 1) % len(s.fields)
			s.cursor = 0
		case "shift+tab", "up", "k":
			s.focus = (s.focus - 1 + len(s.fields)) % len(s.fields)
			s.cursor = 0
		case "left", "h":
			s.cursor = maxInt(0, s.cursor-1)
		case "right", "l":
			s.cursor = min(len([]rune(s.value(s.fields[s.focus]))), s.cursor+1)
		case "enter":
			d.npActivate(s.fields[s.focus])
		case "backspace":
			s.backspace(s.fields[s.focus])
		default:
			if len(msg.Runes) == 1 {
				s.input(s.fields[s.focus], msg.Runes[0])
			}
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			// left menu: source items at body rows 0/1 (left column)
			if msg.X < menuW && msg.Y >= 1 && msg.Y <= 2 {
				d.np.source = msg.Y - 1
				d.np.focus = 0
				d.refresh()
				return
			}
			// right pane rows start at y=1, one line per field
			idx := msg.Y - 1
			if idx >= 0 && idx < len(s.fields) {
				id := s.fields[idx]
				if id == fCreate {
					s.create()
				} else if id == s.fields[s.focus] && idx != 0 {
					s.nextVal(id) // second click on a select toggles
				}
				s.focus = idx
			}
			if d.npBtn.hit(msg.X, msg.Y) {
				s.create()
			}
		}
	}
}

func (d *designA) npActivate(id int) {
	s := d.np
	switch id {
	case fName, fPath:
		s.toast = "(type to edit the name; path follows)"
	case fURL:
		s.toast = "(type to edit the URL)"
	case fCreate:
		s.create()
	default:
		s.nextVal(id)
	}
}

const menuW = 18

func (d *designA) ViewNP(width, height int, h help.Model, k keyMap) string {
	s := d.np
	bodyH := height - 2

	// left menu: source items, then a hint
	menuLines := []string{
		styleFieldLabel.Render("Source"),
		menuItem("Blank project", s.source == 0),
		menuItem("From URL", s.source == 1),
		styleMutedAlt.Render("(URL field when From URL)"),
	}

	// right pane: one line per field, then toast + button
	var rows []string
	for _, id := range s.fields {
		if id == fCreate {
			continue
		}
		rows = append(rows, d.fieldRow(id))
	}
	focusIdx := 0
	for i, id := range s.fields {
		if s.focus < len(s.fields) && id == s.fields[s.focus] {
			focusIdx = i
		}
	}
	// ensure focus tracks the visible field list
	if s.focus >= len(s.fields) {
		s.focus = 0
	}
	_ = focusIdx
	btnY := len(rows) // pane line index of the button
	rows = append(rows, "")
	if s.toast != "" {
		rows = append(rows, styleWarn.Render(s.toast))
		btnY++
	}
	btn := button("Create Project", s.fields[s.focus] == fCreate)
	rows = append(rows, btn)
	d.npBtn = rect{x: menuW + 1, y: 1 + btnY, w: lipgloss.Width(btn), h: 3}

	pane := lipgloss.NewStyle().Width(width-menuW-2).Height(bodyH).MaxHeight(bodyH).PaddingLeft(1).
		Render(strings.Join(rows, "\n"))
	menu := lipgloss.NewStyle().Width(menuW).Height(bodyH).MaxHeight(bodyH).Render(strings.Join(menuLines, "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, menu, pane)
	return shell(width, height, 1, body, h, k)
}

func (d *designA) fieldRow(id int) string {
	s := d.np
	label := fmt.Sprintf("%-24s", fieldLabel(id))
	focused := s.fields[s.focus] == id
	val := s.value(id)
	switch id {
	case fName, fURL:
		if focused {
			rs := []rune(val)
			if s.cursor > len(rs) {
				s.cursor = len(rs)
			}
			val = string(rs[:s.cursor]) + "▌" + string(rs[s.cursor:])
			return styleSelectedRow.Render(label + val)
		}
		if val == "" && id == fName {
			val = styleMutedAlt.Render("…")
		}
		return label + val
	case fPath:
		return styleMutedAlt.Render(label + val)
	default:
		line := label
		cur := s.value(id)
		alt := alternatives(id, cur)
		line += styleCurrentVal.Render("● " + cur)
		if alt != "" {
			line += "  " + styleMutedAlt.Render("○ " + alt)
		}
		if focused {
			return styleSelectedRow.Render(line)
		}
		return line
	}
}

// alternatives lists the not-current options, compacted for one line.
func alternatives(id int, cur string) string {
	var all []string
	switch id {
	case fEdition:
		all = []string{"swdev", "webdev", "hil", "custom", "writing", "ros2"}
	case fModes:
		all = []string{"none", "offline", "hil_mode", "cbm_ui"}
	case fStart:
		all = []string{"opencode", "console", "web"}
	case fAI:
		all = []string{"gwdg-saia", "none"}
	case fVCS:
		all = []string{"none", "github.com", "gitlab.com", "own GitLab", "others"}
	case fProxy:
		all = []string{"yes", "no"}
	default:
		return ""
	}
	var out []string
	for _, a := range all {
		if a != cur {
			out = append(out, a)
		}
	}
	sep := " · "
	res := strings.Join(out, sep)
	if len(res) > 34 {
		res = res[:34] + "…"
	}
	return res
}

func menuItem(label string, selected bool) string {
	if selected {
		return lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true).Width(menuW - 2).Render(label)
	}
	return styleCardFg.Render(label)
}

var styleCardFg = lipgloss.NewStyle().Foreground(colCardFg)

func button(label string, focused bool) string {
	st := lipgloss.NewStyle().Padding(0, 2)
	if focused {
		st = st.Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true)
	} else {
		st = st.Foreground(colCardFg).Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))
	}
	return st.Render(label)
}

// --- Container view (design A) ---

func (d *designA) UpdateCT(msg tea.Msg, width, height int) {
	c := d.ct
	c.toast = ""
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "down", "j":
			if d.ctPane == 0 {
				d.buildSel = (d.buildSel + 1) % len(editions)
			}
		case "up", "k":
			if d.ctPane == 0 {
				d.buildSel = (d.buildSel - 1 + len(editions)) % len(editions)
			}
		case "enter":
			if d.ctPane == 0 {
				c.build(editions[d.buildSel])
			} else {
				c.stop(fakeRunning())
			}
		case " ":
			if d.ctPane == 1 {
				// space toggles select-all
				c.toggleAll(fakeRunning())
			}
		}
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
			return
		}
		if msg.X < menuW && msg.Y >= 1 && msg.Y <= 2 {
			d.ctPane = msg.Y - 1
			return
		}
		running := fakeRunning()
		if d.ctPane == 0 {
			// edition rows: y=1..n
			idx := msg.Y - 1
			if idx >= 0 && idx < len(editions) {
				if d.buildSel == idx {
					c.build(editions[idx]) // second click builds
				}
				d.buildSel = idx
			}
			if d.buildBtn.hit(msg.X, msg.Y) {
				c.build(editions[d.buildSel])
			}
		} else {
			// rows: select-all at y=1, containers at y=2..
			if msg.Y == 1 && msg.X >= menuW {
				c.toggleAll(running)
			}
			idx := msg.Y - 2
			if idx >= 0 && idx < len(running) {
				c.selection[running[idx].Name] = !c.selection[running[idx].Name]
			}
			if d.stopBtn.hit(msg.X, msg.Y) {
				c.stop(running)
			}
		}
	}
}

func (d *designA) ViewCT(width, height int, h help.Model, k keyMap) string {
	bodyH := height - 2
	menuLines := []string{
		menuItem("Build Container", d.ctPane == 0),
		menuItem("Stop Container", d.ctPane == 1),
	}
	var rows []string
	if d.ctPane == 0 {
		for i, e := range editions {
			if i == d.buildSel {
				rows = append(rows, styleSelectedRow.Render(" ▸ "+shortEdition(e)))
			} else {
				rows = append(rows, styleCardFg.Render("   "+shortEdition(e)))
			}
		}
		btn := button("Build", false)
		rows = append(rows, "")
		if d.ct.toast != "" {
			rows = append(rows, styleWarn.Render(d.ct.toast))
		}
		rows = append(rows, btn)
		d.buildBtn = rect{x: menuW + 1, y: 1 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}
	} else {
		running := fakeRunning()
		all := d.ct.allSelected(running)
		box := "☐"
		if all {
			box = "☒"
		}
		rows = append(rows, styleCardFg.Render(" "+box+" select all"))
		d.selAllY = 1
		for _, p := range running {
			box = "☐"
			if d.ct.selection[p.Name] {
				box = "☒"
			}
			rows = append(rows, styleCardFg.Render(" "+box+" "+p.Name+"  "+styleStatusRun.Render("● running")))
		}
		rows = append(rows, "")
		if d.ct.toast != "" {
			rows = append(rows, styleWarn.Render(d.ct.toast))
		}
		btn := button("Stop", false)
		rows = append(rows, btn)
		d.stopBtn = rect{x: menuW + 1, y: 1 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}
	}
	pane := lipgloss.NewStyle().Width(width-menuW-2).Height(bodyH).MaxHeight(bodyH).PaddingLeft(1).
		Render(strings.Join(rows, "\n"))
	menu := lipgloss.NewStyle().Width(menuW).Height(bodyH).MaxHeight(bodyH).Render(strings.Join(menuLines, "\n"))
	body := lipgloss.JoinHorizontal(lipgloss.Top, menu, pane)
	return shell(width, height, 2, body, h, k)
}

// rect is a screen-space hit box (see root.pillGeometry for the pattern).
type rect struct{ x, y, w, h int }

func (r rect) hit(x, y int) bool {
	return y >= r.y && y < r.y+r.h && x >= r.x && x < r.x+r.w
}

var _ help.Model
