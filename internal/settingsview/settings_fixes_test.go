package settingsview

// Regression tests for the reported Settings bugs: export-to-existing-
// destination leaked a raw stderr refusal (no overwrite prompt), the
// Restore button ignored clicks when it did not hold keyboard focus,
// and legacy project roots (infra dirs re-created by start.sh at every
// start) were refused by the import structure check — and a registered
// path showed the referrer prompt instead of the registered guard.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/testutil"
)

// TestRestoreButtonClickableWithoutFocus: the Restore button's hit rect
// must be registered even while a backup row holds focus — a mouse user
// can otherwise never reach it.
func TestRestoreButtonClickableWithoutFocus(t *testing.T) {
	home := homeFixture(t)
	pp := config.Paths{Home: home}.ProjectsPath()
	writeFile(t, pp, "{\n  \"projects\": [],\n  \"version\": \"1.0\"\n}")
	writeBackup(t, home, "projects.json.20260102T000000000.bbb222", "{\n  \"projects\": [],\n  \"version\": \"1.0\"\n}")

	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: subRestore})
	clickLabel(t, v, "20260102") // select a backup row (focus stays on the row)
	clickLabel(t, v, "Restore")
	if !v.confirm.IsOpen() {
		t.Fatalf("clicking the unfocused Restore button did not open the popup:\n%s", body(v))
	}
}

// TestExportOverwriteAsksAndOverwrites: exporting to an existing
// destination opens the overwrite confirmation popup; confirming
// replaces the file (no destination backup machinery, no stderr leak).
func TestExportOverwriteAsksAndOverwrites(t *testing.T) {
	home := homeFixture(t)
	pp := config.Paths{Home: home}.ProjectsPath()
	writeFile(t, pp, "{\n  \"projects\": [],\n  \"version\": \"1.0\"\n}")

	v, mgr := queueSettings(t, home, nil)
	dest := filepath.Join(home, "oc-sandbox-projects.json")
	writeFile(t, dest, "{\"previous\":\"content\"}")

	v.Update(shell.SubTabMsg{Index: subExport})
	clickLabel(t, v, "Export Config…")
	// destination prefill already is oc-sandbox-<base> under $HOME
	clickLabel(t, v, "│  Save  │") // popup opens, export must NOT run yet
	if !v.confirm.IsOpen() {
		t.Fatalf("overwrite popup missing:\n%s", body(v))
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal("destination vanished before confirmation")
	}
	key(v, "tab")
	cmd := key(v, "enter") // confirm
	testutil.Drain(t, v, mgr, cmd)
	if got := readFile(t, dest); !strings.Contains(got, "projects") {
		t.Fatalf("confirmed overwrite did not write the snapshot: %q", got)
	}
	if !strings.Contains(v.toast, "Exported") {
		t.Fatalf("export toast missing: %q", v.toast)
	}
	if orphans := tmpOrphans(t, home, config.Paths{Home: home}.Dir()); len(orphans) > 0 {
		t.Errorf("temp files left behind: %v", orphans)
	}
}

// TestImportLegacyRootRegisters: a root with sandbox_config.json and a
// project/.git worktree imports even without the infra directories
// (start.sh mkdir -p's them at every container start).
func TestImportLegacyRootRegisters(t *testing.T) {
	home := homeFixture(t)
	root := filepath.Join(home, "roots", "legacy")
	if err := os.MkdirAll(filepath.Join(root, "project", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.Create(home, sandboxcfg.Path(root), "base", nil, "console", "none", sandboxcfg.SearchWebsearch); err != nil {
		t.Fatal(err)
	}

	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: subImport})
	typePath(t, v, root)
	clickLabel(t, v, "Import Project")
	if b := body(v); !strings.Contains(b, "Import summary") {
		t.Fatalf("legacy root refused instead of showing the summary:\n%s", b)
	}
	key(v, "down")
	key(v, "enter")
	reg := &registry.Registry{Home: home}
	if _, err := reg.ByPath(root); err != nil {
		t.Fatalf("legacy root not registered: %v", err)
	}
}

// TestImportRegisteredPathWinsOverStructureCheck: a registered path gets
// the already-registered guard even when its folder is incomplete
// (ADR 0017: registered path → info page, never the referrer prompt).
func TestImportRegisteredPathWinsOverStructureCheck(t *testing.T) {
	home := homeFixture(t)
	root := filepath.Join(home, "roots", "listed") // empty folder
	reg := &registry.Registry{Home: home}
	if err := reg.AddProject(registry.AddRequest{Name: "listed", Path: root}); err != nil {
		t.Fatal(err)
	}

	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: subImport})
	typePath(t, v, root)
	clickLabel(t, v, "Import Project")
	if b := body(v); !strings.Contains(b, "Already registered") {
		t.Fatalf("registered path did not hit the guard:\n%s", b)
	}
	if b := body(v); strings.Contains(b, "Import isn't possible here") {
		t.Fatalf("registered path showed the referrer prompt:\n%s", b)
	}
}

// TestBackupFileRefusalStaysOffStderr pins that the backup refusal is
// returned as an error only — the Go TUI owns the terminal and a raw
// stderr write corrupts the alt-screen.
func TestBackupFileRefusalStaysOffStderr(t *testing.T) {
	home := homeFixture(t)
	secret := filepath.Join(home, "proj", ".git_local", "hosts.yml")
	writeFile(t, secret, "token")

	orig := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	_, err := config.BackupFile(home, secret)
	w.Close()
	var buf []byte
	for {
		chunk := make([]byte, 512)
		n, rerr := r.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if rerr != nil {
			break
		}
	}
	os.Stderr = orig
	if err == nil {
		t.Fatal("secret backup was not refused")
	}
	if strings.Contains(string(buf), "Refusing") {
		t.Fatalf("refusal leaked to stderr: %q", string(buf))
	}
}
