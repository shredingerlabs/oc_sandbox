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
	np          *npState
	ct          *ctState
	ctPane      int
	buildSel    int
	npBtn       rect
	ctBtn       rect
	boxX        int
	boxW        int
	tabsRects   [2]rect
	fieldRects  []rect
	optionRects []optRect
	ctTabsRects [2]rect
	chipRects   []rect
	stopRects   []rect
	selAll      rect
	dbl         dblClickTracker
}

// optRect is a click box around one option token inside a select row.
type optRect struct {
	fi  int
	val string
	r   rect
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
		// tabs: hit-test the actual tab pills
		for i, tr := range d.tabsRects {
			if tr.hit(msg.X, msg.Y) {
				s.source = i
				s.focus = 0
				d.refresh()
				return
			}
		}
		// fields: stored rects (label+value as one 2-line box); option
		// tokens first so clicking a specific option activates just it
		for _, or := range d.optionRects {
			if or.r.hit(msg.X, msg.Y) {
				id := s.fields[or.fi]
				s.focus = or.fi
				s.setSelect(id, or.val)
				return
			}
		}
		for fi, fr := range d.fieldRects {
			if fr.hit(msg.X, msg.Y) {
				id := s.fields[fi]
				if s.focus == fi && d.nextVal2(id) {
					return
				}
				s.focus = fi
				// caret where clicked, on text fields
				if id == fName || id == fURL || id == fPath {
					pos := msg.X - fr.x
					s.cursor = min(maxInt(0, pos), len([]rune(s.value(id))))
				}
				return
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
	switch id {
	case fName, fURL, fPath:
		// editable text; Enter is a no-op (arrow keys move the caret)
		return
	case fCreate:
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
	d.optionRects = nil
	x0 := 1 // pinned top-left under the header, like the card grids
	d.boxX = x0
	rows := []string{"", d.dialogTabs(s.source)}
	tabL, tabR := npTab("Blank project", s.source == 0), npTab("From URL", s.source == 1)
	wL := lipgloss.Width(tabL)
	d.tabsRects = [2]rect{{y: 3, w: wL, h: 1}, {y: 3, w: lipgloss.Width(tabR), h: 1}}
	rows[1] = tabL + tabR
	nf := 0
	for _, id := range s.fields {
		if id == fCreate {
			continue
		}
		rows = append(rows, styleMutedAlt.Render(fieldLabel(id)), d.dlgValue(id, nf))
		nf++
	}
	// toast row is always reserved so hit rects never shift
	rows = append(rows, styleWarn.Render(s.toast))
	btn := button("Create Project", s.fields[s.focus] == fCreate)
	rows = append(rows, btn) // right-aligned after the card width is known

	// card as wide as the widest content (no "…" truncation)
	maxw := 0
	for _, r := range rows {
		maxw = maxInt(maxw, lipgloss.Width(r))
	}
	boxW := maxw + 4
	// tab/field/button rects in screen space (content x = x0+2)
	d.tabsRects[0].x = x0 + 2
	d.tabsRects[1].x = x0 + 2 + wL
	d.fieldRects = d.fieldRects[:0]
	nf = 0
	for _, id := range s.fields {
		if id == fCreate {
			continue
		}
		d.fieldRects = append(d.fieldRects, rect{x: x0 + 2, y: 4 + 2*nf, w: boxW - 4, h: 2})
		nf++
	}
	btnX := x0 + 2 + (boxW - 4) - lipgloss.Width(btn)
	d.npBtn = rect{x: btnX, y: 2 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}
	rows[len(rows)-1] = alignRight(maxw, btn)

	inner := lipgloss.NewStyle().Width(boxW - 2).Padding(0, 1).Render(strings.Join(rows, "\n"))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).
		Width(boxW).MaxHeight(bodyH).Render(inner)
	body := padLeft(x0, box)
	// anchor to the top: fill the rest of the body AFTER the box, so the
	// shell's bottom-anchored padding can't push the dialog down
	if fill := bodyH - lipgloss.Height(body); fill > 0 {
		body += strings.Repeat("\n", fill)
	}
	body = lipgloss.NewStyle().Width(width).MaxWidth(width).Render(body)
	return shell(width, height, 1, body, h, k)
}

func npTab(label string, selected bool) string {
	st := lipgloss.NewStyle().Padding(0, 1)
	if selected {
		return st.Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true).Render(label)
	}
	return st.Render(label)
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

// dlgValue renders a field's value line; select fields render ALL options
// in fixed order — ● current, ○ others, no reordering, no "·" separators —
// and record a click rect per option token.
func (d *designC) dlgValue(id, fi int) string {
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
		if focused {
			rs := []rune(val)
			if s.cursor > len(rs) {
				s.cursor = len(rs)
			}
			return styleCurrentVal.Render(string(rs[:s.cursor]) + "▌" + string(rs[s.cursor:]))
		}
		return styleMutedAlt.Render(val)
	default:
		// value line: every option, fixed order; current gets ●
		var toks []string
		off := 0
		y := 4 + 2*fi + 1 // value line row in screen space
		for _, opt := range optionList(id) {
			var tok string
			if opt == val {
				tok = styleCurrentVal.Render("● " + opt)
			} else {
				tok = styleMutedAlt.Render("○ " + opt)
			}
			tw := lipgloss.Width("● " + opt)
			d.optionRects = append(d.optionRects, optRect{
				fi: fi, val: opt,
				r: rect{x: d.boxX + 2 + off, y: y, w: tw, h: 1},
			})
			toks = append(toks, tok)
			off += tw + 2
		}
		line := strings.Join(toks, strings.Repeat(" ", 2))
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
		n := d.dbl.press(msg)
		// tabs Build | Stop: hit-test the actual tab pills
		for i, tr := range d.ctTabsRects {
			if tr.hit(msg.X, msg.Y) {
				d.ctPane = i
				return
			}
		}
		if d.ctPane == 0 {
			// cards: stored rects; FIRST click selects, DOUBLE-click builds
			for i, cr := range d.chipRects {
				if cr.hit(msg.X, msg.Y) {
					if n == 2 && d.buildSel == i {
						c.build(editions[i])
					} else {
						d.buildSel = i
					}
					return
				}
			}
			if d.ctBtn.hit(msg.X, msg.Y) {
				c.build(editions[d.buildSel])
			}
		} else {
			if d.selAll.hit(msg.X, msg.Y) {
				c.toggleAll(running)
			}
			for i, rr := range d.stopRects {
				if rr.hit(msg.X, msg.Y) {
					c.selection[running[i].Name] = !c.selection[running[i].Name]
					return
				}
			}
			if d.ctBtn.hit(msg.X, msg.Y) {
				c.stop(running)
			}
		}
	}
}

func (d *designC) ViewCT(width, height int, h help.Model, k keyMap) string {
	bodyH := height - 2
	// align the Build/Stop sub-menu under "Open Project" in the top menu:
	// the header starts with the padded "oc-sandbox" title, so xTabs is its
	// printed width. PaddingLeft(1) already puts content at x=1; prepend the
	// difference to the tab row only.
	xTabs := lipgloss.Width(styleHeaderTitle.Render("oc-sandbox"))
	tabs := strings.Repeat(" ", xTabs-1) + d.ctTabs(d.ctPane)
	d.ctTabsRects = [2]rect{
		{x: xTabs, y: 1, w: lipgloss.Width("Build Container") + 2, h: 1},
		{x: xTabs + lipgloss.Width("Build Container") + 2, y: 1, w: lipgloss.Width("Stop Container") + 2, h: 1},
	}
	rows := []string{tabs, ""}
	rows = append(rows, d.ctContent(width)...)
	// toast row always reserved so stored rects stay stable
	rows = append(rows, styleWarn.Render(d.ct.toast))
	label := "Build"
	if d.ctPane == 1 {
		label = "Stop"
	}
	btn := button(label, false)
	rows = append(rows, alignRight(width-1, btn))
	d.ctBtn = rect{x: width - lipgloss.Width(btn), y: 1 + len(rows) - 1, w: lipgloss.Width(btn), h: 3}
	content := lipgloss.NewStyle().Width(width).Height(bodyH).MaxHeight(bodyH).PaddingLeft(1).
		Render(strings.Join(rows, "\n"))
	return shell(width, height, 2, content, h, k)
}

// ctContent renders the Build (cards) or Stop (checklist) pane and records
// its hit rects.
func (d *designC) ctContent(width int) []string {
	var rows []string
	d.chipRects = nil
	d.stopRects = nil
	if d.ctPane == 0 {
		cols := maxInt(1, (width-2)/cardW)
		var cards []string
		for i, e := range editions {
			cards = append(cards, editionCard(shortEdition(e), i == d.buildSel))
		}
		for r := 0; r*cols < len(cards); r++ {
			end := min((r+1)*cols, len(cards))
			rowCards := cards[r*cols:end]
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, rowCards...))
			for c := range rowCards {
				d.chipRects = append(d.chipRects, rect{x: 1 + c*cardW, y: 3 + r*cardH, w: cardW, h: cardH})
			}
		}
	} else {
		running := fakeRunning()
		box := "☐"
		if d.ct.allSelected(running) {
			box = "☒"
		}
		rows = append(rows, " "+box+" select all")
		d.selAll = rect{x: 1, y: 3, w: 2 + lipgloss.Width("select all"), h: 1}
		for i, p := range running {
			box = "☐"
			if d.ct.selection[p.Name] {
				box = "☒"
			}
			rows = append(rows, " "+box+" "+p.Name+"  "+styleStatusRun.Render("● running"))
			d.stopRects = append(d.stopRects, rect{x: 1, y: 4 + i, w: width - 2, h: 1})
		}
	}
	return rows
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

func editionCard(title string, selected bool) string {
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

var _ help.Model
