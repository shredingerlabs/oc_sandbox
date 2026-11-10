package workspacesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// cardRowText renders the content line dy rows under a card's top
// border (same convention as the open-workspace harness).
func cardRowText(lines []string, r shell.Rect, dy int) string {
	idx := r.Y - shell.SubTabRowY - 1 + dy
	if idx < 0 || idx >= len(lines) {
		return ""
	}
	l := lines[idx]
	rs := []rune(l)
	if r.X >= len(rs) {
		return ""
	}
	l = string(rs[r.X:])
	if len(rs)-r.X > r.W {
		l = string(rs[r.X : r.X+r.W])
	}
	return l
}

// TestCardShowsRelativeLastUsed: the harness stamps recede minutes from
// the test clock, so the card reads "Today HH:MM" instead of its path.
func TestCardShowsRelativeLastUsed(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	v := e.view(t)
	lines := bodyLines(t, v)
	if len(v.cardRects) < 2 {
		t.Fatalf("cards missing: %v", v.cardRects)
	}
	name := cardRowText(lines, v.cardRects[0], 1) // content row 1 = name
	stampRow := cardRowText(lines, v.cardRects[0], 2)
	if !strings.Contains(name, "alpha") {
		t.Fatalf("first card = %q, want alpha (last-used desc)", name)
	}
	if !strings.Contains(stampRow, "Today") {
		t.Errorf("card stamp row = %q, want a relative last-used date", stampRow)
	}
	if strings.Contains(stampRow, "workspaces") {
		t.Errorf("the path still leaked onto the card: %q", stampRow)
	}
}

// TestCardShowsPathWhenNeverStamped: an entry without last_used (legacy
// registries) falls back to the path rather than a blank row.
func TestCardShowsPathWhenNeverStamped(t *testing.T) {
	e := newEnv(t, "alpha", "beta")
	regPath := filepath.Join(e.home, ".config", "oc-sandbox", "workspaces.json")
	data, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	entries, _ := doc["workspaces"].([]any)
	entry, _ := entries[1].(map[string]any)
	if entry["name"] != "beta" {
		t.Fatalf("fixture changed: %v", entries[1])
	}
	entry["last_used"] = ""
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(regPath, out, 0o644); err != nil {
		t.Fatal(err)
	}
	v := e.view(t)
	v.Reload()
	lines := bodyLines(t, v)
	stampRow := cardRowText(lines, v.cardRects[1], 2)
	want := string([]rune(e.roots["beta"])[:26])
	if !strings.Contains(stampRow, want) {
		t.Errorf("unstamped workspace did not fall back to its path: row %q, want prefix %q", stampRow, want)
	}
}
