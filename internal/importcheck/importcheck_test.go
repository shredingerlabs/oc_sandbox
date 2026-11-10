package importcheck

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// seedRoot writes a workspace root with the given layout: sandbox_config
// content ("" = no file) and the content dir name ("workspace", "project"
// or "" for none), each with a .git marker.
func seedRoot(t *testing.T, sandboxCfg, contentDir string) string {
	t.Helper()
	root := t.TempDir()
	if sandboxCfg != "" {
		cfgDir := filepath.Join(root, ".opencode_config")
		if err := os.MkdirAll(cfgDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, "sandbox_config.json"), []byte(sandboxCfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if contentDir != "" {
		dir := filepath.Join(root, contentDir, ".git")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

const goodCfg = `{"workspace_kind":"code","workspace_source":"cloned","container_edition":"swdev","start_option":"web","vcs_tracking":"github","repo_url":"https://github.com/x/y"}`

// A canonical folder (sandbox_config.json + workspace/ git worktree)
// matches: the ordinary import summary flow runs, with edition/modes/
// start option straight from the stored config and the name prefilled
// from the folder basename.
func TestCheckMatchingRoot(t *testing.T) {
	root := seedRoot(t, goodCfg, "workspace")
	res, err := Check(root, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Kind != KindMatch {
		t.Fatalf("Kind = %v, want KindMatch", res.Kind)
	}
	if res.Config.ContainerEdition != "swdev" || res.Config.StartOption != "web" || res.Config.VCSTracking != "github" {
		t.Fatalf("Config not read from sandbox_config.json: %+v", res.Config)
	}
	if res.Name != filepath.Base(root) {
		t.Fatalf("Name = %q, want folder basename", res.Name)
	}
}

// A pre-rename project/ layout is non-matching but flagged legacy: the
// informational prompt offers the explicit convert action (spec §2 —
// never silent).
func TestCheckLegacyLayout(t *testing.T) {
	root := seedRoot(t, `{"project_kind":"code"}`, "project")
	res, err := Check(root, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Kind != KindLegacy {
		t.Fatalf("Kind = %v, want KindLegacy", res.Kind)
	}
}

// A folder with neither layout is plain non-matching → referrer prompt.
func TestCheckNonMatching(t *testing.T) {
	root := seedRoot(t, "", "")
	res, err := Check(root, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Kind != KindNonMatching {
		t.Fatalf("Kind = %v, want KindNonMatching", res.Kind)
	}
}

// A broken sandbox_config.json gets its own prompt, not the referrer one.
func TestCheckBrokenConfig(t *testing.T) {
	root := seedRoot(t, "{not json", "workspace")
	res, err := Check(root, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Kind != KindBrokenConfig {
		t.Fatalf("Kind = %v, want KindBrokenConfig", res.Kind)
	}
}

// An already-registered path hits the registered guard — even when its
// folder is incomplete (ADR 0017 precedence).
func TestCheckRegisteredGuard(t *testing.T) {
	root := seedRoot(t, "", "")
	home := t.TempDir()
	reg := &registry.Registry{Home: home}
	if err := reg.CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddWorkspace(registry.AddRequest{Name: "some", Path: root}); err != nil {
		t.Fatal(err)
	}
	res, err := Check(root, reg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.Kind != KindRegistered {
		t.Fatalf("Kind = %v, want KindRegistered", res.Kind)
	}
}

// DeriveName prefills the workspace name from the folder basename
// (bash derive_import_project_name parity): spaces → underscores,
// invalid characters drop, empty slug falls back.
func TestDeriveName(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"my repo", "my_repo"},
		{"weird!!name", "weirdname"},
		{"!!!", "imported_workspace"},
	} {
		if got := DeriveName(c.in); got != c.want {
			t.Errorf("DeriveName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
