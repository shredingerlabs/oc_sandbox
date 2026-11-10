package webserver

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// wiredServer starts a Server wired for one fixture home (import_test
// slices): real registry, its own op queue, no uninstall script needed
// yet. Returns the server and the fixture home.
func wiredServer(t *testing.T) (*Server, string) {
	t.Helper()
	home := t.TempDir()
	s := New(Options{BasePort: 0, Home: home})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, home
}

// postForm issues a form-encoded POST like HTMX does and returns status
// and body.
func postFormDoc(t *testing.T, url string, vals url.Values) (int, string) {
	t.Helper()
	resp, err := http.PostForm(url, vals)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 1024)
	for {
		n, err := resp.Body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return resp.StatusCode, string(buf)
}

// seedCanonicalRoot writes a canonical workspace root (sandbox_config.json
// + workspace/.git) and returns its path.
func seedCanonicalRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "my root")
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := `{"workspace_kind":"code","container_edition":"swdev","start_option":"web","vcs_tracking":"github","repo_url":"https://github.com/x/y"}`
	if err := os.WriteFile(filepath.Join(root, ".opencode_config", "sandbox_config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "workspace", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func newRegistry(home string) *registry.Registry {
	reg := &registry.Registry{Home: home}
	if err := reg.CreateWorkspacesFile(); err != nil {
		panic(err)
	}
	return reg
}

// GET /import renders the Import Workspace screen: path input whose form
// posts the server-side validation endpoint (spec §9).
func TestImportScreen(t *testing.T) {
	s, _ := wiredServer(t)
	resp := get(t, s.URL()+"import")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	for _, want := range []string{
		"Import a workspace",
		`hx-post="import-workspace"`,
		`name="path"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("import screen missing %q", want)
		}
	}
}

// A canonical folder gets the ordinary import summary flow: name prefilled
// from the folder basename, edition/modes/start option straight from the
// stored config, and the Register step posting the summary (nothing moved,
// no silent transform).
func TestImportCheckMatchingRoot(t *testing.T) {
	s, _ := wiredServer(t)
	root := seedCanonicalRoot(t)
	status, doc := postFormDoc(t, s.URL()+"import-workspace", url.Values{"path": {root}})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	for _, want := range []string{
		"Import summary",
		`name="name"`,
		`value="my_root"`,
		"swdev", // edition from the stored config
		"web",   // start option
		`hx-post="import-register"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("summary missing %q in:\n%s", want, doc)
		}
	}
	// The summary carries the path for the Register step, hidden.
	if !strings.Contains(doc, `name="path"`) {
		t.Fatalf("summary does not carry the path for Register")
	}
}

// An already-registered path hits the registered guard (ADR 0017) — even
// with an incomplete folder.
func TestImportCheckRegisteredGuard(t *testing.T) {
	s, home := wiredServer(t)
	root := t.TempDir() // deliberately not a matching structure
	newRegistry(home).AddWorkspace(registry.AddRequest{Name: "some", Path: root})
	_, doc := postFormDoc(t, s.URL()+"import-workspace", url.Values{"path": {root}})
	if !strings.Contains(doc, "Already registered") {
		t.Fatalf("registered guard missing:\n%s", doc)
	}
	if strings.Contains(doc, "Import summary") {
		t.Fatalf("registered path must not reach the summary flow:\n%s", doc)
	}
}

// A legacy project/-layout folder gets the informational prompt with the
// explicit "Convert to workspace layout" action — no silent transform
// (spec §2).
func TestImportCheckLegacyLayout(t *testing.T) {
	s, _ := wiredServer(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".opencode_config", "sandbox_config.json"), []byte(`{"project_kind":"code"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "project", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, doc := postFormDoc(t, s.URL()+"import-workspace", url.Values{"path": {root}})
	for _, want := range []string{
		"Import isn't possible here",
		"Convert to workspace layout",
		`hx-post="import-convert"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("legacy prompt missing %q:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "Import summary") {
		t.Fatalf("legacy folder must not reach the summary flow")
	}
}

// A non-matching folder gets the referrer prompt (New Workspace wizard).
func TestImportCheckNonMatching(t *testing.T) {
	s, _ := wiredServer(t)
	_, doc := postFormDoc(t, s.URL()+"import-workspace", url.Values{"path": {t.TempDir()}})
	if !strings.Contains(doc, "Import isn't possible here") {
		t.Fatalf("referrer prompt missing:\n%s", doc)
	}
	if strings.Contains(doc, "Convert to workspace layout") {
		t.Fatalf("non-matching folder must not offer the convert action")
	}
}

// A broken sandbox_config.json gets its own repair prompt.
func TestImportCheckBrokenConfig(t *testing.T) {
	s, _ := wiredServer(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".opencode_config", "sandbox_config.json"), []byte("{nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, doc := postFormDoc(t, s.URL()+"import-workspace", url.Values{"path": {root}})
	if !strings.Contains(doc, "not valid JSON") {
		t.Fatalf("broken-config prompt missing:\n%s", doc)
	}
}
