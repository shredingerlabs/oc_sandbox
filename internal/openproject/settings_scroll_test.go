package openproject

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Settings-pane scroll at tight heights (#77): the pane scrolls so the
// keyboard-focused row (sections and the Save button) stays visible;
// the golden snapshots run at generous sizes and stay pinned elsewhere.
func TestSettingsPaneScrollsToFocusedRowAtTightHeight(t *testing.T) {
	e := newEnv(t, &runnerHooks{psOut: ""})
	const w, h = 100, 12
	e.m.Body(w, h)
	if e.m.pscroll != 0 {
		t.Fatalf("initial scroll = %d, want 0", e.m.pscroll)
	}
	e.m.Update(tea.KeyMsg{Type: tea.KeyTab}) // focus the settings pane
	for i := 0; i < len(e.m.navRows)-1; i++ {
		e.m.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	if e.m.cursor != len(e.m.navRows)-1 {
		t.Fatalf("cursor = %d, want the last nav row (Save)", e.m.cursor)
	}
	lines := bodyOf(t, e.m, w, h)
	if !someLine(lines, "Save Settings") {
		t.Fatalf("Save button not visible at tight height after scrolling:\n%s", strings.Join(lines, "\n"))
	}
	// the focused Save row is itself on screen: activating it saves
	e.m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if e.m.toast != "Settings saved — apply on next start" {
		t.Fatalf("Save activation missing: toast %q", e.m.toast)
	}
}

func TestSettingsPaneStartsAtTopAtGenerousHeight(t *testing.T) {
	e := newEnv(t, &runnerHooks{psOut: ""})
	bodyOf(t, e.m, bodyW, bodyH)
	if e.m.pscroll != 0 {
		t.Fatalf("scroll = %d, want 0 at generous height", e.m.pscroll)
	}
}

// TestSettingsPaneWheelScrolls: wheel messages free-scroll the pane;
// later cursor movement re-engages the focus pull.
func TestSettingsPaneWheelScrolls(t *testing.T) {
	e := newEnv(t, &runnerHooks{psOut: ""})
	const w, h = 100, 12
	top := strings.Join(bodyOf(t, e.m, w, h), "\n")
	e.m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	scrolled := strings.Join(bodyOf(t, e.m, w, h), "\n")
	if scrolled == top {
		t.Fatalf("wheel down did not scroll the pane:\n%s", scrolled)
	}
	if e.m.pscroll != 3 {
		t.Fatalf("pscroll = %d, want 3", e.m.pscroll)
	}
	// moving the cursor re-engages the focus pull (up back toward 0)
	e.m.Update(tea.KeyMsg{Type: tea.KeyTab})
	e.m.Update(tea.KeyMsg{Type: tea.KeyUp})
	if got := strings.Join(bodyOf(t, e.m, w, h), "\n"); got == scrolled {
		t.Fatalf("cursor move did not re-engage follow:\n%s", got)
	}
}

// TestGridWheelScrollsAtSmallBody: wheel down over the card grid pulls
// later rows in when the body is too small; later selection moves re-engage.
func TestGridWheelScrollsAtSmallBody(t *testing.T) {
	e := newEnv(t, &runnerHooks{psOut: ""})
	defer func() {
		for _, p := range e.projects(t) {
			_ = p
		}
	}()
	const w, h = 100, 10
	for i := 0; i < 5; i++ {
		e.addProject(t, fmt.Sprintf("proj%d", i), "base", "console")
	}
	top := strings.Join(bodyOf(t, e.m, w, h), "\n")
	e.m.Update(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	got := strings.Join(bodyOf(t, e.m, w, h), "\n")
	if got == top {
		t.Fatalf("wheel down did not scroll the grid:\n%s", got)
	}
	if e.m.gridScroll <= 0 {
		t.Fatalf("gridScroll = %d, want > 0", e.m.gridScroll)
	}
}
