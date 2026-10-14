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
	BodyPad    = 2  // left padding the view adds to its card
	CardInset  = 2  // card border (1) + inner padding (1) before content
	labelWidth = 16 // "▸ Git user.email" cell width (marker + widest label)
	createBtn  = "Create Project"
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
	case fGWDGToken:
		return "GWDG token"
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

// Body implements shell.CenterView: the merged single view card. Editions
// and modes are detected fresh at each render; hit rects are recorded in
// screen space.
func (m *Model) Body(width, height int) string {
	m.optRects = nil
	m.fieldRect = map[int]fieldHit{}
	m.btnRect = shell.Rect{}

	valX := BodyPad + CardInset + labelWidth + 1
	rows := []string{}
	y := 4 // screen row of the first field row (card border at body line 0)

	for i, f := range m.fields {
		focused := m.focus == i
		switch f {
		case fCreate:
			continue // button rendered below the toast row
		case fPrompt:
			q := ""
			switch m.phase {
			case phaseBuild:
				q = "Build the " + m.edition + " image now?"
			case phaseConfirm:
				q = "Credential files already exist — keep or replace?"
			}
			rows = append(rows, shell.StyleWarn.Render(q))
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

	if m.phase == phaseForm {
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
		m.btnRect = shell.Rect{X: btnX, Y: btnY, W: shell.Width(btn[1]), H: 1}
	}

	card := shell.Card(rows)
	return shell.PadLeft(BodyPad, card)
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
	case fStart:
		return m.start == o
	case fVCS:
		return m.vcs == o
	case fAI:
		return m.ai == o
	}
	return false
}
