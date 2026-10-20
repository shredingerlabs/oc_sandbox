package projectsettings

import (
	"os"
	"path/filepath"
	"testing"
)

// v2-shaped opencode.json (providers.<id> with settings.baseURL) is the
// primary source; the legacy v1 provider.<id> shape (options.baseURL) is
// still displayed pre-migration (ADR 0025).
func TestReadEntriesV2AndLegacyShapes(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	dir := filepath.Join(root, ".opencode_config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{"providers":{"v2-api":{"name":"V2 API","settings":{"baseURL":"https://v2.example.com/v1"}}},
	         "provider":{"old-api":{"name":"Old API","options":{"baseURL":"https://old.example.com/v1"}}}}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := readEntries(root)
	if len(entries) != 2 {
		t.Fatalf("entries = %d (%v), want 2", len(entries), entries)
	}
	byID := map[string]entry{}
	for _, e := range entries {
		byID[e.id] = e
	}
	if byID["v2-api"].endpoint != "https://v2.example.com/v1" || byID["v2-api"].name != "V2 API" {
		t.Fatalf("v2 entry wrong: %+v", byID["v2-api"])
	}
	if byID["old-api"].endpoint != "https://old.example.com/v1" {
		t.Fatalf("legacy entry endpoint wrong: %+v", byID["old-api"])
	}
	if !byID["v2-api"].orphan || !byID["old-api"].orphan {
		t.Fatalf("entries without stored credentials must be orphaned: %+v", entries)
	}
}

// Same id present in both shapes: the v2 providers section wins, and the
// entry is not orphaned if any stored credential exists (legacy
// auth.json fallback). A credential file outranks the legacy auth.json
// value. HasCredential accepts both sources.
func TestReadEntriesCredentialSources(t *testing.T) {
	e := newEnv(t, "alpha")
	root := e.roots["alpha"]
	dir := filepath.Join(root, ".opencode_config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Same id in both shapes; v2 endpoint must win for the render.
	doc := `{"providers":{"dual":{"name":"Duel","settings":{"baseURL":"https://v2.example.com/v1"}}},
	         "provider":{"dual":{"name":"Duel","options":{"baseURL":"https://old.example.com/v1"}}}}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := readEntries(root)
	if len(entries) != 1 || entries[0].endpoint != "https://v2.example.com/v1" {
		t.Fatalf("v2 section must outrank the legacy shape: %+v", entries)
	}

	// Legacy-only credential: not orphaned, token visible.
	dataDir := filepath.Join(root, ".opencode_data")
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "auth.json"),
		[]byte(`{"dual": {"type": "api", "key": "legacy-tok"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	entries = readEntries(root)
	if entries[0].token != "legacy-tok" || entries[0].orphan {
		t.Fatalf("legacy auth.json fallback broken: %+v", entries[0])
	}
	if !HasCredential(root, "dual") {
		t.Fatalf("HasCredential must see the legacy auth.json entry")
	}

	// A credential file outranks the legacy value; HasCredential sees it.
	credDir := filepath.Join(dataDir, "credentials")
	if err := os.MkdirAll(credDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(credDir, "dual"), []byte("file-tok\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries = readEntries(root)
	if entries[0].token != "file-tok" {
		t.Fatalf("credential file must outrank legacy auth.json: %+v", entries[0])
	}
	if !HasCredential(root, "dual") {
		t.Fatalf("authHasID must see the credential file")
	}
	if HasCredential(root, "absent") {
		t.Fatalf("HasCredential reported a credential for an unknown id")
	}
}
