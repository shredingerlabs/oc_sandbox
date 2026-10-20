package shell

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Guarded exit must not grow the frame: the exitPending warn strip
// replaces the footer line, so the View stays exactly terminal-height
// and the header menu never scrolls off-screen. Regression for the
// "clicking Exit while background tasks run hides the header menu" bug
// (the old render prepended an extra blank line, making the frame one
// row taller than the terminal).
func TestExitPendingFrameStaysTerminalHeight(t *testing.T) {
	root := New(&probe{busy: true})
	root.width, root.height = 100, MinHeight

	exitRect := MenuRects()[len(MenuItems)-1] // Exit is the last menu item
	model, _ := root.Update(tea.MouseMsg{X: exitRect.X, Y: 0,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m := model.(Model)
	if !m.ExitPending() {
		t.Fatal("exit click while busy did not raise the confirmation")
	}

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Fatalf("exitPending view is %d lines, want exactly terminal height %d (header scrolled off)",
			len(lines), m.height)
	}
	// the header menu is still the first row on screen
	if !strings.Contains(lines[0], Title) || !strings.Contains(lines[0], "Exit") {
		t.Fatalf("header menu not on row 0 after guarded exit click: %q", lines[0])
	}
}
