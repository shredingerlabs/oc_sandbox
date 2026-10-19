package projectsettings

import (
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Tab core: the screen presents itself as the "Edit Project settings"
// sub-menu tab of the Settings center.
func TestTabsShowEditProjectSettings(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	items, sel := v.SubTabs()
	if len(items) != 1 || items[0] != "Edit Project settings" || sel != 0 {
		t.Fatalf("SubTabs() = %v, %d", items, sel)
	}
}

// The registered projects render as cards; the open project (last used)
// is preselected: its card carries the selected-card frame and the vcs /
// AI Provider button hit rects (unselected cards render the same labels
// muted and they merely select — card-grid idiom).
func TestOpenProjectPreselected(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	lines := bodyLines(t, v)
	if !someLine(lines, "alpha") || !someLine(lines, "beta") {
		t.Fatalf("project cards missing:\n%s", join(lines))
	}
	if n := countLines(lines, "vcs  AI Provider"); n != 2 {
		t.Fatalf("AI Provider on %d cards, want 2 (one per card):\n%s", n, join(lines))
	}
	// Preselected card = the open project's (first card): its button row.
	if i, _ := findLine(lines, "vcs  AI Provider"); i != 4 {
		t.Fatalf("button row at line %d, want 4 (first card)", i)
	}
	if got := v.Rects(); len(got) != 3 || got[0].Y != 7 {
		t.Fatalf("button rects not on the first card: %+v", got)
	}
}

// Button hit rects pin the regression geometry: one card per column at
// width 76, buttons on the selected card's content row 4.
func TestButtonRectsGeometry(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	v.Body(76, 26)
	want := []shell.Rect{
		{X: 3, Y: 7, W: 3, H: 1},
		{X: 8, Y: 7, W: 11, H: 1},
		{X: 21, Y: 7, W: 6, H: 1},
	}
	if got := v.Rects(); !rectsEqual(got, want) {
		t.Fatalf("rects = %+v, want %+v", got, want)
	}
}

// The right pane starts empty with the hint.
func TestPaneStartsEmpty(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	lines := bodyLines(t, v)
	if !someLine(lines, "select a card, then vcs, AI Provider") {
		t.Fatalf("empty-pane hint missing:\n%s", join(lines))
	}
}

// Clicking a card selects it and resets the right pane to the hint.
func TestCardClickSelectsAndResetsPane(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	v.Body(76, 26)
	// open the pane on alpha first, then switch cards
	clickCard(t, v, "vcs")
	if !someLine(bodyLines(t, v), "VCS — alpha") {
		t.Fatalf("vcs pane not open:\n%s", join(bodyLines(t, v)))
	}

	v.Update(clickAt(1, 10))
	lines := bodyLines(t, v)
	if !someLine(lines, "select a card, then vcs, AI Provider") {
		t.Fatalf("pane not reset to hint after card switch:\n%s", join(lines))
	}
	// Selection moved to beta: its card now holds the button rects.
	if got := v.Rects(); len(got) != 3 || got[0].Y != 14 {
		t.Fatalf("button rects not on beta's card: %+v", got)
	}
}

// Clicking vcs / AI Provider opens the right pane's placeholder for the
// selected project.
func TestButtonOpensPane(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	v.Body(76, 26)
	clickCard(t, v, "vcs")
	if !someLine(bodyLines(t, v), "VCS — alpha") {
		t.Fatalf("vcs pane missing:\n%s", join(bodyLines(t, v)))
	}
	clickCard(t, v, "AI Provider")
	if !someLine(bodyLines(t, v), "AI Provider — alpha") {
		t.Fatalf("AI provider pane missing:\n%s", join(bodyLines(t, v)))
	}
}

// Button clicks on an unselected card merely select it (pane stays empty).
func TestUnselectedButtonOnlySelects(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	v.Body(76, 26)
	v.Update(clickAt(1, 10)) // beta card
	if !someLine(bodyLines(t, v), "select a card, then vcs, AI Provider") {
		t.Fatalf("pane should stay empty on mere selection")
	}
	clickCard(t, v, "vcs")
	if !someLine(bodyLines(t, v), "VCS — beta") {
		t.Fatalf("vcs pane for beta missing:\n%s", join(bodyLines(t, v)))
	}
}

// Keyboard parity: arrows move the selection, pane resets on switch.
func TestKeyboardMovesSelection(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	v.Body(76, 26)
	clickCard(t, v, "vcs") // pane open on alpha
	v.Update(keyMsg("right"))
	lines := bodyLines(t, v)
	if !someLine(lines, "select a card, then vcs, AI Provider") {
		t.Fatalf("pane not reset after keyboard selection:\n%s", join(lines))
	}
	if got := v.Rects(); len(got) != 3 || got[0].Y != 14 {
		t.Fatalf("selection did not move to beta: %+v", got)
	}
	v.Update(keyMsg("left"))
	bodyLines(t, v) // re-render so the rects reflect the selection
	if got := v.Rects(); len(got) != 3 || got[0].Y != 7 {
		t.Fatalf("selection did not move back to alpha: %+v", got)
	}
}

// Tab cycles focus between the card grid and the pane; the pane has no
// activatable items yet, so tab returns to the grid.
func TestTabCyclesFocus(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	v.Body(76, 26)
	v.Update(keyMsg("tab"))
	v.Update(keyMsg("tab"))  // pane has no items: back to the grid
	v.Update(keyMsg("down")) // still moves the grid selection
	bodyLines(t, v)          // re-render so the rects reflect the selection
	if got := v.Rects(); len(got) != 3 || got[0].Y != 14 {
		t.Fatalf("focus lost after tab cycle: %+v", got)
	}
}

// Enter on the grid does not start anything (this screen never starts).
func TestEnterOnGridDoesNothing(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	v.Body(76, 26)
	if cmd := v.Update(keyMsg("enter")); cmd != nil {
		t.Fatalf("enter on the grid produced a cmd %v", cmd)
	}
	if !someLine(bodyLines(t, v), "select a card, then vcs, AI Provider") {
		t.Fatalf("enter must not open the pane")
	}
}

// Empty registry: the grid shows the create-first hint.
func TestEmptyRegistryHint(t *testing.T) {
	e := newEnv(t)
	v := e.view(t)
	lines := bodyLines(t, v)
	if !someLine(lines, "No projects registered") || !someLine(lines, "New Project") {
		t.Fatalf("empty-registry hint missing:\n%s", join(lines))
	}
}

// Wheel scrolling (regression: 12 projects, one card per column at width
// 76 — only the window renders; wheel dir scrolls the grid window).
func TestGridScrollWindow(t *testing.T) {
	// Letter+digit-free-of-path names so substring assertions stay clean.
	e := newEnv(t, "b", "f", "h", "j", "k", "p", "q", "u", "v", "x", "y", "z")
	v := e.view(t)
	lines := bodyLines(t, v)
	if !someLine(lines, "│ b ") {
		t.Fatalf("first card missing:\n%s", join(lines))
	}
	if someLine(lines, "│ z ") {
		t.Fatalf("out-of-window card rendered:\n%s", join(lines))
	}
	for i := 0; i < 9; i++ {
		v.Update(wheelAt(1))
	}
	lines = bodyLines(t, v)
	if !someLine(lines, "│ z ") {
		t.Fatalf("wheel scroll did not reach the last card:\n%s", join(lines))
	}
}

// --- helpers ---

func join(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// clickCard clicks the button rect whose rendered row carries the label
// (labels render on both cards; the rects exist on the selected card).
func clickCard(t *testing.T, v *View, label string) {
	t.Helper()
	lines := bodyLines(t, v)
	for _, r := range v.Rects() {
		if strings.Contains(paneRowText(lines, r), label) {
			v.Update(clickAt(r.X, r.Y))
			return
		}
	}
	t.Fatalf("no hit rect carrying %q: rects=%+v", label, v.Rects())
}
