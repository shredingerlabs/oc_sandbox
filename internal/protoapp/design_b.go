// Design B ("B flow"): no left menus anywhere — both center-views are
// single columns. New Project: segmented source control on top, field
// rows below, Create button right-aligned at the bottom. Container:
// segmented Build/Stop control; build = wrapping edition chips; stop =
// checklist + Stop. THROWAWAY prototype (issue #70).
package protoapp

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type designB struct {
	np       *npState
	ct       *ctState
	ctPane   int
	buildSel int
	npBtn    rect
	ctBtn    rect
	chips    []rect
	segY     int
}

func newDesignB() *designB {
	np := newNPState()
	np.fields = npFieldList(np)
	return &designB{np: np, ct: newCTState()}
}

func (d *designB) Name() string { return "B flow" }

func (d *designB) UpdateNP(msg tea.Msg, width, height int) {
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
			if s.focus == 0 {
				s.source = 0
				d.refresh()
				return
			}
			s.cursor = maxInt(0, s.cursor-1)
		case "right", "l":
			if s.focus == 0 {
				s.source = 1
				d.refresh()
				return
			}
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
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
			return
		}
		// segmented control on body row 0 (y=1)
		if msg.Y == 1 {
			if msg.X < width/2 {
				s.source = 0
			} else {
				s.source = 1
			}
			s.focus = 0
			d.refresh()
			return
		}
		// fields start at y=3
		idx := msg.Y - 3
		if idx >= 0 && idx < len(s.fields) {
			id := s.fields[idx]
			if id == fCreate {
				s.create()
			} else if id == fPath || id == fName || id == fURL {
				s.focus = idx
			} else if s.focus == idx {
				s.nextVal(id)
			} else {
				s.focus = idx
			}
		}
		if d.npBtn.hit(msg.X, msg.Y) {
			s.create()
		}
	}
}

func (d *designB) refresh() { d.np.fields = npFieldList(d.np) }

func (d *designB) npActivate(id int) {
	s := d.np
	if id == fCreate {
		s.create()
		return
	}
	if s.nextVal(id) {
		return
	}
	s.toast = "(type to edit)"
}

const flowW = 64

func (d *designB) ViewNP(width, height int, h help.Model, k keyMap) string {
	s := d.np
	bodyH := height - 2
	rows := []string{segment("Source", "Blank project", "From URL", s.source), ""}
	for _, id := range s.fields {
		if id == fCreate {
			continue
		}
		rows = append(rows, d.fieldRow(id))
	}
	rows = append(rows, "")
	if s.toast != "" {
		rows = append(rows, styleWarn.Render(s.toast))
	}
	btn := button("Create Project", s.fields[s.focus] == fCreate)
	// right-align the button within the flow column
	rows = append(rows, alignRight(flowW-4, btn))
	d.npBtn = rect{x: flowW - 4 - lipgloss.Width(btn), y: 1 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}

	col := lipgloss.NewStyle().Width(flowW).Height(bodyH).MaxHeight(bodyH).Padding(0, 2).
		Render(strings.Join(rows, "\n"))
	body := lipgloss.NewStyle().Width(width).MaxWidth(width).Render(
		lipgloss.Place(width, bodyH, lipgloss.Center, lipgloss.Top, col))
	return shell(width, height, 1, body, h, k)
}

// segment renders a two-way segmented control on one line.
func segment(prefix, left, right string, sel int) string {
	st := lipgloss.NewStyle().Padding(0, 1)
	if sel == 0 {
		left = "[" + left + "]"
	} else {
		right = "[" + right + "]"
	}
	out := st.Render(left) + st.Render(right)
	if prefix != "" {
		out = styleFieldLabel.Render(prefix+" ") + out
	}
	return out
}

// alignRight right-aligns a (possibly multiline) block in n columns.
func alignRight(n int, s string) string {
	return lipgloss.NewStyle().Width(n).Align(lipgloss.Right).Render(s)
}

func (d *designB) fieldRow(id int) string {
	s := d.np
	label := fieldLabel(id) + ":"
	val := s.value(id)
	focused := s.fields[s.focus] == id
	switch id {
	case fName, fURL:
		if focused {
			rs := []rune(val)
			if s.cursor > len(rs) {
				s.cursor = len(rs)
			}
			val = string(rs[:s.cursor]) + "▌" + string(rs[s.cursor:])
			return styleSelectedRow.Render(label + " " + val)
		}
		if val == "" && id == fName {
			val = styleMutedAlt.Render("…")
		}
		return label + " " + val
	case fPath:
		return styleMutedAlt.Render(label + " " + val)
	default:
		line := label + " " + styleCurrentVal.Render("● "+val)
		if alt := alternatives(id, val); alt != "" {
			line += "  " + styleMutedAlt.Render("○ " + alt)
		}
		if focused {
			return styleSelectedRow.Render(line)
		}
		return line
	}
}

// --- Container (design B) ---

func (d *designB) UpdateCT(msg tea.Msg, width, height int) {
	c := d.ct
	c.toast = ""
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			d.ctPane = 0
		case "right", "l":
			d.ctPane = 1
		case "tab", "down", "j":
			if d.ctPane == 0 {
				d.buildSel = (d.buildSel + 1) % len(editions)
			}
		case "up", "k":
			if d.ctPane == 0 {
				d.buildSel = (d.buildSel - 1 + len(editions)) % len(editions)
			}
		case " ":
			if d.ctPane == 1 {
				c.toggleAll(fakeRunning())
			}
		case "enter":
			if d.ctPane == 0 {
				c.build(editions[d.buildSel])
			} else {
				c.stop(fakeRunning())
			}
		}
	case tea.MouseMsg:
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
			return
		}
		running := fakeRunning()
		if msg.Y == 1 { // segmented Build | Stop
			if msg.X < width/2 {
				d.ctPane = 0
			} else {
				d.ctPane = 1
			}
			return
		}
		if d.ctPane == 0 {
			for i, ch := range d.chips {
				if ch.hit(msg.X, msg.Y) {
					if d.buildSel == i {
						c.build(editions[i])
					}
					d.buildSel = i
					return
				}
			}
			if d.ctBtn.hit(msg.X, msg.Y) {
				c.build(editions[d.buildSel])
			}
		} else {
			if msg.Y == 3 && msg.X >= 2 {
				c.toggleAll(running)
			}
			idx := msg.Y - 4
			if idx >= 0 && idx < len(running) {
				c.selection[running[idx].Name] = !c.selection[running[idx].Name]
			}
			if d.ctBtn.hit(msg.X, msg.Y) {
				c.stop(running)
			}
		}
	}
}

func (d *designB) ViewCT(width, height int, h help.Model, k keyMap) string {
	bodyH := height - 2
	seg := segment("", "Build Container", "Stop Container", d.ctPane)
	rows := []string{seg, ""}
	d.chips = nil
	if d.ctPane == 0 {
		// wrapping edition chips
		line := styleFieldLabel.Render("Editions") + "  "
		lineW := lipgloss.Width(line)
		var lineBuf strings.Builder
		x := 0
		y := 2
		rowStartX := 2
		for i, e := range editions {
			chip := chipStyle(i == d.buildSel).Render(shortEdition(e))
			cw := lipgloss.Width(chip) + 2
			if lineW+cw > flowW-2 {
				rows = append(rows, lineBuf.String())
				lineBuf.Reset()
				lineW = 0
				x = rowStartX
				y++
			}
			d.chips = append(d.chips, rect{x: x + lineW, y: 1 + y, w: cw, h: 1})
			lineBuf.WriteString(chip + "  ")
			lineW += cw
		}
		rows = append(rows, lineBuf.String())
		if d.ct.toast != "" {
			rows = append(rows, styleWarn.Render(d.ct.toast))
		}
		btn := button("Build", false)

		rows = append(rows, alignRight(flowW-4, btn))
		d.ctBtn = rect{x: flowW - 4 - lipgloss.Width(btn), y: 1 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}
	} else {
		running := fakeRunning()
		box := "☐"
		if d.ct.allSelected(running) {
			box = "☒"
		}
		rows = append(rows, " "+box+" select all")
		for _, p := range running {
			box = "☐"
			if d.ct.selection[p.Name] {
				box = "☒"
			}
			rows = append(rows, " "+box+" "+p.Name+"  "+styleStatusRun.Render("● running"))
		}
		if d.ct.toast != "" {
			rows = append(rows, styleWarn.Render(d.ct.toast))
		}
		btn := button("Stop", false)

		rows = append(rows, alignRight(flowW-4, btn))
		d.ctBtn = rect{x: flowW - 4 - lipgloss.Width(btn), y: 1 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}
	}
	col := lipgloss.NewStyle().Width(flowW).Height(bodyH).MaxHeight(bodyH).Padding(0, 2).
		Render(strings.Join(rows, "\n"))
	body := lipgloss.NewStyle().Width(width).MaxWidth(width).Render(
		lipgloss.Place(width, bodyH, lipgloss.Center, lipgloss.Top, col))
	return shell(width, height, 2, body, h, k)
}

func chipStyle(selected bool) lipgloss.Style {
	if selected {
		return lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1)
	}
	return lipgloss.NewStyle().Foreground(colCardFg).Padding(0, 1)
}

var _ help.Model
