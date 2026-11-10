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

// GET /config renders the Export/Restore Config screen: the destination
// export form (prefilled with the machinery's backups-level default)
// and the restore list slot (empty state wording).
func TestConfigScreen(t *testing.T) {
	s, home := wiredServer(t)
	resp := get(t, s.URL()+"config")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	for _, want := range []string{
		"Export config",
		`hx-post="export-config"`,
		`name="dest"`,
		`value="` + filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-current") + `"`,
		`name="src"`,            // the restore-from-folder input (R3Q2)
		"No config backups yet", // empty restore list state
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("config screen missing %q:\n%s", want, doc)
		}
	}
}

// POST /export-config {dest} creates the server-side backup at the typed
// destination: a backups/export-<label>/ snapshot carrying the active
// general config files byte-exact (the fixed list, new filenames included).
func TestExportConfigNamedBackup(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	dest := filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-pre-release")
	status, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {dest}})
	if status != http.StatusOK || !strings.Contains(doc, "Exported") {
		t.Fatalf("export failed (status %d):\n%s", status, doc)
	}
	for _, name := range []string{"workspaces.json", "global_config.json", "catalogs.json"} {
		data, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			t.Fatalf("snapshot missing %s: %v", name, err)
		}
		if !strings.Contains(string(data), `"version":"2.0"`) {
			t.Fatalf("snapshot %s not byte-exact JSON: %s", name, data)
		}
	}
}

// Exporting an existing destination asks for the overwrite confirmation
// first; with the confirmation the snapshot is replaced.
func TestExportConfigOverwriteConfirm(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	dest := filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-snap")
	if _, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {dest}}); !strings.Contains(doc, "Exported") {
		t.Fatalf("first export failed")
	}
	// Change the active file, then re-export without confirmation.
	gc := filepath.Join(home, ".config", "oc-sandbox", "global_config.json")
	os.WriteFile(gc, []byte(`{"default_workspace_path":"/other","version":"2.0"}`), 0o644) //nolint:errcheck
	_, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {dest}})
	if !strings.Contains(doc, "already exists") {
		t.Fatalf("overwrite confirm missing:\n%s", doc)
	}
	// The snapshot still holds the old content — nothing overwritten yet.
	data, _ := os.ReadFile(filepath.Join(dest, "global_config.json")) //nolint:errcheck
	if !strings.Contains(string(data), "/tmp/workspaces") {
		t.Fatalf("snapshot changed without confirmation: %s", data)
	}
	// With the confirmation the snapshot is replaced.
	_, doc = postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {dest}, "confirm": {"1"}})
	if !strings.Contains(doc, "Exported") {
		t.Fatalf("confirmed export failed:\n%s", doc)
	}
	data, _ = os.ReadFile(filepath.Join(dest, "global_config.json")) //nolint:errcheck
	if !strings.Contains(string(data), "/other") {
		t.Fatalf("confirmed export did not overwrite: %s", data)
	}
}

// A malformed destination is refused — the machinery stays in the
// backups dir (label slug, export- prefix); the escape-hatch input is
// the restore side, never the export side.
func TestExportConfigBadLabel(t *testing.T) {
	s, home := wiredServer(t)
	backups := config.Paths{Home: home}.BackupsDir()
	type item struct{ dest, want string }
	d := filepath.Join(home, "..", "evil")
	outside := filepath.Join(filepath.Dir(backups), "export-evil")
	for _, it := range []item{
		{"", "Destination folder is required"},
		{d, "must lie directly in"},
		{filepath.Join(backups, "export-has space"), "Invalid backup name"},
		{outside, "must lie directly in"},
		{filepath.Join(backups, "brenamed"), "export- prefix"},
	} {
		_, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {it.dest}})
		if !strings.Contains(doc, it.want) {
			t.Fatalf("dest %q not refused as %q:\n%s", it.dest, it.want, doc)
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
	_, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-snap")}})
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
	if _, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-snap")}}); !strings.Contains(doc, "Exported") {
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

// The restore input accepts any folder holding the config files
// (R3Q2): the typed path validates like a named backup — missing
// folder, valid-JSON check, safety backup, write.
func TestRestoreConfigFromFolder(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	src := t.TempDir()
	// an invalid folder: none of the config files inside
	_, doc := postFormDoc(t, s.URL()+"restore-config", url.Values{"src": {src}, "confirm": {"1"}})
	if !strings.Contains(doc, "No config files") {
		t.Fatalf("empty restore folder not refused:\n%s", doc)
	}
	// a valid folder: a prior export works as-is (the safety backup and
	// the notice ride the named path).
	os.WriteFile(filepath.Join(src, "workspaces.json"), []byte(`{"version":"2.0","workspaces":[{"name":"fromfolder","path":"/tmp/ff"}]}`), 0o644) //nolint:errcheck
	_, doc = postFormDoc(t, s.URL()+"restore-config", url.Values{"src": {src}, "confirm": {"1"}})
	if !strings.Contains(doc, "Restored workspaces.json from") {
		t.Fatalf("folder restore failed:\n%s", doc)
	}
	data, _ := os.ReadFile(filepath.Join(home, ".config", "oc-sandbox", "workspaces.json")) //nolint:errcheck
	if !strings.Contains(string(data), "fromfolder") {
		t.Fatalf("folder restore did not write the active file: %s", data)
	}
	// invalid JSON in the chosen folder refuses before any write.
	os.WriteFile(filepath.Join(src, "global_config.json"), []byte("{not json"), 0o644) //nolint:errcheck
	_, doc = postFormDoc(t, s.URL()+"restore-config", url.Values{"src": {src}, "confirm": {"1"}})
	if !strings.Contains(doc, "not valid JSON") {
		t.Fatalf("invalid JSON folder not refused:\n%s", doc)
	}
}

// Restore refuses while an op is queued/running.
func TestRestoreConfigRefusesWhileBusy(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	if _, doc := postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-snap")}}); !strings.Contains(doc, "Exported") {
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

// The restore screen: the named-backup list (newest first) with Restore
// actions behind the confirm popup attribute, plus the always-present
// restore-from-folder input + button (R3Q2).
func TestConfigScreenListsNamedBackups(t *testing.T) {
	s, home := wiredServer(t)
	seedGlobalConfigs(t, home)
	postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-first")}})  //nolint:errcheck
	postFormDoc(t, s.URL()+"export-config", url.Values{"dest": {filepath.Join(config.Paths{Home: home}.BackupsDir(), "export-second")}}) //nolint:errcheck
	doc := body(t, get(t, s.URL()+"config"))
	if !strings.Contains(doc, "first") || !strings.Contains(doc, "second") {
		t.Fatalf("named backups not listed:\n%s", doc)
	}
	if !strings.Contains(doc, `data-confirm-popup`) {
		t.Fatalf("restore actions missing the confirm popup attribute:\n%s", doc)
	}
	if !strings.Contains(doc, `name="src"`) || !strings.Contains(doc, "Restore config</button>") {
		t.Fatalf("restore input + button missing:\n%s", doc)
	}
}
