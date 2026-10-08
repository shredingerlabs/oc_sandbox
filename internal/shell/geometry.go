package shell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Rect is a screen-space hit box (x/y are 0-based cell coordinates).
type Rect struct{ X, Y, W, H int }

// Hit reports whether the cell (x, y) falls inside the rect.
func (r Rect) Hit(x, y int) bool {
	return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H
}

// Width returns the printed cell width of s (ANSI-aware).
func Width(s string) int { return lipgloss.Width(s) }

// AlignRight right-aligns a (possibly multiline) block in n columns.
func AlignRight(n int, s string) string {
	return lipgloss.NewStyle().Width(n).Align(lipgloss.Right).Render(s)
}

// PadLeft prefixes every line of s with n spaces (0 is a no-op).
func PadLeft(n int, s string) string {
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

// StripANSI removes escape sequences so tests can assert on plain text.
func StripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
