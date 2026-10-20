package app_test

// Composed-TUI regression tests for the Settings bugs reported against
// the Export / Restore / Import cards: exporting twice to the same
// destination leaked a raw "Refusing to back up…" stderr line into the
// alt-screen (no overwrite prompt), the Restore button ignored mouse
// clicks when it did not hold keyboard focus, and legacy project roots
// (start.sh re-creates the infra dirs at every start) were refused by
// the import structure check.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	teatest "github.com/charmbracelet/x/exp/teatest"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// clickText clicks the first rendered occurrence of sub at row >= minRow.
func clickText(t *testing.T, tm *teatest.TestModel, rec *recorder, sub string, minRow int) {
	t.Helper()
	g := renderScreen(120, 41, string(rec.contents()))
	for y := minRow; y < 41; y++ {
		line := string(g.rows[y])
		if x := strings.Index(line, sub); x >= 0 {
			tm.Send(mousePress(x+1, y))
			return
		}
	}
	t.Fatalf("click target %q not on screen:\n%s", sub, g.text())
}

func waitText(t *testing.T, rec *recorder, subs ...string) {
	t.Helper()
	if !waitScreen(t, rec, false, subs...) {
		t.Fatalf("screen never showed %v; last:\n%s", subs, screenOf(t, rec))
	}
}

// legacyRoot builds a pre-cbd48b7-style project root: sandbox_config.json
// and project/.git exist, but none of the infra dirs added later
// (.cbm_cache, .ssh_local). start.sh mkdir -p's those at every start, so
// such roots are real sandbox projects.
func legacyRoot(t *testing.T, home, name string) string {
	t.Helper()
	root := filepath.Join(home, "oc-sandbox", name)
	if err := os.MkdirAll(filepath.Join(root, "project", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.Create(home, sandboxcfg.Path(root), "base", nil, "console", "none"); err != nil {
		t.Fatal(err)
	}
	return root
}

// E2E: exporting to an existing destination asks for an overwrite
// confirmation (no raw stderr refusal line), the restore button works
// for mouse users regardless of keyboard focus, and a legacy (removed)
// project root imports while a registered root hits the guard.
func TestE2ESettingsExportOverwriteRestoreImport(t *testing.T) {
	home := installHome(t)
	if err := (&registry.Registry{Home: home}).CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	pp := filepath.Join(home, ".config", "oc-sandbox", "projects.json")
	if err := os.WriteFile(pp, []byte("{\"projects\":[],\"version\":\"1.0\"}"), 0o600); err != nil {
		t.Fatal(err)
	}
	// BUG3 fixtures: a prior-registered legacy root (registry entry
	// removed) and a still-registered legacy root.
	removedRoot := legacyRoot(t, home, "removedproj")
	listedRoot := legacyRoot(t, home, "listedproj")
	reg := &registry.Registry{Home: home, Now: time.Now}
	for _, r := range []string{removedRoot, listedRoot} {
		if err := reg.AddProject(registry.AddRequest{
			Name: filepath.Base(r), Path: r, GitTracking: "none",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.RemoveProject(removedRoot); err != nil {
		t.Fatal(err)
	}
	// BUG2 fixtures: two restorable backups with distinct content.
	bd := filepath.Join(home, ".config", "oc-sandbox", "backups")
	if err := os.MkdirAll(bd, 0o755); err != nil {
		t.Fatal(err)
	}
	// BUG2 fixtures: two restorable backups with distinct (valid
	// registry) content — restoring one over the live registry must
	// keep it a working registry file.
	writeBackupFile := func(name, content string) {
		if err := os.WriteFile(filepath.Join(bd, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeBackupFile("projects.json.20260101T000000000.aaa111", "{\"projects\":[],\"version\":\"0.1\"}")
	writeBackupFile("projects.json.20260102T000000000.bbb222", "{\"projects\":[],\"version\":\"0.2\"}")

	run := &podmanRunner{images: defaultImages()}
	tm, rec := newTUI(t, home, run)
	defer tm.Quit()
	waitText(t, rec, "Open Project")

	// --- Settings → Export Config ---
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4")})
	waitText(t, rec, "Edit Project settings")
	clickText(t, tm, rec, "Export Config", 2)
	waitText(t, rec, "Export Config…")

	dest := filepath.Join(home, "oc-sandbox-projects.json")

	// First export: fresh destination, no prompt.
	clickText(t, tm, rec, "Export Config…", 3)
	waitText(t, rec, "Destination file")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})   // dest field → Save
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter}) // Save
	waitText(t, rec, "Exported")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("first export did not create %s", dest)
	}

	// Second export to the SAME destination: the wizard is still open,
	// Save → Cancel → Save again reaches Save with focus.
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyShiftTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitText(t, rec, "Overwrite the destination file?")
	if strings.Contains(string(rec.contents()), "Refusing to back up") {
		t.Errorf("refusal leaked into the output stream")
	}
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // popup: Cancel → Overwrite
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitText(t, rec, "Exported projects.json to "+dest)
	if got, err := os.ReadFile(dest); err != nil || !strings.Contains(string(got), "projects") {
		t.Fatalf("overwrite did not write the snapshot: %q (%v)", string(got), err)
	}

	// --- Restore Config: mouse click on the unfocused Restore button ---
	clickText(t, tm, rec, "Restore Config", 2)
	waitText(t, rec, "Config backups")
	clickText(t, tm, rec, "20260101", 3) // select the older backup row
	clickText(t, tm, rec, "Restore  │", 3)
	waitText(t, rec, "Restore this config backup?")
	tm.Send(tea.KeyMsg{Type: tea.KeyTab}) // popup: Cancel → Restore
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	waitText(t, rec, "Restored projects.json")
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(pp)
		if err == nil && strings.Contains(string(b), "0.1") {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("active file never restored, got %q", string(b))
		}
		time.Sleep(50 * time.Millisecond)
	}

	// --- Import project: legacy (previously removed) root imports ---
	// (the restore replaced the registry file, so re-register the root
	// whose entry must hit the already-registered guard below)
	if err := reg.AddProject(registry.AddRequest{
		Name: "listedproj", Path: listedRoot, GitTracking: "none",
	}); err != nil {
		t.Fatal(err)
	}
	// The path field holds focus on entry; replace the prefill by
	// typing (the chooser's ".oc-sandbox" row also contains
	// "oc-sandbox", so navigation clicks are ambiguous here).
	importRoot := func(root string) {
		clickText(t, tm, rec, "Import project", 2)
		waitText(t, rec, "Project folder path")
		for i := 0; i < 160; i++ {
			tm.Send(tea.KeyMsg{Type: tea.KeyBackspace})
		}
		for _, r := range root {
			tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		waitText(t, rec, root)
		clickText(t, tm, rec, "Import Project  │", 3)
	}
	importRoot(removedRoot)
	waitText(t, rec, "Import summary")
	clickText(t, tm, rec, "Register  │", 3)
	waitText(t, rec, "Imported removedproj")
	if _, err := reg.ByPath(removedRoot); err != nil {
		t.Errorf("root not registered after import: %v", err)
	}

	// Still-listed legacy root: already-registered guard wins.
	importRoot(listedRoot)
	waitText(t, rec, "Already registered")
	if strings.Contains(screenOf(t, rec), "Import isn't possible here") {
		t.Errorf("registered root showed the referrer prompt")
	}
}
