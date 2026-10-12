package containerview

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// snapshotHome captures the whole $HOME config tree as path→bytes so a
// Build invocation can be asserted byte-identical afterwards (#92:
// "Build writes no config files").
func snapshotHome(t *testing.T, home string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(home, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(home, path)
		if err != nil {
			return err
		}
		out[rel] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// #92 acceptance: a Build op invocation does NOT touch $HOME
// config/registry files — the tree is byte-identical before/after.
func TestBuildOpLeavesHomeConfigTreeUntouched(t *testing.T) {
	m, fake, reg, home := newFixture(t)
	fake.Script = func(name string, args []string) (string, error) {
		return "STEP 1/2\n", nil
	}

	projectsFile := filepath.Join(home, ".config", "oc-sandbox", "projects.json")
	globalFile := filepath.Join(home, ".config", "oc-sandbox", "global_config.json")
	if err := os.MkdirAll(filepath.Dir(projectsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := reg.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(globalFile, []byte(`{"default_project_path":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(home, "projects", "demo")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.Create(home, sandboxcfg.Path(root), "base", []string{"skills"}, "console", "gwdg-saia"); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject(registry.AddRequest{Name: "demo", Path: root, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}

	m.Body(80, 22)
	before := snapshotHome(t, home)

	cmd := m.Update(keyType(tea.KeyEnter)) // Build button on the selected edition
	runCmds(t, m, cmd)
	if m.Busy() {
		t.Fatal("build op still busy after completion")
	}

	after := snapshotHome(t, home)
	if len(after) != len(before) {
		t.Fatalf("config tree changed size: %d files before, %d after", len(before), len(after))
	}
	for rel, b := range before {
		afterB, ok := after[rel]
		if !ok {
			t.Fatalf("file vanished during build: %s", rel)
		}
		if !bytes.Equal(b, afterB) {
			t.Errorf("file modified during build: %s", rel)
		}
	}
}
