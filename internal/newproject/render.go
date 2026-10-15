package newproject

import (
	"strings"

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
	case fModes:
		return "Modes"
	case fStart:
		return "Start"
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
		return "AI name"
	case fAIEndpoint:
		return "AI endpoint"
	case fAIToken:
		return "AI token"
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
			toks := m.promptVals()
			rows = append(rows, shell.StyleFieldLabel.Render(padCell(""))+" "+m.optionRow(fPrompt, toks, y, &m.optRects))
			y++
			continue
		}

		cell := "  " + fieldLabel(f)
		if focused {
			cell = "▸ " + fieldLabel(f)
		}
		labelCell := shell.StyleFieldLabel.Render(padCell(cell))
		if optOpts := m.optionsFor(f); optOpts != nil {
			rows = append(rows, labelCell+" "+m.optionRow(f, optOpts, y, &m.optRects))
		} else {
			// text fields render as a boxed text area: label row, then the
			// 3-line input box (border / value / border)
			rows = append(rows, labelCell)
			top, content, bottom := shell.TextFieldBoxLines(m.textVal(f), m.cursor, focused)
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
	case fVCS:
		return vcsOptions
	case fAI:
		return aiOptions
	}
	return nil
}

// optionRow renders "● current ○ alt …" and records one hit rect per
// token into rects.
func (m *Model) optionRow(f int, opts []string, y int, out *[]optRect) string {
	var parts []string
	x := BodyPad + CardInset + labelWidth + 1
	for _, o := range opts {
		cur := m.optionIsCurrent(f, o)
		sym := shell.SymOff
		st := shell.StyleMutedAlt
		if cur {
			sym = shell.SymOn
			st = shell.StyleCurrentVal
		}
		tok := sym + " " + o
		parts = append(parts, st.Render(tok))
		*out = append(*out, optRect{field: f, val: o, r: shell.Rect{X: x, Y: y, W: shell.Width(tok), H: 1}})
		x += shell.Width(tok) + 1
	}
	return strings.Join(parts, " ")
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
	rows, btnRect := m.fullRows(width)
	// Scroll bookkeeping: the follow pulls the focused row into the
	// window (wheel free-scroll pauses it — the follow only fires when
	// the focused field moved since the last pass), windowed rows sit
	// inside the card border (it consumes 2 rows of the body region).
	avail := max(1, height-2)
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
	}
	return body
}
