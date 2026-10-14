package shell

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Background paint helpers.
//
// A background cannot be applied by wrapping already-rendered content in
// a single lipgloss style: any inner element (card border, menu pill,
// separator) ends its style with an SGR reset, and that reset kills the
// wrapping background for the rest of the line — the visible bug is paint
// stopping at the first element of a line. Instead each line is truncated
// to width, the background sequence is re-established after every inner
// reset, and the line is padded with painted spaces:
//
//	seq <line with resets patched> seq <pad> reset-line
//
// With the Ascii profile (NO_COLOR, golden tests) the sequence is empty
// and the block is only truncated, so rendering stays escape-free.

// paintSeq extracts the escape sequence a style emits: a sentinel is
// rendered and what comes before it is the sequence.
func paintSeq(st lipgloss.Style) string {
	const sentinel = "X"
	r := st.Render(sentinel)
	if i := strings.LastIndex(r, sentinel); i >= 0 {
		return r[:i]
	}
	return ""
}

// paint repaints one line in the background sequence seq.
func paint(line, seq string, width int) string {
	line = ansi.Truncate(line, width, "")
	if seq == "" {
		return line
	}
	line = seq + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+seq)
	if pad := width - ansi.StringWidth(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return line + "\x1b[0m"
}

// paintLines paints every line of a multi-line block under a background.
func paintLines(block string, st lipgloss.Style, width int) string {
	seq := paintSeq(st)
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		lines[i] = paint(line, seq, width)
	}
	return strings.Join(lines, "\n")
}
