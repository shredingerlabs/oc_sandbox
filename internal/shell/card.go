package shell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Card renders the universal Variant C frame (approved record #72/#71):
// a rounded accent border around rows, pinned top-left by the caller,
// width set by the widest row. Callers keep the toast row (Toast) always
// present — possibly empty — so recorded hit rects never shift.
func Card(rows []string) string {
	maxw := 0
	for _, r := range rows {
		maxw = max(maxw, Width(r))
	}
	inner := lipgloss.NewStyle().Width(maxw + 2).Padding(0, 1).Render(strings.Join(rows, "\n"))
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(ColAccent).Render(inner)
}

// Toast renders a toast line; pass "" for the reserved-but-empty row so
// the card keeps a stable line count.
func Toast(text string) string { return StyleWarn.Render(text) }

// Button renders the uniform bordered button (focused: accent-filled
// inner row). Rendered as one line by default; callers that need a
// 3-line block with stable geometry use ButtonLines.
func Button(label string, focused bool) string {
	st := lipgloss.NewStyle().Padding(0, 2)
	if focused {
		st = st.Background(ColAccent).Foreground(lipgloss.Color("0")).Bold(true).
			Width(Width(label) + 6).Align(lipgloss.Center)
	} else {
		st = st.Foreground(ColCardFg).Border(lipgloss.RoundedBorder()).BorderForeground(ColBorder)
	}
	return st.Render(label)
}

// ButtonLines renders the bordered button as exactly 3 lines so focus
// cannot change the block geometry; the middle line is the hit row.
func ButtonLines(label string, focused bool) []string {
	lines := strings.Split(Button(label, false), "\n")
	if focused {
		inner := lipgloss.NewStyle().Background(ColAccent).Foreground(lipgloss.Color("0")).Bold(true).
			Width(Width(label) + 4).Align(lipgloss.Center).Render(label)
		lines[1] = "│" + inner + "│"
	}
	return lines
}
