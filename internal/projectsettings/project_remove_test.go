package projectsettings

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Remove project from list (glossary): the selected card's third button
// opens the removal pane; only the pane's guarded button behind the
// shared confirmation popup (ADR-0021, red confirm) deregisters the
// project — projects.json only, everything else on disk untouched.

// openRemovePane opens the removal pane on the selected (first) card.
func openRemovePane(t *testing.T, v *View) {
	t.Helper()
	v.Body(76, 60)
	clickCard(t, v, "remove")
	if !someLine(bodyLines(t, v), "Remove project from list") {
		t.Fatalf("removal pane not open:\n%s", join(bodyLines(t, v)))
	}
}

// The card's remove button opens the pane (info text + warning), and the
// click itself changes nothing: registry intact, no popup, no op.
func TestCardRemoveButtonOpensPaneWithoutEffects(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	openRemovePane(t, v)
	lines := bodyLines(t, v)
	for _, want := range []string{
		"Remove — alpha",
		"Removes the project's entry from the",
		"registry (projects.json) only. Files,",
		"sandbox_config.json and the container",
		"A running container keeps running",
		"untracked — nothing gets stopped.",
	} {
		if !someLine(lines, want) {
			t.Errorf("pane missing %q:\n%s", want, join(lines))
		}
	}
	if v.ConfirmOpen() {
		t.Error("card button armed the popup")
	}
	rows, err := e.reg(t, time.Now()).AllOrdered()
	if err != nil || len(rows) != 2 {
		t.Fatalf("registry touched by the pane opening: %v %v", rows, err)
	}
	if len(e.launch.ops) != 0 {
		t.Errorf("op queued: %v", e.launch.ops)
	}
}

// The pane's list button opens the confirmation popup (title + context
// lines) before anything runs; cancelling leaves the registry untouched
// and the pane open for the project.
func TestRemoveListButtonPopupGatesTheWrite(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	openRemovePane(t, v)
	if cmd := clickListLabel(t, v); cmd != nil {
		t.Fatalf("list click produced cmd %v", cmd)
	}
	if !v.ConfirmOpen() {
		t.Fatalf("confirmation popup not open:\n%s", join(bodyLines(t, v)))
	}
	lines := bodyLines(t, v)
	for _, want := range []string{
		"Remove alpha from the project list?",
		"Removes the registry entry only:",
		"files, configs and the container stay on disk.",
		"A running container keeps running untracked.",
		"Remove",
		"Cancel",
	} {
		if !someLine(lines, want) {
			t.Errorf("popup missing %q:\n%s", want, join(lines))
		}
	}
	rows, err := e.reg(t, time.Now()).AllOrdered()
	if err != nil || len(rows) != 2 {
		t.Fatalf("registry touched before confirmation: %v %v", rows, err)
	}
	// Cancel (default popup focus) → nothing happens, pane stays.
	v.Update(keyMsg("esc"))
	if v.ConfirmOpen() {
		t.Error("esc did not cancel the popup")
	}
	if !someLine(bodyLines(t, v), "Remove — alpha") {
		t.Errorf("pane closed by the cancel")
	}
	if rows, err := e.reg(t, time.Now()).AllOrdered(); err != nil || len(rows) != 2 {
		t.Fatalf("registry changed by the cancel: %v %v", rows, err)
	}
}

// Confirming the popup deregisters the project: the registry entry is
// gone, the files (sandbox_config.json et al) stay, the cards and pane
// reset, and the toast names the removed project.
func TestRemoveConfirmWritesRegistryOnly(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	openRemovePane(t, v)
	clickListLabel(t, v)
	removePopupConfirm(t, v)
	if v.ConfirmOpen() {
		t.Error("popup not closed after confirm")
	}
	rows, err := e.reg(t, time.Now()).AllOrdered()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "beta" {
		t.Fatalf("registry after removal = %v, want only beta", rows)
	}
	// No project file changes: the root is still a complete project.
	if _, err := os.Stat(sandboxcfg.Path(e.roots["alpha"])); err != nil {
		t.Fatalf("sandbox_config.json removed with the entry: %v", err)
	}
	lines := bodyLines(t, v)
	if someLine(lines, "│ alpha") {
		t.Errorf("removed card still rendered:\n%s", join(lines))
	}
	if !someLine(lines, "Removed alpha from the project") {
		t.Errorf("toast missing:\n%s", join(lines))
	}
	if !someLine(lines, "select a card, then vcs") {
		t.Errorf("pane not reset after removal:\n%s", join(lines))
	}
}

// A failed registry write surfaces the error, changes nothing, and the
// removal pane stays put.
func TestRemoveFailureSurfacedNothingChanged(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	openRemovePane(t, v)
	if err := os.Remove(e.home + "/.config/oc-sandbox/projects.json"); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	clickListLabel(t, v)
	removePopupConfirm(t, v)
	lines := bodyLines(t, v)
	if !someLine(lines, "Removal failed") {
		t.Errorf("failure not surfaced:\n%s", join(lines))
	}
	if !someLine(lines, "Remove — alpha") {
		t.Errorf("pane not kept after the failure")
	}
}

// Idle guard (ADR-0021): the popup never fires while one of this
// screen's ops is still queued/running.
func TestRemoveIdleGuardDuringOps(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	openRemovePane(t, v)
	v.aiRunning = true
	if cmd := clickListLabel(t, v); cmd != nil {
		t.Fatalf("guard click produced cmd %v", cmd)
	}
	if v.ConfirmOpen() {
		t.Fatal("popup armed while an op runs")
	}
	if !someLine(bodyLines(t, v), "Wait for the running operation.") {
		t.Errorf("guard toast missing")
	}
}

// Keyboard parity: tab reaches the pane's list button, enter arms the
// popup, the popup's keys confirm/cancel as everywhere.
func TestRemoveKeyboardParity(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	openRemovePane(t, v)
	v.Update(keyMsg("tab")) // grid → list button
	if !someLine(bodyLines(t, v), "Remove — alpha") {
		t.Fatalf("tab dropped the pane")
	}
	v.Update(keyMsg("enter"))
	if !v.ConfirmOpen() {
		t.Fatalf("enter on the list button did not arm the popup")
	}
	v.Update(keyMsg("esc"))
	// And tab backwards returns to the grid without leaving a stale focus
	// (arrows move the card selection again thereafter).
	v.Update(keyMsg("shift+tab"))
	if v.ConfirmOpen() {
		t.Error("popup open after cancel")
	}
	v.Update(keyMsg("down"))
	bodyLines(t, v) // re-render so the rects reflect the selection
	if got := v.Rects(); len(got) < 3 || got[0].Y != 14 {
		t.Fatalf("shift+tab did not return focus to the grid: %+v", got)
	}
}

// Selected-button highlight: the button whose pane is open renders in
// the selected menu-entry style. Visible in geometry: the highlighted
// label gains its two padding columns (+2 width, shifting neighbours).
func TestSelectedButtonHighlightRidesTheOpenPane(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	v.Body(76, 60)
	if got, want := cardButtonWidths(v), []int{3, 11, 6}; !intsEq(got, want) {
		t.Fatalf("idle widths = %v, want %v", got, want)
	}
	clickCard(t, v, "AI Provider")
	bodyLines(t, v) // re-render so the rects carry the open-pane widths
	if got, want := cardButtonWidths(v), []int{3, 13, 6}; !intsEq(got, want) {
		t.Fatalf("AI-open widths = %v, want %v", got, want)
	}
	clickCard(t, v, "remove")
	bodyLines(t, v)
	if got, want := cardButtonWidths(v), []int{3, 11, 8}; !intsEq(got, want) {
		t.Fatalf("remove-open widths = %v, want %v", got, want)
	}
	clickCard(t, v, "vcs")
	bodyLines(t, v)
	if got, want := cardButtonWidths(v), []int{5, 11, 6}; !intsEq(got, want) {
		t.Fatalf("vcs-open widths = %v, want %v", got, want)
	}
}

// The highlighted card button uses the selected menu-entry style
// (accent fill, bold, dark fg) — asserted via the raw render.
func TestHighlightedButtonUsesHeaderSelectedStyle(t *testing.T) {
	e := newEnv(t, "alpha")
	v := e.view(t)
	v.Body(76, 60)
	clickCard(t, v, "remove")
	raw := v.Body(76, 60)
	if !strings.Contains(raw, shell.StyleHeaderItemSel.Render("remove")) {
		t.Errorf("highlighted button not rendered with the selected menu-entry style")
	}
}

func clickListLabel(t *testing.T, v *View) tea.Cmd {
	t.Helper()
	lines := bodyLines(t, v)
	for _, r := range v.Rects() {
		if strings.Contains(paneRowText(lines, r), "Remove project from list") {
			return v.Update(clickAt(r.X, r.Y))
		}
	}
	t.Fatalf("list button not found in rects\n%s", join(lines))
	return nil
}

// removePopupConfirm switches the popup focus to its red confirm and
// activates it (keyboard path).
func removePopupConfirm(t *testing.T, v *View) tea.Cmd {
	t.Helper()
	if !v.ConfirmOpen() {
		t.Fatal("popup not open")
	}
	v.Update(keyMsg("tab"))
	return v.Update(keyMsg("enter"))
}

// cardButtonWidths is the widths of the first three rects (the selected
// card's buttons come first in Rects()).
func cardButtonWidths(v *View) []int {
	return widthsOf(v.Rects()[:3])
}

func intsEq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func widthsOf(rects []shell.Rect) []int {
	out := make([]int, len(rects))
	for i, r := range rects {
		out[i] = r.W
	}
	return out
}
