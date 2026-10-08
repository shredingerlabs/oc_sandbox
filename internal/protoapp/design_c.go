// Design C ("C dialog"): New Project as a centered bordered dialog —
// source tabs inside the box, label-above-value fields, Create button
// bottom-right. Container: Build/Stop as tabs across the top (no left
// menu); build = 2-column edition cards; stop = container rows with
// checkboxes. THROWAWAY prototype (issue #70).
package protoapp

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type designC struct {
	np       *npState
	ct       *ctState
	ctPane   int
	buildSel int
	npBtn    rect
	ctBtn    rect
	boxX     int
	boxW     int
}

func newDesignC() *designC {
	np := newNPState()
	np.fields = npFieldList(np)
	return &designC{np: np, ct: newCTState()}
}

func (d *designC) Name() string { return "C dialog" }

func (d *designC) refresh() { d.np.fields = npFieldList(d.np) }

func (d *designC) UpdateNP(msg tea.Msg, width, height int) {
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
		if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
			return
		}
		if d.npBtn.hit(msg.X, msg.Y) {
			s.create()
			return
		}
		// dialog tabs at y = 3 (border 1, blank 2)
		if msg.Y == 3 && msg.X >= d.boxX && msg.X < d.boxX+d.boxW {
			if msg.X < d.boxX+d.boxW/2 {
				s.source = 0
			} else {
				s.source = 1
			}
			s.focus = 0
			d.refresh()
			return
		}
		// fields: two lines each (label, value) starting y=4
		fi := (msg.Y - 4) / 2
		if fi >= 0 && fi < len(s.fields) {
			id := s.fields[fi]
			if id == fCreate {
				s.create()
			} else if s.focus == fi && d.nextVal2(id) {
			} else {
				s.focus = fi
			}
		}
	}
}

// nextVal2 is nextVal that ignores the create button id.
func (d *designC) nextVal2(id int) bool {
	if id == fCreate {
		return false
	}
	return d.np.nextVal(id)
}

func (d *designC) npActivate(id int) {
	s := d.np
	if id == fCreate {
		s.create()
		return
	}
	if d.nextVal2(id) {
		return
	}
	s.toast = "(type to edit)"
}

const boxW = 64

func (d *designC) ViewNP(width, height int, h help.Model, k keyMap) string {
	s := d.np
	bodyH := height - 2
	rows := []string{"", d.dialogTabs(s.source)}
	for _, id := range s.fields {
		if id == fCreate {
			continue
		}
		rows = append(rows, styleMutedAlt.Render(fieldLabel(id)), d.dlgValue(id))
	}
	rows = append(rows, "")
	if s.toast != "" {
		rows = append(rows, styleWarn.Render(s.toast))
	}
	btn := button("Create Project", s.fields[s.focus] == fCreate)
	rows = append(rows, alignRight(boxW-4, btn))

	inner := lipgloss.NewStyle().Width(boxW - 2).Padding(0, 1).Render(strings.Join(rows, "\n"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).
		Width(boxW).MaxHeight(bodyH).Render(inner)
	x0 := (width - boxW) / 2
	if x0 < 0 {
		x0 = 0
	}
	// button rect in screen space (box starts at body top, right-aligned
	// within the inner width)
	d.npBtn = rect{x: x0 + 2 + (boxW - 4) - lipgloss.Width(btn), y: 2 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}
	d.boxX = x0
	d.boxW = boxW
	body := lipgloss.NewStyle().Width(width).MaxWidth(width).Render(padLeft(x0, box))
	return shell(width, height, 1, body, h, k)
}

func padLeft(n int, s string) string {
	if n <= 0 {
		return s
	}
	pad := strings.Repeat(" ", n)
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = pad + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (d *designC) dialogTabs(sel int) string {
	l := lipgloss.NewStyle().Padding(0, 1).Render("Blank project")
	r := lipgloss.NewStyle().Padding(0, 1).Render("From URL")
	if sel == 0 {
		l = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1).Render("Blank project")
	} else {
		r = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1).Render("From URL")
	}
	return l + r
}

func (d *designC) dlgValue(id int) string {
	s := d.np
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
			return styleSelectedRow.Render(val)
		}
		if val == "" && id == fName {
			return styleMutedAlt.Render("…")
		}
		return styleCurrentVal.Render(val)
	case fPath:
		return styleMutedAlt.Render(val)
	default:
		line := styleCurrentVal.Render("● "+val)
		if alt := alternatives(id, val); alt != "" {
			line += "  " + styleMutedAlt.Render("○ " + alt)
		}
		if focused {
			return styleSelectedRow.Render(line)
		}
		return line
	}
}

// --- Container (design C): tabs on top, edition cards ---

func (d *designC) UpdateCT(msg tea.Msg, width, height int) {
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
			d.buildSel = (d.buildSel + 1) % len(editions)
		case "up", "k":
			d.buildSel = (d.buildSel - 1 + len(editions)) % len(editions)
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
		if msg.Y == 1 { // tabs Build | Stop
			if msg.X < width/2 {
				d.ctPane = 0
			} else {
				d.ctPane = 1
			}
			return
		}
		if d.ctPane == 0 {
			// 2-col cards, cardW x cardH, start at y=3 (after tabs+blank)
			col := (msg.X - 1) / cardW
			row := (msg.Y - 3) / cardH
			cols := maxInt(1, (width-2)/cardW)
			idx := row*cols + col
			if idx >= 0 && idx < len(editions) {
				if d.buildSel == idx {
					c.build(editions[idx])
				}
				d.buildSel = idx
			}
			if d.ctBtn.hit(msg.X, msg.Y) {
				c.build(editions[d.buildSel])
			}
		} else {
			if msg.Y == 3 {
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

func (d *designC) ViewCT(width, height int, h help.Model, k keyMap) string {
	bodyH := height - 2
	rows := []string{d.ctTabs(d.ctPane), ""}
	if d.ctPane == 0 {
		cols := maxInt(1, (width-2)/cardW)
		for i, e := range editions {
			rows = append(rows, editionCard(shortEdition(e), i == d.buildSel, cols, i))
		}
		// join cards horizontally in rows of `cols`
		rows = joinCardRows(rows[2:], cols)
		rows = append([]string{d.ctTabs(d.ctPane), ""}, rows...)
		if d.ct.toast != "" {
			rows = append(rows, styleWarn.Render(d.ct.toast))
		}
		btn := button("Build", false)
		rows = append(rows, alignRight(width-1, btn))
		d.ctBtn = rect{x: width - lipgloss.Width(btn), y: 1 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}
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
		rows = append(rows, "")
		if d.ct.toast != "" {
			rows = append(rows, styleWarn.Render(d.ct.toast))
		}
		btn := button("Stop", false)
		rows = append(rows, alignRight(width-1, btn))
		d.ctBtn = rect{x: width - lipgloss.Width(btn), y: 1 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}
	}
	content := lipgloss.NewStyle().Width(width).Height(bodyH).MaxHeight(bodyH).PaddingLeft(1).
		Render(strings.Join(rows, "\n"))
	return shell(width, height, 2, content, h, k)
}

func (d *designC) ctTabs(sel int) string {
	l := lipgloss.NewStyle().Padding(0, 1).Render("Build Container")
	r := lipgloss.NewStyle().Padding(0, 1).Render("Stop Container")
	if sel == 0 {
		l = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1).Render("Build Container")
	} else {
		r = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1).Render("Stop Container")
	}
	return l + r
}

func editionCard(title string, selected bool, cols, idx int) string {
	inner := []string{title, styleMutedAlt.Render("not built (fake)"), ""}
	st := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(cardW - 2).Height(cardH - 2).MaxHeight(cardH)
	if selected {
		st = st.BorderForeground(colAccent)
	} else {
		st = st.BorderForeground(lipgloss.Color("240"))
	}
	return st.Render(strings.Join(inner, "\n"))
}

// joinCardRows takes a flat list of card strings (one per card) and
// joins every `cols` of them horizontally into single rows.
func joinCardRows(cards []string, cols int) []string {
	var rows []string
	for i := 0; i < len(cards); i += cols {
		end := min(i+cols, len(cards))
		rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cards[i:end]...))
	}
	return rows
}

var _ help.Model
