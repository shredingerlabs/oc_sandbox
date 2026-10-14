package settingsview

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// textField renders a text input as the boxed text area (shell.TextFieldBox
// provides the shared input-area look; switches take the sibling helpers):
// focused shows the caret ▌ in an accent box; unfocused shows the current
// value (or the muted "…" placeholder).
func textField(f *field, focused bool) string {
	return shell.TextFieldBox(*f.val, *f.cur, focused)
}

// appendInput appends the boxed text-input block (3 lines: borders + value
// row) to pane rows and returns the content-row index for hit registration.
func appendInput(rows []string, box string) []string {
	for _, l := range strings.Split(box, "\n") {
		rows = append(rows, "  "+l)
	}
	return rows
}

// inputRow is the hit-row index of an appendInput'd box: last-1 (between
// the borders).
func inputRow(rows []string) int { return len(rows) - 2 }

// chooserRow renders a picker row; the selected row carries the ▶ marker.
func chooserRow(label string, selected bool) string {
	if selected {
		return shell.StyleSelectedRow.Render(" ▶ " + label)
	}
	return "   " + shell.StyleMutedAlt.Render(label)
}

// optionRow renders a toggleable option row ([x]/[ ] never rides on color).
func optionRow(label string, on, focused bool) string {
	box := "[ ]"
	if on {
		box = "[x]"
	}
	line := " " + box + " " + label
	switch {
	case focused:
		return shell.StyleSelectedRow.Render(line)
	case on:
		return shell.StyleCurrentVal.Render(line)
	default:
		return shell.StyleMutedAlt.Render(line)
	}
}

// buttonBlock appends a bordered button (uniform shell.Button style) below
// a blank spacer row and returns the button's mid-line hit row.
func buttonBlock(rows []string, label string, focused bool, act func() tea.Cmd) ([]string, hitRow) {
	lines := shell.ButtonLines(label, focused)
	rows = append(rows, "")
	mid := len(rows) + 1
	for _, l := range lines {
		rows = append(rows, "  "+l)
	}
	return rows, hitRow{row: mid, x: cardX, w: lipgloss.Width("  " + lines[1]), act: act}
}

// buttonPair appends two bordered buttons side by side below a blank
// spacer row and returns both mid-line hit rows.
func buttonPair(rows []string, label1 string, focus1 bool, act1 func() tea.Cmd, label2 string, focus2 bool, act2 func() tea.Cmd) ([]string, hitRow, hitRow) {
	b1 := shell.ButtonLines(label1, focus1)
	b2 := shell.ButtonLines(label2, focus2)
	rows = append(rows, "")
	mid := len(rows) + 1
	for i := range b1 {
		rows = append(rows, "  "+b1[i]+"  "+b2[i])
	}
	w1 := lipgloss.Width("  " + b1[1])
	return rows,
		hitRow{row: mid, x: cardX, w: w1, act: act1},
		hitRow{row: mid, x: cardX + w1 + 2, w: lipgloss.Width("  " + b2[1]), act: act2}
}

// plainWidth is the ANSI-stripped width of a pane row.
func plainWidth(row string) int { return shell.Width(shell.StripANSI(row)) }

// focusLabel renders a field label row.
func focusLabel(text string) string { return shell.StyleFieldLabel.Render(text) }

// muted renders explanatory card text.
func muted(text string) string { return shell.StyleMutedAlt.Render(text) }

// chooserRows returns the import picker's chooser row labels: up one
// level, the current directory's subdirs, select this folder.
func (v *View) chooserRows() []string {
	rows := []string{".. (up one level)"}
	rows = append(rows, v.impDirs...)
	rows = append(rows, "Select this folder")
	return rows
}

func truncJoin(labels []string) string { return strings.Join(labels, ", ") }
