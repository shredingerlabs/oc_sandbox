package webserver

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// POST /import-register performs the registry-only change from the
// summary: the workspace is added as stopped; no folder content is
// touched. The registry file carries the entry afterwards.
func TestImportRegister(t *testing.T) {
	s, home := wiredServer(t)
	root := seedCanonicalRoot(t)
	newRegistry(home) // workspaces.json exists (first-run setup ran)
	status, doc := postFormDoc(t, s.URL()+"import-register", url.Values{
		"path": {root}, "name": {"imported"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if !strings.Contains(doc, "Imported imported") {
		t.Fatalf("confirmation missing:\n%s", doc)
	}
	reg := &registry.Registry{Home: home}
	ws, err := reg.ByName("imported")
	if err != nil {
		t.Fatalf("registry entry missing: %v", err)
	}
	if ws.Path != filepath.Clean(root) {
		t.Fatalf("Path = %q, want %q", ws.Path, root)
	}
	if ws.ContainerStatus != "stopped" {
		t.Fatalf("ContainerStatus = %q, want stopped", ws.ContainerStatus)
	}
}

// blockedOp is an op that stays running until release closes — the busy
// fixture for refusal tests.
func blockedOp(release chan struct{}) webOp {
	return webOp{Key: "settings", Run: func(emit func(string)) error {
		<-release
		return nil
	}}
}

// The register step refuses while any op is queued or running (taskmgr
// semantics mirrored): busy notice, nothing registered.
func TestImportRegisterRefusesWhileBusy(t *testing.T) {
	s, home := wiredServer(t)
	root := seedCanonicalRoot(t)
	newRegistry(home)
	release := make(chan struct{})
	s.queue.Launch(blockedOp(release))
	for !s.busy() {
		time.Sleep(time.Millisecond)
	}
	status, doc := postFormDoc(t, s.URL()+"import-register", url.Values{
		"path": {root}, "name": {"imported"},
	})
	if status != http.StatusOK || !strings.Contains(doc, "queued or running") {
		t.Fatalf("busy refusal missing (status %d):\n%s", status, doc)
	}
	if _, err := (&registry.Registry{Home: home}).ByName("imported"); err == nil {
		t.Fatalf("registered despite busy refusal")
	}
	// After the op drains, the register goes through.
	close(release)
	for s.busy() {
		time.Sleep(time.Millisecond)
	}
	_, doc = postFormDoc(t, s.URL()+"import-register", url.Values{
		"path": {root}, "name": {"imported"},
	})
	if !strings.Contains(doc, "Imported imported") {
		t.Fatalf("register after busy period failed:\n%s", doc)
	}
}

// POST /import-convert runs the same per-root converter the first-start
// migration uses (stages 1–2) and then imports normally — the response
// is the ordinary registration summary.
func TestImportConvertThenSummary(t *testing.T) {
	s, home := wiredServer(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := `{"project_kind":"code","project_source":"cloned","container_edition":"swdev","start_option":"web","vcs_tracking":"github"}`
	if err := os.WriteFile(filepath.Join(root, ".opencode_config", "sandbox_config.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "project", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	newRegistry(home)
	status, doc := postFormDoc(t, s.URL()+"import-convert", url.Values{"path": {root}})
	if status != http.StatusOK || !strings.Contains(doc, "Import summary") {
		t.Fatalf("convert did not lead to the summary flow (status %d):\n%s", status, doc)
	}
	// Stages 1–2 actually ran: content dir renamed, keys rewritten.
	if _, err := os.Stat(filepath.Join(root, "workspace", ".git")); err != nil {
		t.Fatalf("project/ not renamed to workspace/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "project")); err == nil {
		t.Fatalf("legacy project/ still present")
	}
	data, err := os.ReadFile(filepath.Join(root, ".opencode_config", "sandbox_config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "project_kind") || !strings.Contains(string(data), "workspace_kind") {
		t.Fatalf("sandbox_config keys not rewritten: %s", data)
	}
}

// A failed conversion reports the error and keeps the folder untouched.
func TestImportConvertFailure(t *testing.T) {
	s, home := wiredServer(t)
	root := t.TempDir()
	// Both layouts present: the converter refuses (stage 1 guard).
	if err := os.MkdirAll(filepath.Join(root, "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "workspace"), 0o755); err != nil {
		t.Fatal(err)
	}
	newRegistry(home)
	_, doc := postFormDoc(t, s.URL()+"import-convert", url.Values{"path": {root}})
	if !strings.Contains(doc, "Converting the legacy layout failed") {
		t.Fatalf("failure notice missing:\n%s", doc)
	}
}
