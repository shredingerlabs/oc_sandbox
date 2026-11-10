package smoke

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/webserver"
)

// A10 — WebUI global settings screens (#159): import with server-side
// validation (canonical roots register via the summary flow, legacy
// project/-layout folders prompt with the explicit convert action, the
// registered guard holds), export/restore named config backups with
// overwrite + restore confirmations (fixed list with the new filenames),
// and uninstall's typed confirmation with the TUI flag mapping. Asserted
// against a fixture home, a Fake runner and the real server on loopback.
func checkWebSettings(home, root string) Item {
	p := newPhase("A10", "WebUI global settings: import/convert flow, named config export/restore round trip, uninstall typed confirm + flag mapping, busy refusals")
	fix := filepath.Join(home, "A10")

	fake := &cmdrun.Fake{}
	s := webserver.New(webserver.Options{
		BasePort:    0,
		Home:        fix,
		InstallRoot: fix,
		Runner:      fake,
	})
	if err := s.Start(); err != nil {
		p.reasonf("start: %v", err)
		return p.finish()
	}
	defer func() { _ = s.Close() }()

	getBody := func(url string) string {
		resp, err := http.Get(url)
		if err != nil {
			p.reasonf("GET %s: %v", url, err)
			return ""
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			p.reasonf("GET %s read: %v", url, err)
		}
		return string(b)
	}
	post := func(path string, vals url.Values) string {
		resp, err := http.PostForm(s.URL()+path, vals)
		if err != nil {
			p.reasonf("POST %s: %v", path, err)
			return ""
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			p.reasonf("POST %s read: %v", path, err)
		}
		return string(b)
	}

	// --- Import: a canonical root registers via the summary flow ---
	rootOK := filepath.Join(fix, "workspaces", "alpha")
	if err := os.MkdirAll(filepath.Join(rootOK, ".opencode_config"), 0o755); err != nil {
		p.reasonf("seed: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(rootOK, ".opencode_config", "sandbox_config.json"),
		[]byte(`{"workspace_kind":"code","container_edition":"swdev","start_option":"web","vcs_tracking":"none"}`), 0o644); err != nil {
		p.reasonf("seed cfg: %v", err)
		return p.finish()
	}
	if err := os.MkdirAll(filepath.Join(rootOK, "workspace", ".git"), 0o755); err != nil {
		p.reasonf("seed git: %v", err)
		return p.finish()
	}
	confDir := filepath.Join(fix, ".config", "oc-sandbox")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		p.reasonf("confdir: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(confDir, "workspaces.json"), []byte(`{"version":"2.0","workspaces":[]}`), 0o644); err != nil {
		p.reasonf("seed registry: %v", err)
		return p.finish()
	}

	doc := getBody(s.URL() + "import")
	if !strings.Contains(doc, `hx-post="import-workspace"`) {
		p.reasonf("GET /import: screen form missing")
	}
	doc = post("import-workspace", url.Values{"path": {rootOK}})
	if !strings.Contains(doc, "Import summary") || !strings.Contains(doc, "swdev") {
		p.reasonf("canonical root: summary flow missing (edition not surfaced)")
	}
	doc = post("import-register", url.Values{"path": {rootOK}, "name": {"alpha"}})
	if !strings.Contains(doc, "Imported alpha") {
		p.reasonf("register: confirmation missing")
	}
	regData, err := os.ReadFile(filepath.Join(fix, ".config", "oc-sandbox", "workspaces.json"))
	if err != nil || !strings.Contains(string(regData), `"name": "alpha"`) || !strings.Contains(string(regData), `"container_status": "stopped"`) {
		p.reasonf("registry shape: %v / %s", err, regData)
	}

	// Registered guard beats the structure check.
	doc = post("import-workspace", url.Values{"path": {rootOK}})
	if !strings.Contains(doc, "Already registered") {
		p.reasonf("registered guard missing")
	}

	// --- Legacy folder: prompt with the convert action, then the
	// ordinary summary — no silent transform anywhere ---
	rootLegacy := filepath.Join(fix, "workspaces", "legacy")
	if err := os.MkdirAll(filepath.Join(rootLegacy, ".opencode_config"), 0o755); err != nil {
		p.reasonf("seed legacy: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(rootLegacy, ".opencode_config", "sandbox_config.json"),
		[]byte(`{"project_kind":"chat","project_source":"cloned"}`), 0o644); err != nil {
		p.reasonf("seed legacy cfg: %v", err)
		return p.finish()
	}
	if err := os.MkdirAll(filepath.Join(rootLegacy, "project", ".git"), 0o755); err != nil {
		p.reasonf("seed legacy git: %v", err)
		return p.finish()
	}
	doc = post("import-workspace", url.Values{"path": {rootLegacy}})
	if !strings.Contains(doc, "Convert to workspace layout") || strings.Contains(doc, "Import summary") {
		p.reasonf("legacy folder: convert prompt missing or silent transform")
	}
	doc = post("import-convert", url.Values{"path": {rootLegacy}})
	if !strings.Contains(doc, "Import summary") {
		p.reasonf("convert did not lead into the summary flow")
	}
	if _, err := os.Stat(filepath.Join(rootLegacy, "workspace")); err != nil {
		p.reasonf("convert: content dir not renamed: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(rootLegacy, ".opencode_config", "sandbox_config.json"))
	if !strings.Contains(string(data), "workspace_kind") || strings.Contains(string(data), "project_kind") {
		p.reasonf("convert: sandbox_config keys not rewritten: %s", data)
	}
	doc = post("import-register", url.Values{"path": {rootLegacy}, "name": {"legacy"}})
	if !strings.Contains(doc, "Imported legacy") {
		p.reasonf("register after convert failed")
	}

	// --- Export/restore: named server-side backup with overwrite
	// confirm; restore behind the confirm; round trip carries the new
	// filenames ---
	wsPath := filepath.Join(confDir, "workspaces.json")
	gcPath := filepath.Join(confDir, "global_config.json")
	if err := os.WriteFile(gcPath, []byte(`{"default_workspace_path":"`+filepath.Join(fix, "workspaces")+`","version":"2.0"}`), 0o644); err != nil {
		p.reasonf("seed global: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(confDir, "catalogs.json"), []byte(`{"version":"2.0","trackers":[],"providers":[]}`), 0o644); err != nil {
		p.reasonf("seed catalogs: %v", err)
		return p.finish()
	}
	doc = post("export-config", url.Values{"label": {"pre-release"}})
	if !strings.Contains(doc, "Exported") || !strings.Contains(doc, "workspaces.json") || !strings.Contains(doc, "catalogs.json") {
		p.reasonf("export: fixed list missing the new filenames: %s", doc)
	}
	if _, err := os.Stat(filepath.Join(confDir, "backups", "export-pre-release", "workspaces.json")); err != nil {
		p.reasonf("export snapshot missing workspaces.json: %v", err)
	}
	// Overwrite confirm: second export without confirm asks first.
	doc = post("export-config", url.Values{"label": {"pre-release"}})
	if !strings.Contains(doc, "Overwrite") {
		p.reasonf("overwrite confirm missing")
	}
	// Restore behind the confirm; without it nothing changes.
	if err := os.WriteFile(wsPath, []byte(`{"version":"2.0","workspaces":[]}`), 0o644); err != nil {
		p.reasonf("mutate registry: %v", err)
		return p.finish()
	}
	doc = post("restore-config", url.Values{"label": {"pre-release"}})
	if !strings.Contains(doc, "confirmation") && !strings.Contains(doc, "Restore failed") {
		p.reasonf("restore without confirm did not refuse")
	}
	restored, _ := os.ReadFile(wsPath)
	if !strings.Contains(string(restored), `"workspaces":[]`) {
		p.reasonf("restore ran without the confirmation popup: %s", restored)
	}
	doc = post("restore-config", url.Values{"label": {"pre-release"}, "confirm": {"1"}})
	if !strings.Contains(doc, "Restored") {
		p.reasonf("confirmed restore failed: %s", doc)
	}
	restored, _ = os.ReadFile(wsPath)
	if !strings.Contains(string(restored), `"name": "alpha"`) {
		p.reasonf("restore round trip failed: %s", restored)
	}

	// --- Uninstall: typed confirmation gates the script; toggles map to
	// the script flags exactly like the TUI (always --force); while a
	// script run holds, op-launching POSTs refuse ---
	var scriptCalls int32
	release := make(chan struct{})
	fake.Script = func(name string, args []string) (string, error) {
		if atomic.AddInt32(&scriptCalls, 1) == 1 {
			<-release // hold the first run — the busy fixture
		}
		return "", nil
	}
	post("uninstall", url.Values{"confirm_text": {"nope"}}) //nolint:errcheck
	if snap := fake.Snapshot(); len(snap) != 0 {
		p.reasonf("uninstall ran without the typed confirmation (%d calls)", len(snap))
	}
	post("uninstall", url.Values{"confirm_text": {"uninstall"}}) //nolint:errcheck
	doc = post("export-config", url.Values{"label": {"during"}})
	if !strings.Contains(doc, "queued or running") {
		p.reasonf("busy refusal missing while the uninstall op runs: %s", doc)
	}
	close(release)
	waitCalls(1, fake)
	calls := fake.Snapshot()
	if len(calls) != 1 {
		p.reasonf("typed-confirm uninstall: script not invoked")
	} else {
		call := calls[0]
		want := filepath.Join(fix, "scripts", "uninstall.sh")
		if call.Name != "bash" || len(call.Args) < 2 || call.Args[0] != want ||
			strings.Join(call.Args[1:], " ") != "--force --no-symlinks" {
			p.reasonf("uninstall argv = %v %v, want bash <uninstall.sh> --force --no-symlinks", call.Name, call.Args)
		}
	}
	// The toggled run: wait for the first uninstall op to drain (a busy
	// refusal would swallow the second POST), then assert call #2.
	for i := 0; i < 250 && s.Busy(); i++ {
		sleepMillisSmoke(2)
	}
	post("uninstall", url.Values{"confirm_text": {"uninstall"}, "remove_symlinks": {"1"}, "remove_shortcuts": {"1"}, "remove_config": {"1"}}) //nolint:errcheck
	waitCalls(2, fake)
	calls = fake.Snapshot()
	if len(calls) == 2 {
		got := strings.Join(calls[1].Args[1:], " ")
		if got != "--force --remove-shortcuts --remove-config" {
			p.reasonf("toggled uninstall flags = %q", got)
		}
	} else {
		p.reasonf("toggled uninstall: script not invoked (%d calls)", len(calls))
	}

	return p.finish()
}

// waitCalls polls until the runner recorded n calls (ops run on a
// goroutine) — bounded so the harness never hangs. Reads through the
// Fake's lock (Snapshot), never the bare slice.
func waitCalls(n int, fake *cmdrun.Fake) {
	for i := 0; i < 250 && len(fake.Snapshot()) < n; i++ {
		sleepMillisSmoke(2)
	}
}

func sleepMillisSmoke(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }
