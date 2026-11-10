package migration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// fixtureLegacy builds the committed pre-rename install shape: an empty
// workspace root with the legacy project/ content dir, a sandbox_config
// with the old keys, the legacy registry and a global config file.
func fixtureLegacy(t *testing.T) (home, root string) {
	t.Helper()
	home = t.TempDir()
	root = filepath.Join(home, "oc-sandbox", "alpha")
	for _, d := range []string{"project", ".opencode_config", ".git_local"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "project", ".keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sbx := `{
  "container_edition": "swdev",
  "project_source": "empty",
  "project_kind": "pkm",
  "repo_url": "",
  "version": "1.0"
}
`
	if err := os.WriteFile(filepath.Join(root, ".opencode_config", "sandbox_config.json"), []byte(sbx), 0o644); err != nil {
		t.Fatal(err)
	}
	p := config.Paths{Home: home}
	if err := os.MkdirAll(p.Dir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		p.LegacyProjectsPath(),
		[]byte("{\n  \"projects\": [{\"name\": \"alpha\", \"path\": \""+root+"\", \"container_id\": \"c01ffeecab1e\", \"last_used\": \"2020-01-02T03:04:05Z\", \"container_status\": \"stopped\", \"git_tracking\": \"none\", \"repo_url\": \"\"}],\n  \"version\": \"1.0\"\n}\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.GlobalConfigPath(), []byte("{\n  \"default_project_path\": \""+filepath.Join(home, "oc-sandbox")+"\",\n  \"version\": \"1.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return home, root
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDetectLegacy(t *testing.T) {
	home, _ := fixtureLegacy(t)
	if got := Detect(home); got != Migrate {
		t.Fatalf("Detect = %v, want Migrate", got)
	}
}

func TestDetectFresh(t *testing.T) {
	home := t.TempDir()
	if got := Detect(home); got != Fresh {
		t.Fatalf("Detect = %v, want Fresh", got)
	}
}

// A fresh install never re-enters the migration on its second start
// (§2 guard): every fresh-create path writes its general config file at
// the 2.0 marker, so once the registry exists Detect reads Nothing.
func TestDetectFreshInstallNoRerun(t *testing.T) {
	home := t.TempDir()
	if err := (&registry.Registry{Home: home}).CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	if err := config.CreateGlobalConfig(home, ""); err != nil {
		t.Fatal(err)
	}
	if err := catalogs.EnsureFile(home); err != nil {
		t.Fatal(err)
	}
	if got := Detect(home); got != Nothing {
		t.Fatalf("Detect = %v, want Nothing on a fresh install (fresh creates start at 2.0)", got)
	}
}

// A catalogs.json still at 1.0 (mid-migration leftover) reruns the
// migration, and stage 6 bumps it to the 2.0 completion marker.
func TestRunBumpsLegacyCatalogVersion(t *testing.T) {
	home := t.TempDir()
	if err := (&registry.Registry{Home: home}).CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	cat := config.Paths{Home: home}.CatalogsPath()
	if err := os.MkdirAll(filepath.Dir(cat), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cat, []byte("{\n  \"trackers\": [],\n  \"providers\": [],\n  \"version\": \"1.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Detect(home); got != Migrate {
		t.Fatalf("Detect = %v, want Migrate (catalogs < 2.0 reruns)", got)
	}
	if _, err := Run(home); err != nil {
		t.Fatal(err)
	}
	if v := ReadVersion(cat); v != "2.0" {
		t.Fatalf("catalogs.json version = %q, want 2.0 after the rerun", v)
	}
	if got := Detect(home); got != Nothing {
		t.Fatalf("Detect after rerun = %v, want Nothing", got)
	}
}

func TestDetectRerunOnStaleVersion(t *testing.T) {
	home := t.TempDir()
	reg := &registry.Registry{Home: home}
	if err := reg.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	// seed writes 2.0; downgrade to simulate an interrupted migration
	if err := os.WriteFile(config.Paths{Home: home}.WorkspacesPath(),
		[]byte("{\"workspaces\":[],\"version\":\"1.0\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Detect(home); got != Migrate {
		t.Fatalf("Detect = %v, want Migrate (version < 2.0 reruns)", got)
	}
}

// Stage-2's sandbox_config.json writes rotate into the REAL home's
// backups/ (§2: "through the existing backups/ rotation") — on the
// committed fixture layout the roots sit under <home>/ws/, and the old
// workingHome guess rotated their backups into <home>/ws/.config/….
func TestRunSandboxConfigBackupsLandInRealHome(t *testing.T) {
	home := copyFixtureLegacyHome(t)
	if _, err := Run(home); err != nil {
		t.Fatal(err)
	}
	bd, err := os.ReadDir(config.Paths{Home: home}.BackupsDir())
	if err != nil {
		t.Fatal(err)
	}
	sbx := 0
	for _, e := range bd {
		if strings.HasPrefix(e.Name(), "sandbox_config.json.") {
			sbx++
		}
	}
	if sbx < 2 { // at least one per registered root (pkm-a, code-b)
		t.Fatalf("sandbox_config.json backups in the real home backups/ = %d, want ≥ 2 (%v)", sbx, bd)
	}
	if _, err := os.Stat(filepath.Join(home, "ws", ".config")); !os.IsNotExist(err) {
		t.Fatalf("stage-2 backups rotated into the roots parent dir: %v", err)
	}
}

// copyFixtureLegacyHome materializes the committed pre-rename fixture
// layout (testdata/legacy-home) with @HOME@ placeholders substituted.
func copyFixtureLegacyHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	if err := os.CopyFS(home, os.DirFS("testdata/legacy-home")); err != nil {
		t.Fatal(err)
	}
	rep := func(rel string) {
		p := filepath.Join(home, rel)
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(strings.ReplaceAll(string(data), "@HOME@", home)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rep(".config/oc-sandbox/projects.json")
	rep(".config/oc-sandbox/global_config.json")
	return home
}

func TestRunMigrates(t *testing.T) {
	home, root := fixtureLegacy(t)
	st, err := Run(home)
	if err != nil {
		t.Fatal(err)
	}
	if st != Migrate {
		t.Fatalf("Run state = %v, want Migrate", st)
	}

	// Stage order landed with the exact new names:
	if _, err := os.Stat(filepath.Join(root, "workspace")); err != nil {
		t.Fatalf("workspace/ not created by the rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "project")); !os.IsNotExist(err) {
		t.Fatalf("project/ still exists after migration: %v (err %v)", filepath.Join(root, "project"), err)
	}

	sbx := read(t, filepath.Join(root, ".opencode_config", "sandbox_config.json"))
	if strings.Contains(sbx, "project_source") || strings.Contains(sbx, "project_kind") {
		t.Fatalf("sandbox_config.json still carries legacy keys:\n%s", sbx)
	}
	if !strings.Contains(sbx, `"workspace_source": "empty"`) || !strings.Contains(sbx, `"workspace_kind": "pkm"`) {
		t.Fatalf("renamed keys lost: %s", sbx)
	}

	reg := read(t, config.Paths{Home: home}.WorkspacesPath())
	if !strings.Contains(reg, `"workspaces"`) || strings.Contains(reg, `"projects"`) {
		t.Fatalf("workspaces.json top key wrong:\n%s", reg)
	}
	if strings.Contains(reg, "\"path\": \""+filepath.Join(root, "workspace")) {
		t.Fatalf("registry path field was repointed at the content dir")
	}
	if !strings.Contains(reg, "\"path\": \""+filepath.Join(root, "\"")) && !strings.Contains(reg, "\"path\": \""+root) {
		t.Fatalf("workspace path field not preserved:\n%s", reg)
	}
	if !strings.Contains(reg, "c01ffeecab1e") {
		t.Fatalf("container identity re-derived or lost:\n%s", reg)
	}
	if _, err := os.Stat(config.Paths{Home: home}.LegacyProjectsPath()); !os.IsNotExist(err) {
		t.Fatalf("legacy projects.json still present: %v", err)
	}

	gc := read(t, config.Paths{Home: home}.GlobalConfigPath())
	if strings.Contains(gc, "default_project_path") || strings.Contains(gc, "default_workspace_path") == false {
		t.Fatalf("global_config key rename failed:\n%s", gc)
	}
	if !strings.Contains(gc, `"version": "2.0"`) {
		t.Fatalf("global_config version not 2.0:\n%s", gc)
	}

	cat := read(t, config.Paths{Home: home}.CatalogsPath())
	if !strings.Contains(cat, `"trackers"`) || !strings.Contains(cat, `"providers"`) {
		t.Fatalf("catalogs.json not seeded:\n%s", cat)
	}

	// backups exist for the global config and the legacy registry.
	bd, _ := os.ReadDir(config.Paths{Home: home}.BackupsDir())
	sawGlobal, sawProject := false, false
	for _, e := range bd {
		if strings.HasPrefix(e.Name(), "global_config.json.") {
			sawGlobal = true
		}
		if strings.HasPrefix(e.Name(), "projects.json.") {
			sawProject = true
		}
	}
	if !sawGlobal || !sawProject {
		t.Fatalf("migration backups missing (global=%v project=%v): %v", sawGlobal, sawProject, bd)
	}
}

func TestRunIdempotent(t *testing.T) {
	home, root := fixtureLegacy(t)
	if _, err := Run(home); err != nil {
		t.Fatal(err)
	}
	second, err := Run(home)
	if err != nil {
		t.Fatalf("rerun errored: %v", err)
	}
	if second != Nothing {
		t.Fatalf("rerun state = %v, want Nothing", second)
	}
	// content dir untouched on rerun
	if _, err := os.Stat(filepath.Join(root, "workspace")); err != nil {
		t.Fatalf("workspace/ lost on rerun: %v", err)
	}
	// a fully-migrated install passes the guard
	if got := Detect(home); got != Nothing {
		t.Fatalf("Detect after migration = %v, want Nothing", got)
	}
}

func TestRunAbortsOnFirstError(t *testing.T) {
	home, _ := fixtureLegacy(t)
	// Break the migration: make the registry file unreadable mid-flow by
	// shadowing config.AtomicWrite? Instead corrupt the legacy registry
	// json so jsonfmt stage 3 fails loudly.
	// Simpler: create BOTH project/ and workspace/ in a root to trigger
	// the rename conflict.
	root := filepath.Join(home, "oc-sandbox", "alpha")
	for _, d := range []string{"project", "workspace"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Run(home)
	if err == nil {
		t.Fatal("migration did not abort on the conflicting layout")
	}
	if !strings.Contains(err.Error(), "both project/ and workspace/ exist") {
		t.Fatalf("abort reason unclear: %v", err)
	}
}
