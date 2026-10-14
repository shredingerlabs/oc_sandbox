package shell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// TextFieldMinWidth keeps an empty text-input box readable (one text area
// row minimum, wide enough for a typical path head).
const TextFieldMinWidth = 30

// TextFieldBox renders one text input as a boxed text area: a rounded
// border around the value row (label ownership is the caller's). Focused
// shows the caret ▌ and an accent border; unfocused shows the value, or
// the muted "…" placeholder. Text inputs render through this so every
// form field gets the same input-area look.
func TextFieldBox(val string, cursor int, focused bool) string {
	top, mid, bot := TextFieldBoxLines(val, cursor, focused)
	return top + "\n" + mid + "\n" + bot
}

// TextFieldBoxLines is TextFieldBox as its three lines (top border,
// value row, bottom border) for form layouts that append rows one by
// one and need stable geometry.
func TextFieldBoxLines(val string, cursor int, focused bool) (top, mid, bot string) {
	var inside string
	switch {
	case focused:
		rs := []rune(val)
		if cursor < 0 || cursor > len(rs) {
			cursor = len(rs)
		}
		inside = StyleSelectedRow.Render(string(rs[:cursor]) + "▌" + string(rs[cursor:]))
	case val == "":
		inside = StyleMutedAlt.Render("…")
	default:
		inside = StyleCurrentVal.Render(val)
	}
	inner := max(Width(inside)+2, TextFieldMinWidth) // +2 = inner padding (lipgloss Width includes it)
	border := ColBorder
	if focused {
		border = ColAccent
	}
	lines := strings.Split(lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		Width(inner).
		Padding(0, 1).
		Render(inside), "\n")
	return lines[0], lines[1], lines[2]
}
