package newproject

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Body geometry: the card is pinned top-left with its own left padding.
// The body lines start at screen row 3; field row i lands on screen
// row 4+i (card border line + i). Hit rects are recorded in screen space
// during every render so clicks never lag the view.
const (
	BodyPad       = 2  // left padding the view adds to its card
	CardInset     = 2  // card border (1) + inner padding (1) before content
	cardFirstRowY = 4  // body line 4 = first row inside the card border
	labelWidth    = 16 // "▸ Git user.email" cell width (marker + widest label)
	createBtn     = "Create Project"
)

// fieldHit records one form-row's screen-space rect for mouse hits.
type fieldHit struct {
	idx  int // index into m.fields
	valX int // x of the value column (for caret positioning)
	rect shell.Rect
}

// fieldLabel returns the row label of a field id.
func fieldLabel(f int) string {
	switch f {
	case fName:
		return "Project name"
	case fPath:
		return "Project path"
	case fURL:
		return "Repo URL"
	case fEdition:
		return "Edition"
	case fBuild:
		return "Image build"
	case fAgent:
		return "Agent"
	case fModes:
		return "Modes"
	case fStart:
		return "Start"
	case fSearch:
		return "Search"
	case fVCS:
		return "VCS tracking"
	case fGitName:
		return "Git user.name"
	case fGitEmail:
		return "Git user.email"
	case fVCSToken:
		return "VCS token"
	case fVCSHost:
		return "VCS host"
	case fAI:
		return "AI provider"
	case fAIName:
		return "Provider name"
	case fAIEndpoint:
		return "Provider endpoint"
	case fAIToken:
		return "Provider token"
	case fPrompt:
		return ""
	case fCreate:
		return createBtn
	}
	return ""
}

// HasToast reports whether a toast message is currently displayed.
func (m *Model) HasToast() bool { return m.toast != "" }

// ToastText returns the current toast message ("" when none).
func (m *Model) ToastText() string { return m.toast }

// fullRows builds the unwindowed card rows (body-relative geometry) and
// the Create button rect.
func (m *Model) fullRows(width int) ([]string, shell.Rect) {
	m.optRects = nil
	m.fieldRect = map[int]fieldHit{}
	m.btnRect = shell.Rect{}

	valX := BodyPad + CardInset + labelWidth + 1
	rows := []string{}
	y := cardFirstRowY // first field row (card border at row 0)

	for i, f := range m.fields {
		focused := m.focus == i
		switch f {
		case fCreate:
			continue // button rendered below the toast row
		case fPrompt:
			rows = append(rows, shell.StyleWarn.Render("Credential files already exist — keep or replace?"))
			y++
			ls := m.optionLines(fPrompt, m.promptVals(), y, width-BodyPad-CardInset-1, &m.optRects)
			rows = append(rows, ls...)
			y += len(ls) - 1
			continue
		}

		cell := "  " + fieldLabel(f)
		if focused {
			cell = "▸ " + fieldLabel(f)
		}
		labelCell := shell.StyleFieldLabel.Render(padCell(cell))
		if optOpts := m.optionsFor(f); optOpts != nil {
			// Option rows wrap onto continuation lines when their tokens
			// exceed the terminal budget, so a long option never widens
			// the card past the standard 80-column window.
			ls := m.optionLines(f, optOpts, y, width-BodyPad-CardInset-1, &m.optRects)
			rows = append(rows, ls...)
			y += len(ls) - 1
		} else {
			// text fields render as a boxed text area: label row, then the
			// 3-line input box (border / value / border)
			rows = append(rows, labelCell)
			val, cur := windowValue(m.textVal(f), m.cursor, width, focused)
			top, content, bottom := shell.TextFieldBoxLines(val, cur, focused)
			yTop := y + 1 // box top border
			rows = append(rows, top, content, bottom)
			y += 3 // label + top border + content consumed; bottom counts as its own row below
			contentY := yTop + 1
			// value column: box border + inner padding (column 0 = card content)
			m.fieldRect[contentY] = fieldHit{idx: i, valX: BodyPad + CardInset + 2, rect: shell.Rect{X: BodyPad + CardInset, Y: contentY, W: width - BodyPad - CardInset, H: 1}}
			y++ // bottom border row
			continue
		}
		m.fieldRect[y] = fieldHit{idx: i, valX: valX, rect: shell.Rect{X: BodyPad + CardInset, Y: y, W: width - BodyPad - CardInset, H: 1}}
		y++
	}

	rows = append(rows, shell.Toast(m.toast))
	y++

	btnLabel := createBtn
	if m.fields[m.focus] == fCreate {
		btnLabel = "▸ " + createBtn
	}
	lines := shell.ButtonLines(btnLabel, m.fields[m.focus] == fCreate)
	w := 0
	for _, r := range rows {
		if cw := shell.Width(r); cw > w {
			w = cw
		}
	}
	var btn []string
	for _, l := range lines {
		btn = append(btn, shell.AlignRight(w, l))
	}
	btnX := BodyPad + CardInset + w - shell.Width(btn[1])
	btnY := y + 1
	for _, l := range btn {
		rows = append(rows, l)
	}
	m.btnRow = btnY
	return rows, shell.Rect{X: btnX, Y: btnY, W: shell.Width(btn[1]), H: 1}
}

// focusFirstRow returns the body-relative row of the focused field
// (its smallest recorded label/token/button row), or -1 when the field
// has no recorded rect.
func (m *Model) focusFirstRow() int {
	best := -1
	f := m.fields[m.focus]
	if f == fCreate && m.btnRow > 0 {
		best = m.btnRow // the Create button's unwindowed row
	}
	for _, fh := range m.fieldRect {
		if m.fields[fh.idx] != f {
			continue
		}
		if best < 0 || fh.rect.Y < best {
			best = fh.rect.Y
		}
	}
	for _, or := range m.optRects {
		if or.field != f {
			continue
		}
		if best < 0 || or.r.Y-1 < best { // label row sits one above the token row
			best = or.r.Y - 1
		}
	}
	return best
}

// padCell zero-pads a label cell to the label column width.
func padCell(cell string) string {
	if n := labelWidth - shell.Width(cell); n > 0 {
		return cell + strings.Repeat(" ", n)
	}
	return cell
}

// windowValue clips a long text value to the input-box budget so the
// card stays inside the terminal: the box adds 4 columns (border +
// padding) beyond the value and the card another 4 (border + inner
// padding) atop the body pad — an unclamped long token/URL widened the
// card past the terminal and the frame wrapped apart. Values longer
// than the budget show their tail; while focused the window slides with
// the caret (caret cell budgeted) so newly typed characters stay
// visible. Returns the windowed value and the caret offset within it.
func windowValue(val string, cursor int, width int, focused bool) (string, int) {
	w := max(1, width-BodyPad-CardInset-6)
	rs := []rune(val)
	if !focused {
		if len(rs) > w {
			val, cursor = string(rs[len(rs)-w:]), max(0, cursor-(len(rs)-w))
		}
		return val, cursor
	}
	budget := max(0, w-1) // the caret cell eats one column
	if len(rs) > budget {
		s := max(0, min(cursor-budget+1, len(rs)-budget))
		return string(rs[s : s+budget]), cursor - s
	}
	return val, cursor
}

// optionsFor lists the option tokens of a non-text field, or nil for
// text fields. Editions/modes are detected fresh at each render.
func (m *Model) optionsFor(f int) []string {
	switch f {
	case fEdition:
		return m.editions()
	case fBuild:
		return buildOptions
	case fModes:
		return m.modeOptions()
	case fStart:
		return startOptions
	case fSearch:
		return searchOptions
	case fAgent:
		return agentOptions
	case fVCS:
		return vcsOptions
	case fAI:
		return aiOptions
	}
	return nil
}

// renderOptionToken renders one option token ("● current ○ alt").
func (m *Model) renderOptionToken(f int, o string) (string, int) {
	cur := m.optionIsCurrent(f, o)
	disp := o
	if f == fSearch && o == sandboxcfg.SearchSearxng {
		disp = "SearXNG" // ADR 0026 row: display capitalization
	}
	sym := shell.SymOff
	st := shell.StyleMutedAlt
	if cur {
		sym = shell.SymOn
		st = shell.StyleCurrentVal
	}
	tok := sym + " " + disp
	return st.Render(tok), shell.Width(tok)
}

// lineLabel renders the label cell of an option row: the real label on
// the first line, a blank cell on continuation lines (styled like the
// other option-row labels, shell.StyleFieldLabel).
func (m *Model) lineLabel(f int, first bool) string {
	var cell string
	if first {
		cell = "  " + fieldLabel(f)
		if len(m.fields) > 0 && m.fields[m.focus] == f {
			cell = "▸ " + fieldLabel(f)
		}
	}
	return shell.StyleFieldLabel.Render(padCell(cell))
}

// optionLines renders the option tokens of a field, wrapping onto
// continuation lines (blank label cell) when the joined line would push
// the card wider than budget, so a long option list never widens the
// card past the terminal. One hit rect per token is recorded into rects
// at its actual row.
func (m *Model) optionLines(f int, opts []string, y int, budget int, out *[]optRect) []string {
	valX := BodyPad + CardInset + labelWidth + 1
	var lines []string
	var parts []string
	rowY := y
	w := shell.Width(m.lineLabel(f, true)) + 1 // incl. separator space
	x := valX
	for i, o := range opts {
		styled, tw := m.renderOptionToken(f, o)
		if i == 0 {
			x = valX
		} else if w+tw+1 > budget { // +1: trailing separator space
			lines = append(lines, m.lineLabel(f, rowY == y)+" "+strings.Join(parts, " "))
			parts = nil
			rowY++
			w = shell.Width(m.lineLabel(f, false)) + 1
			x = valX
		}
		parts = append(parts, styled)
		*out = append(*out, optRect{field: f, val: o, r: shell.Rect{X: x, Y: rowY, W: tw, H: 1}})
		x += tw + 1
		w += tw + 1
	}
	if len(parts) > 0 {
		lines = append(lines, m.lineLabel(f, rowY == y)+" "+strings.Join(parts, " "))
	}
	return lines
}

// optionIsCurrent reports whether an option token is the current (●)
// value of its field. Modes are multi-select: "none" is current when the
// selection is empty, otherwise every toggled mode is.
func (m *Model) optionIsCurrent(f int, o string) bool {
	switch f {
	case fPrompt:
		vals := m.promptVals()
		return m.choice >= 0 && m.choice < len(vals) && vals[m.choice] == o
	case fModes:
		if o == "none" {
			return len(m.modes) == 0
		}
		return indexOf(m.modes, o) >= 0
	case fEdition:
		return m.edition == o
	case fBuild:
		cur := buildOptions[1]
		if m.buildNow {
			cur = buildOptions[0]
		}
		return o == cur
	case fStart:
		return m.start == o
	case fSearch:
		return m.searchMode == o
	case fAgent:
		return m.agent == o
	case fVCS:
		return m.vcs == o
	case fAI:
		return m.ai == o
	}
	return false
}

// Body implements shell.CenterView: the merged single view card. Editions
// and modes are detected fresh at each render; hit rects are recorded in
// screen space. When rows exceed height the card scrolls: rows outside
// the scroll window are dropped and all recorded rects shift up by the
// scroll offset.
func (m *Model) Body(width, height int) string {
	// The streamed create-chain pane rides below the card while an op
	// runs (spinner head + live output); after a failure it stays
	// readable next to the failure toast.
	showPane := m.paneVisible()
	rows, btnRect := m.fullRows(width)
	// Scroll bookkeeping: the follow pulls the focused row into the
	// window (wheel free-scroll pauses it — the follow only fires when
	// the focused field moved since the last pass), windowed rows sit
	// inside the card border (it consumes 2 rows of the body region).
	// While the pane shows, the card yields ~8 body rows to it.
	avail := max(1, height-2)
	if showPane {
		avail = max(3, avail-8)
	}
	m.follow.Pull(&m.npScroll, m.focus, m.focusFirstRow(), len(rows), avail)
	end := min(len(rows), m.npScroll+avail)
	vis := rows[m.npScroll:end]

	// Shift every recorded rect up by the scroll offset and drop off-
	// window rects (rects keep their body-relative Y so clicks stay
	// body-line-mapped).
	off := m.npScroll
	var keep []optRect
	for _, or := range m.optRects {
		or.r.Y -= off
		if or.r.Y >= cardFirstRowY && or.r.Y < cardFirstRowY+avail {
			keep = append(keep, or)
		}
	}
	m.optRects = keep
	fields := map[int]fieldHit{}
	for rowY, fh := range m.fieldRect {
		fh.rect.Y -= off
		if rowY-off >= cardFirstRowY && rowY-off < cardFirstRowY+avail {
			fields[rowY-off] = fh
		}
	}
	m.fieldRect = fields
	btnRect.Y -= off
	if btnRect.W > 0 && !(btnRect.Y >= cardFirstRowY && btnRect.Y < cardFirstRowY+avail) {
		btnRect = shell.Rect{}
	}
	m.btnRect = btnRect

	card := shell.Card(vis)
	body := shell.PadLeft(BodyPad, card)
	if m.confirm.IsOpen() { // the popup overlays the form card (ADR-0021)
		body = m.confirm.Overlay(width, height, shell.BodyTop)
	} else if showPane {
		if rest := height - lipgloss.Height(card); rest > 1 {
			m.log = m.log.SetSize(width-BodyPad, rest)
			body = body + "\n" + shell.PadLeft(BodyPad, m.log.View())
		}
	}
	return body
}
