package openproject

import (
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
	if e.m.scroll != 0 {
		t.Fatalf("initial scroll = %d, want 0", e.m.scroll)
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
	if e.m.scroll != 0 {
		t.Fatalf("scroll = %d, want 0 at generous height", e.m.scroll)
	}
}
