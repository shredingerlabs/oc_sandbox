package projectsettings

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Right-pane scroll (AI provider / removal panes): at tight heights the
// pane content scrolls; wheel free-scroll pauses the follow, keyboard
// re-arms it and the focused pane item pulls back into view. The golden
// view tests run at generous heights and stay pinned elsewhere.
func TestAIPaneWheelScrollsAtTightHeight(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	v.Body(76, 12)
	clickCard(t, v, "AI Provider")
	v.Body(76, 12)
	if v.paneScroll != 0 {
		t.Fatalf("initial pane scroll = %d, want 0", v.paneScroll)
	}
	top := paneText(t, v)
	v.Update(tea.MouseMsg{X: 40, Button: tea.MouseButtonWheelDown})
	if v.paneScroll != 3 {
		t.Fatalf("paneScroll = %d, want 3", v.paneScroll)
	}
	if paneText(t, v) == top {
		t.Fatalf("wheel down did not scroll the AI pane:\n%s", top)
	}
	// wheel over the card grid keeps scrolling the grid, not the pane
	gridScroll := v.gridScroll
	v.Update(tea.MouseMsg{X: 5, Button: tea.MouseButtonWheelDown})
	if v.gridScroll == gridScroll {
		t.Fatalf("grid wheel over the left half did not scroll the grid")
	}
	if v.paneScroll != 3 {
		t.Fatalf("grid wheel moved paneScroll to %d", v.paneScroll)
	}
}

// The keyboard navigation pulls the focused pane item into view after a
// wheel scroll paused the follow (openproject parity).
func TestAIPaneFocusPullsAtTightHeight(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	v.Body(76, 12)
	clickCard(t, v, "AI Provider")
	v.Body(76, 12)
	v.Update(tea.MouseMsg{X: 40, Button: tea.MouseButtonWheelDown})
	v.Update(keyMsg("tab")) // focus leaves the grid → focused Name field
	if !findInLines(paneLines(t, v), "Name:") {
		t.Fatalf("focused Name field not visible after scroll+tab:\n%s", paneText(t, v))
	}
	// tab to the Add button: it pulls into the window too
	for {
		v.Update(keyMsg("tab"))
		if v.focus == piAdd {
			break
		}
		if v.focus == 0 {
			t.Fatalf("tab cycling never reached the Add button")
		}
		v.Body(76, 12)
	}
	if !findInLines(paneLines(t, v), "Add") {
		t.Fatalf("focused Add button not visible:\n%s", paneText(t, v))
	}
}

// A fresh pane (open / card switch) starts at the top.
func TestPaneScrollResetsOnOpen(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	v.Body(76, 12)
	clickCard(t, v, "AI Provider")
	v.Update(tea.MouseMsg{X: 40, Button: tea.MouseButtonWheelDown})
	v.Update(tea.MouseMsg{X: 40, Button: tea.MouseButtonWheelDown})
	if v.paneScroll == 0 {
		t.Fatalf("setup: scroll stayed 0")
	}
	// switching cards resets the pane (and its scroll)
	v.Update(clickAt(1, 10))
	v.Body(76, 12)
	clickCard(t, v, "AI Provider")
	if v.paneScroll != 0 {
		t.Fatalf("paneScroll = %d after re-open, want 0", v.paneScroll)
	}
}

// The removal pane scrolls with the same mechanism.
func TestRemovePaneWheelScrollsAtTightHeight(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	v.Body(76, 14)
	clickCard(t, v, "remove")
	v.Body(76, 14)
	top := paneText(t, v)
	v.Update(tea.MouseMsg{X: 40, Button: tea.MouseButtonWheelDown})
	if v.paneScroll != 3 {
		t.Fatalf("paneScroll = %d, want 3", v.paneScroll)
	}
	if paneText(t, v) == top {
		t.Fatalf("wheel down did not scroll the removal pane:\n%s", top)
	}
}

// --- helpers ---

func paneLines(t *testing.T, v *View) []string {
	t.Helper()
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })
	var out []string
	for _, l := range strings.Split(shell.StripANSI(v.Body(76, 12)), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
}

func paneText(t *testing.T, v *View) string {
	return strings.Join(paneLines(t, v), "\n")
}

func findInLines(lines []string, needle string) bool {
	for _, l := range lines {
		if strings.Contains(l, needle) {
			return true
		}
	}
	return false
}
