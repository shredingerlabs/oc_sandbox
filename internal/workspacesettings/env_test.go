package workspacesettings

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// Logic-test fixture: real registry entries in a temp home (the read
// helpers need a registered workspace root, nothing else).
type env struct {
	home  string
	seq   int
	roots map[string]string
}

func newEnv(t *testing.T, names ...string) *env {
	t.Helper()
	home := filepath.Join(os.TempDir(), "ocps-"+t.Name())
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	e := &env{home: home, roots: map[string]string{}}
	if err := e.reg(t, time.Now()).CreateWorkspacesFile(); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		e.addWorkspace(t, n)
	}
	return e
}

func (e *env) reg(t *testing.T, now time.Time) *registry.Registry {
	t.Helper()
	seq := e.seq
	e.seq++
	return &registry.Registry{Home: e.home, Now: func() time.Time { return now.Add(-time.Duration(seq) * time.Minute) }}
}

func (e *env) addWorkspace(t *testing.T, name string) {
	t.Helper()
	root := filepath.Join(e.home, "workspaces", name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	r := e.reg(t, time.Now())
	if err := r.AddWorkspace(registry.AddRequest{Name: name, Path: root, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.Create(e.home, sandboxcfg.Path(root), "base", []string{"skills"}, "console", "gwdg-saia", sandboxcfg.SearchWebsearch); err != nil {
		t.Fatal(err)
	}
	e.roots[name] = root
}
