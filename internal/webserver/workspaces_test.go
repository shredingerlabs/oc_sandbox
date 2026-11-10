package webserver

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// seedWorkspace writes one registry entry plus its sandbox_config.json
// under a fake home — the card-fragment fixtures.
func seedWorkspace(t *testing.T, home, name, edition string, modes []string) string {
	t.Helper()
	root := t.TempDir()
	reg := &registry.Registry{Home: home}
	if err := reg.CreateWorkspacesFile(); err != nil && !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("seed registry file: %v", err)
	}
	if err := reg.AddWorkspace(registry.AddRequest{Name: name, Path: root}); err != nil {
		t.Fatalf("seed registry: %v", err)
	}
	if err := sandboxcfg.Create(home, sandboxcfg.Path(root), edition, modes, "web", "", sandboxcfg.SearchSearxng); err != nil {
		t.Fatalf("seed sandbox_config: %v", err)
	}
	return root
}

// seedRegistry pins registry entries with exact stored fields (byte
// shape per registry.AddWorkspace) — for deterministic last_used reads.
func seedRegistry(t *testing.T, home string, entries ...struct{ name, lastUsed, status string }) string {
	t.Helper()
	roots := make([]string, len(entries))
	items := make([]string, len(entries))
	for i, e := range entries {
		root := t.TempDir()
		roots[i] = root
		items[i] = fmt.Sprintf(`        {
            "name": %q,
            "path": %q,
            "container_id": %q,
            "last_used": %q,
            "container_status": %q,
            "git_tracking": "none",
            "repo_url": ""
        }`, e.name, root, registry.ContainerIdentity(root), e.lastUsed, e.status)
	}
	body := fmt.Sprintf(`{
    "workspaces": [
%s
    ],
    "version": "1.0"
}`, strings.Join(items, ",\n"))
	if err := os.MkdirAll(filepath.Join(home, ".config", "oc-sandbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "oc-sandbox", "workspaces.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return roots[0]
}

// startFixtureServer starts a test server pointed at a fake home.
func startFixtureServer(t *testing.T, home string) *Server {
	t.Helper()
	s := New(Options{BasePort: 0, Home: home})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// GET /workspaces (#153, spec §7): workspace cards in a grid — edition
// icon tile tinted via the edition hue, name, settings row, relative
// last-used date, action rows.
func TestWorkspacesCards(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	seedWorkspace(t, home, "hil-lab", "embedded", nil)

	s := startFixtureServer(t, home)
	resp := get(t, s.URL()+"workspaces")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html fragment", ct)
	}
	doc := body(t, resp)
	for _, want := range []string{
		`data-ws="alpha"`,
		`data-ws="hil-lab"`,
		`img/editions/swdev.svg`,    // edition icon tile (Lucide glyph, §5)
		`img/editions/embedded.svg`, // per-edition glyph
		`--ed:#2f6fed`,              // swdev hue tints the tile
		`--ed:#0ea5a4`,              // embedded hue
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("card fragment missing %q", want)
		}
	}
}

// The HIL chip renders whenever hil_mode is on — any edition (§6) —
// and never onto workspaces without it.
func TestWorkspacesHILChip(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "hil-lab", "embedded", []string{"hil_mode"})
	seedWorkspace(t, home, "plain", "swdev", nil)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	var hil, plain string
	for _, card := range strings.Split(doc, "<article")[1:] {
		if strings.Contains(card, `data-ws="hil-lab"`) {
			hil = card
		}
		if strings.Contains(card, `data-ws="plain"`) {
			plain = card
		}
	}
	if hil == "" || plain == "" {
		t.Fatalf("cards missing:\n%s", doc)
	}
	if !strings.Contains(hil, `data-chip="hil"`) {
		t.Fatal("HIL chip missing for hil_mode workspace")
	}
	if strings.Contains(plain, `data-chip="hil"`) {
		t.Fatal("HIL chip must not leak onto workspaces without hil_mode")
	}
}

// Relative last-used date (§7): registry last_used rendered relative to
// server time — hours, days, else the date; never a raw timestamp.
func TestWorkspacesRelativeLastUsed(t *testing.T) {
	home := t.TempDir()
	seedRegistry(t, home, struct{ name, lastUsed, status string }{"alpha", "2026-01-05T10:00:00Z", "stopped"})
	now := time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)

	s := New(Options{BasePort: 0, Home: home, Now: func() time.Time { return now }})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, "2h ago") {
		t.Fatalf("card missing relative last-used \"2h ago\", got:\n%s", doc)
	}
	if strings.Contains(doc, "2026-01-05T10:00:00Z") {
		t.Fatal("raw registry timestamp must not leak into the card")
	}
}

// Running state: registry container_status "running" marks the card —
// the Signal running dot — without a podman probe on the render path.
func TestWorkspacesRunningDot(t *testing.T) {
	home := t.TempDir()
	seedRegistry(t, home,
		struct{ name, lastUsed, status string }{"alpha", "2026-01-05T10:00:00Z", "running"},
		struct{ name, lastUsed, status string }{"beta", "2026-01-05T09:00:00Z", "stopped"},
	)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	iAlpha := strings.Index(doc, `data-ws="alpha"`)
	iBeta := strings.Index(doc, `data-ws="beta"`)
	if iAlpha < 0 || iBeta < 0 {
		t.Fatalf("cards missing:\n%s", doc)
	}
	if !strings.Contains(doc[iAlpha:iBeta], `data-running`) {
		t.Fatal("running card missing the running marker")
	}
}

// Card start options (ADR-0033 cards): all three accesses render on
// every card in the Web · OpenCode · Console order — the stop/delete
// confirms left the cards (Container center owns them), and the Restart
// confirm gates the running-container replacement.
func TestWorkspacesCardActions(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	_ = root

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	iWeb := strings.Index(doc, `data-act="web"`)
	iOC := strings.Index(doc, `data-act="opencode"`)
	iCs := strings.Index(doc, `data-act="console"`)
	if iWeb < 0 || iOC < 0 || iCs < 0 || !(iWeb < iOC && iOC < iCs) {
		t.Fatalf("card start options must render in Web · OpenCode · Console order, got:\n%s", doc)
	}
	for _, want := range []string{
		`data-act="web"`,
		`data-act="opencode"`,
		`data-act="console"`,
		`data-confirm="restart"`,
	} {
		if !wantB(doc, want) {
			t.Fatalf("card missing %q, got:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, `data-act="open"`) ||
		strings.Contains(doc, `data-confirm="stop"`) ||
		strings.Contains(doc, `data-confirm="delete"`) {
		t.Fatal("Open and stop/delete confirm rows left the cards")
	}
	if strings.Contains(doc, `data-act="console" data-advanced-only`) {
		t.Fatal("start options are no longer advanced-gated")
	}
}

func wantB(doc, want string) bool { return strings.Contains(doc, want) }

// Server-side ?q= filtering (§9): only matching names render.
func TestWorkspacesQueryFilter(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	seedWorkspace(t, home, "beta", "web", nil)

	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces?q=alp"))
	if !strings.Contains(doc, `data-ws="alpha"`) {
		t.Fatal("matching workspace filtered out")
	}
	if strings.Contains(doc, `data-ws="beta"`) {
		t.Fatal("non-matching workspace must not render under ?q=")
	}
	// Case-insensitive, no match → empty grid, not an error.
	doc = body(t, get(t, s.URL()+"workspaces?q=ZZZ"))
	if strings.Contains(doc, `data-ws="`) {
		t.Fatal("no-match ?q= must render an empty grid")
	}
	if resp := get(t, s.URL()+"workspaces?q=ZZZ"); resp.StatusCode != http.StatusOK {
		t.Fatalf("no-match status = %d, want 200", resp.StatusCode)
	}
}

// Empty registry (§1 wording): "No workspaces registered" — never a
// dead end, and a missing registry file reads as empty.
func TestWorkspacesEmpty(t *testing.T) {
	s := startFixtureServer(t, t.TempDir())
	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, "No workspaces registered") {
		t.Fatalf("empty state missing, got:\n%s", doc)
	}

	home := t.TempDir()
	seedWorkspace(t, home, "gone", "swdev", nil)
	reg := &registry.Registry{Home: home}
	if w, err := reg.ByName("gone"); err != nil {
		t.Fatalf("fixture: %v", err)
	} else if err := reg.RemoveWorkspace(w.Path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	s2 := startFixtureServer(t, home)
	if doc := body(t, get(t, s2.URL()+"workspaces")); !strings.Contains(doc, "No workspaces registered") {
		t.Fatalf("emptied registry must render the empty state, got:\n%s", doc)
	}
}

// Unknown editions fall back to the base glyph + hue — a card never
// renders a broken tile.
func TestWorkspacesUnknownEditionFallsBack(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "weird", "swdev", nil)
	if err := sandboxcfg.PatchString(home, sandboxcfg.Path(root), "container_edition", "nonexistent-edition"); err != nil {
		t.Fatalf("patch edition: %v", err)
	}
	s := startFixtureServer(t, home)
	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, `img/editions/base.svg`) {
		t.Fatalf("unknown edition must fall back to base.svg, got:\n%s", doc)
	}
}
