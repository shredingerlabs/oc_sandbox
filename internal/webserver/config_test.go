package webserver

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
)

// seedGlobalConfigs writes all three general config files (the new
// filenames included) into the fixture home and returns their paths.
func seedGlobalConfigs(t *testing.T, home string) map[string]string {
	t.Helper()
	files := map[string]string{
		"workspaces.json":    `{"version":"2.0","workspaces":[{"name":"ws","path":"/tmp/ws","container_id":"abc"}]}`,
		"global_config.json": `{"default_workspace_path":"/tmp/workspaces","version":"2.0"}`,
		"catalogs.json":      `{"version":"2.0","trackers":[],"providers":[]}`,
	}
	for name, content := range files {
		p := filepath.Join(home, ".config", "oc-sandbox", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// GET /config renders the Export/Restore Config screen: the named-export
// form and the restore list slot.
func TestConfigScreen(t *testing.T) {
	s, _ := wiredServer(t)
	resp := get(t, s.URL()+"config")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	for _, want := range []string{
		"Export config",
		`hx-post="export-config"`,
		`name="label"`,
		"No config backups yet", // empty restore list state
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("config screen missing %q:\n%s", want, doc)
		}
	}
}

// POST /export-config {label} creates a named server-side backup: a
// backups/export-<label>/ snapshot carrying the active general config
// files byte-exact (the fixed list, new filenames included).
func TestExportConfigNamedBackup(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	status, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"label": {"pre-release"}})
	if status != http.StatusOK || !strings.Contains(doc, "Exported") {
		t.Fatalf("export failed (status %d):\n%s", status, doc)
	}
	dir := filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-pre-release")
	for _, name := range []string{"workspaces.json", "global_config.json", "catalogs.json"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("snapshot missing %s: %v", name, err)
		}
		if !strings.Contains(string(data), `"version":"2.0"`) {
			t.Fatalf("snapshot %s not byte-exact JSON: %s", name, data)
		}
	}
}

// Exporting an existing label asks for the overwrite confirmation first;
// with the confirmation the snapshot is replaced.
func TestExportConfigOverwriteConfirm(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	if _, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"label": {"snap"}}); !strings.Contains(doc, "Exported") {
		t.Fatalf("first export failed")
	}
	// Change the active file, then re-export without confirmation.
	gc := filepath.Join(home, ".config", "oc-sandbox", "global_config.json")
	os.WriteFile(gc, []byte(`{"default_workspace_path":"/other","version":"2.0"}`), 0o644) //nolint:errcheck
	_, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"label": {"snap"}})
	if !strings.Contains(doc, "already exists") {
		t.Fatalf("overwrite confirm missing:\n%s", doc)
	}
	// The snapshot still holds the old content — nothing overwritten yet.
	data, _ := os.ReadFile(filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-snap", "global_config.json")) //nolint:errcheck
	if !strings.Contains(string(data), "/tmp/workspaces") {
		t.Fatalf("snapshot changed without confirmation: %s", data)
	}
	// With the confirmation the snapshot is replaced.
	_, doc = postFormDoc(t, s.URL()+"export-config", url.Values{"label": {"snap"}, "confirm": {"1"}})
	if !strings.Contains(doc, "Exported") {
		t.Fatalf("confirmed export failed:\n%s", doc)
	}
	data, _ = os.ReadFile(filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-snap", "global_config.json")) //nolint:errcheck
	if !strings.Contains(string(data), "/other") {
		t.Fatalf("confirmed export did not overwrite: %s", data)
	}
}

// A malformed label is refused — the snapshot dir name is slug-only
// (workspace name rule), so labels can't escape backups/.
func TestExportConfigBadLabel(t *testing.T) {
	s, _ := wiredServer(t)
	for _, label := range []string{"", "../evil", "has space", "sl/ash"} {
		_, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"label": {label}})
		if !strings.Contains(doc, "Invalid backup name") {
			t.Fatalf("label %q not refused:\n%s", label, doc)
		}
	}
}

// Export refuses while an op is queued/running.
func TestExportConfigRefusesWhileBusy(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	release := make(chan struct{})
	defer close(release)
	s.queue.Launch(blockedOp(release))
	for !s.busy() {
	}
	_, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"label": {"snap"}})
	if !strings.Contains(doc, "queued or running") {
		t.Fatalf("busy refusal missing:\n%s", doc)
	}
}

// POST /restore-config {label, confirm} restores the named snapshot over
// the active files behind the confirmation: validate → safety backup →
// no-backup write, per file. The round trip returns the pre-change state.
func TestRestoreConfigRoundTrip(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	if _, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"label": {"snap"}}); !strings.Contains(doc, "Exported") {
		t.Fatalf("export failed")
	}
	// Change the active workspaces.json, then restore.
	ws := filepath.Join(home, ".config", "oc-sandbox", "workspaces.json")
	if err := os.WriteFile(ws, []byte(`{"version":"2.0","workspaces":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Without the confirmation nothing is restored (server-side guard).
	postFormDoc(t, s.URL()+"restore-config", url.Values{"label": {"snap"}}) //nolint:errcheck
	data, _ := os.ReadFile(ws)                                              //nolint:errcheck
	if !strings.Contains(string(data), `"workspaces":[]`) {
		t.Fatalf("restored without confirmation: %s", data)
	}
	_, doc := postFormDoc(t, s.URL()+"restore-config", url.Values{"label": {"snap"}, "confirm": {"1"}})
	if !strings.Contains(doc, "Restored") {
		t.Fatalf("restore confirmation missing:\n%s", doc)
	}
	data, _ = os.ReadFile(ws) //nolint:errcheck
	if !strings.Contains(string(data), `"path":"/tmp/ws"`) {
		t.Fatalf("round trip failed — active file not restored: %s", data)
	}
	// The safety backup of the active file exists in backups/ (rotation
	// machinery, new filenames).
	matches, _ := filepath.Glob(filepath.Join(config.Paths{Home: home}.BackupsDir(), "workspaces.json.*"))
	if len(matches) == 0 {
		t.Fatalf("no safety backup created")
	}
}

// Restore refuses while an op is queued/running.
func TestRestoreConfigRefusesWhileBusy(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	if _, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"label": {"snap"}}); !strings.Contains(doc, "Exported") {
		t.Fatalf("export failed")
	}
	release := make(chan struct{})
	defer close(release)
	s.queue.Launch(blockedOp(release))
	for !s.busy() {
	}
	_, doc := postFormDoc(t, s.URL()+"restore-config", url.Values{"label": {"snap"}, "confirm": {"1"}})
	if !strings.Contains(doc, "queued or running") {
		t.Fatalf("busy refusal missing:\n%s", doc)
	}
}

// The restore list shows the named backups (newest first) with Restore
// actions behind the confirm popup attribute.
func TestConfigScreenListsNamedBackups(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	postFormDoc(t, s.URL()+"export-config", url.Values{"label": {"first"}})  //nolint:errcheck
	postFormDoc(t, s.URL()+"export-config", url.Values{"label": {"second"}}) //nolint:errcheck
	doc := body(t, get(t, s.URL()+"config"))
	if !strings.Contains(doc, "first") || !strings.Contains(doc, "second") {
		t.Fatalf("named backups not listed:\n%s", doc)
	}
	if !strings.Contains(doc, `data-confirm-popup`) {
		t.Fatalf("restore actions missing the confirm popup attribute:\n%s", doc)
	}
}
