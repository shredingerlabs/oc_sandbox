package openproject

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Card content row 2: the last-used stamp as a relative date; a project
// never stamped keeps its path (legacy registries must not render blank
// cards).

// TestCardShowsRelativeLastUsed: harness stamps recede minutes from the
// test clock — the card reads "Today HH:MM", never the long path.
func TestCardShowsRelativeLastUsed(t *testing.T) {
	e := newEnv(t, nil)
	lines := bodyOf(t, e.m, bodyW, bodyH)
	var rect shell.Rect
	found := false
	for _, r := range e.m.CardRects() {
		if strings.Contains(paneRowRowAt(lines, r, 1), "alpha") { // content row 1 = name
			rect, found = r, true
		}
	}
	if !found {
		t.Fatal("alpha card missing")
	}
	row := paneRowRowAt(lines, rect, 2) // content row 2 = the stamp row
	if !strings.Contains(row, "Today") {
		t.Errorf("card stamp row = %q, want a relative last-used date", row)
	}
	if strings.Contains(row, "/") && strings.Contains(row, "projects") {
		t.Errorf("the path still leaked onto the card: %q", row)
	}
}

// TestCardShowsPathWhenNeverStamped: a registry entry without last_used
// (pre-stamp projects) falls back to the path.
func TestCardShowsPathWhenNeverStamped(t *testing.T) {
	e := newEnv(t, nil)
	regPath := filepath.Join(e.home, ".config", "oc-sandbox", "projects.json")
	data, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	entries, _ := doc["projects"].([]any)
	stamped, _ := entries[0].(map[string]any)
	stamped["last_used"] = "" // alpha: never stamped
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(regPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	e.m.Activate()
	lines := bodyOf(t, e.m, bodyW, bodyH)
	var rect shell.Rect
	found := false
	for _, r := range e.m.CardRects() {
		if strings.Contains(paneRowRowAt(lines, r, 1), "alpha") {
			rect, found = r, true
		}
	}
	if !found {
		t.Fatal("alpha card missing after reload")
	}
	row := paneRowRowAt(lines, rect, 2)
	want := string([]rune(e.roots["alpha"])[:25]) + "…"
	if !strings.Contains(row, want) {
		t.Errorf("unstamped project did not fall back to its path: row %q, want prefix %q", row, want)
	}
}
