package webserver

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/providerid"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// --- Workspace Settings screen harness (#157) --------------------------------
//
// newCreateFixture provides the install-layout paths the screen touches
// (Dockerfile edition registry + configure-workspace.sh + runner seam);
// seedWorkspace adds the registry entry + sandbox_config.json.

// seedLegacyConfig writes a sandbox_config.json without the search_mode
// key — the legacy read rule (absent field reads websearch, §6).
func seedLegacyConfig(t *testing.T, root, edition string, modes []string, startOption string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, ".opencode_config"), 0o755); err != nil {
		t.Fatal(err)
	}
	quote := func(ss []string) string {
		out := make([]string, len(ss))
		for i, s := range ss {
			out[i] = `"` + s + `"`
		}
		return "[" + strings.Join(out, ", ") + "]"
	}
	doc := `{
    "container_edition": "` + edition + `",
    "container_modes": ` + quote(modes) + `,
    "start_option": "` + startOption + `",
    "cbm_auto_index": true,
    "cbm_auto_watch": true,
    "ai_provider": "none",
    "workspace_source": "empty",
    "repo_url": "",
    "setup_clone_complete": false,
    "setup_cbm_complete": false,
    "setup_skills_complete": false,
    "setup_complete": false,
    "version": "2.0"
}`
	if err := os.WriteFile(sandboxcfg.Path(root), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The Workspace Settings screen in Standard (default): Standard rows
// only — AI-Provider, VCS Tracking, remove-from-list behind the red
// confirm, the first-run setup retry link — plus the workspace selector
// dropdown (letter-tinted) and the next-start note. The Advanced rows
// are absent from the markup (the matrix hides them server-side —
// Standard never re-imposes them), and no settings form renders.
func TestWsSettingsScreenStandard(t *testing.T) {
	fx := newCreateFixture(t, nil)
	seedWorkspace(t, fx.home, "alpha", "swdev", nil)

	doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	for _, want := range []string{
		`data-screen="ws-settings"`,
		`data-ws="alpha"`,
		`data-role="ws-selector"`,                    // menu-entry workspace selector dropdown
		`style="--ed:#2f6fed"`,                       // letter-tinted avatar (Signal hue)
		`apply on <b>next start</b>`,                 // the next-start note
		`data-row="provider"`,                        // Standard row: AI-Provider
		`data-row="vcs"`,                             // Standard row: VCS Tracking
		`data-act="setup-retry"`,                     // first-run setup recovery (#161 link)
		`remove-from-list`,                           // remove targets the §9 route
		`data-confirm-title="Remove alpha from the workspace list?"`, // red confirm
		`presets, see Advanced`,                      // §6 helper wording
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("Standard settings screen missing %q:\n%s", want, doc)
		}
	}
	for _, hidden := range []string{
		`data-row="edition"`, `data-row="start_option"`, `data-row="proxy"`,
		`data-row="search_mode"`, `data-row="offline"`, `data-row="cbm_ui"`,
		`data-row="hil"`,
	} {
		if strings.Contains(doc, hidden) {
			t.Fatalf("Standard must hide the Advanced row %s (matrix §6):\n%s", hidden, doc)
		}
	}
}

// The same screen in Advanced reveals every stored option — the stored
// sandbox_config.json is the single truth, revealed as stored (§6).
func TestWsSettingsScreenAdvanced(t *testing.T) {
	fx := newCreateFixture(t, nil)
	seedWorkspace(t, fx.home, "alpha", "swdev", []string{"hil_mode"})
	root := rootOf(t, fx.home, "alpha")
	if err := sandboxcfg.PatchString(fx.home, sandboxcfg.Path(root), "start_option", "opencode"); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.PatchBool(fx.home, sandboxcfg.Path(root), "use_proxy", true); err != nil {
		t.Fatal(err)
	}
	postForm(t, fx.s.URL()+"settings", map[string]string{"display_mode": "advanced"})

	doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	for _, want := range []string{
		`data-row="edition"`,      // switchable, next-start apply
		`data-row="start_option"`, // the console/OpenCode-TUI/Web triad
		`data-row="proxy"`,        // stored on
		`data-row="search_mode"`,  // the triad with searxng preselected
		`data-row="offline"`,
		`data-row="cbm_ui"`,
		`data-row="hil"`,          // HIL row for every edition
		`value="swdev" selected`,  // stored edition selected
		`value="opencode" checked`, // stored start option checked
		`name="proxy" value="yes" checked`,
		`value="searxng" checked`, // §6 preselected
		`name="hil" value="on" checked`, // hil_mode recorded in modes
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("Advanced settings screen missing %q:\n%s", want, doc)
		}
	}
}

// The legacy read rule stays intact (§6): an absent search_mode key
// reads websearch and renders selected — the screen never guesses
// searxng from a preset, only from stored truth.
func TestWsSettingsScreenLegacySearchModeReadsWebsearch(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	seedLegacyConfig(t, root, "swdev", nil, "web")
	postForm(t, fx.s.URL()+"settings", map[string]string{"display_mode": "advanced"})

	doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	if !strings.Contains(doc, `value="websearch" checked`) {
		t.Fatalf("absent search_mode must read websearch:\n%s", doc)
	}
	if strings.Contains(doc, `value="searxng" checked`) {
		t.Fatalf("absent search_mode must not read searxng:\n%s", doc)
	}
}

// The selector: every registered workspace lists, the edited one is
// preselected, running state is shown, and the menu entry
// (GET /workspace-settings) jumps in with the most recent workspace
// preselected (§7).
func TestWsSettingsSelectorPreselect(t *testing.T) {
	fx := newCreateFixture(t, nil)
	seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	seedWorkspace(t, fx.home, "beta", "web", nil)
	root := rootOf(t, fx.home, "beta")
	reg := &registry.Registry{Home: fx.home}
	if err := reg.UpdateStatus(root, "running"); err != nil {
		t.Fatal(err)
	}

	doc := body(t, get(t, fx.s.URL()+"workspaces/beta/settings"))
	if !strings.Contains(doc, `data-ws="beta"`) || !strings.Contains(doc, `value="alpha"`) {
		t.Fatalf("selector must list every workspace:\n%s", doc)
	}
	if !strings.Contains(doc, `selected value="beta"`) && !strings.Contains(doc, `value="beta" selected`) {
		t.Fatalf("beta must be preselected:\n%s", doc)
	}
	if !strings.Contains(doc, "running") {
		t.Fatalf("running state must show:\n%s", doc)
	}

	// Menu entry: no name — the first (MRU) registry entry jumps in
	// preselected (§7).
	mru, _ := reg.AllOrdered()
	menu := body(t, get(t, fx.s.URL()+"workspace-settings"))
	if !strings.Contains(menu, `data-screen="ws-settings"`) ||
		!strings.Contains(menu, `data-ws="`+mru[0].Name+`"`) {
		t.Fatalf("menu entry must preselect the first (MRU) workspace %s:\n%s", mru[0].Name, menu)
	}
}

// Unknown workspace names refuse with 404 (registry name is the URL
// identity, §9); an empty registry renders the menu entry as the
// explicit empty state, never a dead end.
func TestWsSettingsUnknownWorkspaceAndEmpty(t *testing.T) {
	fx := newCreateFixture(t, nil)
	resp := get(t, fx.s.URL()+"workspaces/ghost/settings")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown workspace: status = %d, want 404", resp.StatusCode)
	}
	empty := body(t, get(t, fx.s.URL()+"workspace-settings"))
	if !strings.Contains(empty, "No workspaces registered") {
		t.Fatalf("empty registry must render the empty state:\n%s", empty)
	}
}

// --- POST /workspaces/{name}/settings (§9: next-start apply) ------------------

// The settings POST persists the next-start values into the stored
// sandbox_config.json — the single truth — as one atomic write. Fields
// the form does not carry stay untouched: a running container is never
// reconfigured, and Standard-hidden rows are never re-imposed.
func TestWsSettingsPostPersistsNextStart(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	postForm(t, fx.s.URL()+"settings", map[string]string{"display_mode": "advanced"})

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/settings", map[string]string{
		"edition": "embedded", "start_option": "console", "proxy": "yes",
		"search_mode": "none", "modes_present": "1", "offline": "on", "hil": "on",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ContainerEdition != "embedded" || cfg.StartOption != "console" ||
		!cfg.UseProxy || cfg.SearchMode != "none" {
		t.Fatalf("next-start values not persisted: %+v", cfg)
	}
	if len(cfg.ContainerModes) != 2 || cfg.ContainerModes[0] != "offline" || cfg.ContainerModes[1] != "hil_mode" {
		t.Fatalf("modes not persisted: %v", cfg.ContainerModes)
	}
}

// Validation refuses what start.sh would hard-exit on (ADR-0026):
// searxng + proxy, searxng + offline, offline + cbm_ui — nothing is
// written on a refusal. An unknown edition is refused too.
func TestWsSettingsPostValidation(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	before, err := os.ReadFile(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	postForm(t, fx.s.URL()+"settings", map[string]string{"display_mode": "advanced"})

	for name, form := range map[string]map[string]string{
		"searxng+proxy":  {"search_mode": "searxng", "proxy": "yes"},
		"searxng+offline": {"search_mode": "searxng", "modes_present": "1", "offline": "on"},
		"offline+cbm":    {"modes_present": "1", "offline": "on", "cbm_ui": "on"},
		"bad-edition":    {"edition": "ghost"},
		"bad-start":      {"start_option": "x11"},
		"bad-search":     {"search_mode": "bing"},
	} {
		resp := postForm(t, fx.s.URL()+"workspaces/alpha/settings", form)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200 (fragment re-render)", name, resp.StatusCode)
		}
		if doc := body(t, resp); !strings.Contains(doc, `role="alert"`) {
			t.Fatalf("%s: refusal must render the form error:\n%s", name, doc)
		}
	}
	after, _ := os.ReadFile(sandboxcfg.Path(root))
	if string(after) != string(before) {
		t.Fatalf("refused POST rewrote sandbox_config.json:\n%s", after)
	}
}

// Cross-editing Standard↔Advanced in either order just works (§6,
// asserted): an Advanced edit stays sticky when Standard rows are edited
// afterwards — a Standard-mode settings write (rows hidden) touches
// nothing, and the next Advanced edit starts from the stored truth.
func TestWsSettingsCrossEditStickiness(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)

	// Advanced edit: search none, proxy on.
	postForm(t, fx.s.URL()+"settings", map[string]string{"display_mode": "advanced"})
	resp := postForm(t, fx.s.URL()+"workspaces/alpha/settings", map[string]string{
		"search_mode": "none", "proxy": "yes",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("advanced edit: status = %d", resp.StatusCode)
	}

	// Standard mode: the settings rows are hidden, so the screen renders
	// no settings form — a re-render (any GET) never re-imposes.
	postForm(t, fx.s.URL()+"settings", map[string]string{"display_mode": "standard"})
	doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	if strings.Contains(doc, `hx-post="`) && strings.Contains(doc, "/settings") &&
		strings.Contains(doc, `data-row="edition"`) {
		t.Fatalf("Standard must render no settings rows:\n%s", doc)
	}

	// The stored truth survives the mode switch untouched.
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SearchMode != "none" || !cfg.UseProxy || cfg.ContainerEdition != "swdev" {
		t.Fatalf("mode switch disturbed the stored truth: %+v", cfg)
	}

	// Advanced again: the previous edits are sticky, a new edit applies
	// on top of them.
	postForm(t, fx.s.URL()+"settings", map[string]string{"display_mode": "advanced"})
	doc = body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	if !strings.Contains(doc, `value="none" checked`) || !strings.Contains(doc, `name="proxy" value="yes" checked`) {
		t.Fatalf("Advanced edits of Standard-hidden options must stay sticky:\n%s", doc)
	}
	resp = postForm(t, fx.s.URL()+"workspaces/alpha/settings", map[string]string{
		"start_option": "opencode",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second advanced edit: status = %d", resp.StatusCode)
	}
	cfg, err = sandboxcfg.Read(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StartOption != "opencode" || cfg.SearchMode != "none" || !cfg.UseProxy {
		t.Fatalf("second edit lost the first edit's values: %+v", cfg)
	}
}

// Confirm-gated writes refuse while an op is queued or running (§9):
// the settings POST answers with the busy refusal, the stored file stays
// untouched.
func TestWsSettingsPostBusyRefusal(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	release := make(chan struct{})
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "bash" {
			<-release
		}
		return "", nil
	}}
	s := startServerWith(t, home, fake)
	postForm(t, s.URL()+"settings", map[string]string{"display_mode": "advanced"})

	postMap(t, s.URL()+"workspaces/alpha/start", nil)
	waitForBusyCard(t, s, "alpha")

	before, err := os.ReadFile(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	resp := postForm(t, s.URL()+"workspaces/alpha/settings", map[string]string{"search_mode": "none"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("busy refusal: status = %d, want 200 (fragment re-render)", resp.StatusCode)
	}
	if doc := body(t, resp); !strings.Contains(doc, "queued or running") {
		t.Fatalf("busy refusal must render the busy notice:\n%s", doc)
	}
	after, _ := os.ReadFile(sandboxcfg.Path(root))
	if string(after) != string(before) {
		t.Fatalf("busy refusal rewrote sandbox_config.json")
	}
	close(release)
}

// --- POST /workspaces/{name}/vcs-credentials (§9: kind, host, token, replace) --

// The VCS save runs the configure-workspace.sh vcs-credentials step in
// the exact TUI argument vector (ADR-0007) as one queued op and mirrors
// the tracker into the stored vcs_tracking (the Standard row's write).
func TestWsVCSCredentialsSave(t *testing.T) {
	fx := newCreateFixture(t, func(name string, args []string) (string, error) {
		return "credential written\n", nil
	})
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "gitlab_self", "host": "git.example.com", "token": "t0k", "replace": "",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200:\n%s", resp.StatusCode, body(t, resp))
	}
	waitForNoOp(t, fx)

	// The script vector mirrors the TUI's vcsCredArgs — a fresh write
	// (no stored credentials) needs no --replace.
	want := []string{"vcs-credentials", "gitlab", root, "--token", "t0k", "--host", "git.example.com"}
	var cred bool
	for _, c := range fx.fake.Snapshot() {
		if c.Name != "bash" || len(c.Args) < 3 {
			continue
		}
		if strings.HasSuffix(c.Args[0], "configure-workspace.sh") && slices.Equal(c.Args[1:], want) {
			cred = true
		}
	}
	if !cred {
		t.Fatalf("vcs-credentials script vector wrong:\n%v", fx.fake.Snapshot())
	}

	// The tracker persists as the stored vcs_tracking label.
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.VCSTracking != "own GitLab" {
		t.Fatalf("vcs_tracking = %q, want own GitLab", cfg.VCSTracking)
	}
}

// First save without stored credentials writes the credential without
// --replace (the confirm gates only an overwrite); kind none unlinks —
// the tracker persists "none", no script runs.
func TestWsVCSCredentialsFreshAndNone(t *testing.T) {
	fx := newCreateFixture(t, func(name string, args []string) (string, error) {
		return "credential written\n", nil
	})
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "github", "token": "ghtok", "replace": "",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fresh save: status = %d", resp.StatusCode)
	}
	waitForNoOp(t, fx)
	cfg, _ := sandboxcfg.Read(sandboxcfg.Path(root))
	if cfg.VCSTracking != "github.com" {
		t.Fatalf("vcs_tracking = %q, want github.com", cfg.VCSTracking)
	}
	// github never takes --replace (the script refuses it).
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" && slices.Contains(c.Args, "--replace") {
			t.Fatalf("github save must not pass --replace: %v", c.Args)
		}
	}

	// none: tracker unlinked, no script call.
	postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{"kind": "none"})
	waitForNoOp(t, fx)
	cfg, _ = sandboxcfg.Read(sandboxcfg.Path(root))
	if cfg.VCSTracking != "none" {
		t.Fatalf("vcs_tracking = %q, want none", cfg.VCSTracking)
	}
	nBefore := len(fx.fake.Snapshot())
	postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{"kind": "none"})
	waitForNoOp(t, fx)
	if len(fx.fake.Snapshot()) != nBefore {
		t.Fatal("none must not run the script")
	}
}

// The replace-credentials confirm gates the credential write (§9): with
// stored credentials and replace unset, the save keeps the credentials —
// the tracker still moves, the script does not run; with replace=1 the
// confirm-gated write runs with --replace.
func TestWsVCSCredentialsReplaceGate(t *testing.T) {
	fx := newCreateFixture(t, func(name string, args []string) (string, error) {
		return "credential written\n", nil
	})
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	// Stored credentials: the tracker's host file exists.
	if err := os.MkdirAll(filepath.Join(root, ".git_local", "glab-cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git_local", "glab-cli", "config.yml"), []byte("x: y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.PatchString(fx.home, sandboxcfg.Path(root), "vcs_tracking", "own GitLab"); err != nil {
		t.Fatal(err)
	}

	// Screen renders the replace confirm on Save and the Keep option.
	doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	if !strings.Contains(doc, `data-confirm-title="Replace credentials?"`) ||
		!strings.Contains(doc, `name="replace" value="keep"`) {
		t.Fatalf("existing credentials must arm the confirm + keep option:\n%s", doc)
	}

	// replace unset: tracker moves, credentials kept (no script run).
	resp := postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "gitlab_self", "host": "git.example.com", "token": "t0k", "replace": "",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("keep save: status = %d", resp.StatusCode)
	}
	waitForNoOp(t, fx)
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" {
			t.Fatalf("keep must not write credentials: %v", c.Args)
		}
	}
	cfg, _ := sandboxcfg.Read(sandboxcfg.Path(root))
	if cfg.VCSTracking != "own GitLab" {
		t.Fatalf("vcs_tracking = %q", cfg.VCSTracking)
	}

	// replace=1 (the confirm's OK): the write runs with --replace.
	resp = postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", map[string]string{
		"kind": "gitlab_self", "host": "git.example.com", "token": "t0k", "replace": "1",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replace save: status = %d", resp.StatusCode)
	}
	waitForNoOp(t, fx)
	want := []string{"vcs-credentials", "gitlab", root, "--token", "t0k", "--host", "git.example.com", "--replace"}
	var ran bool
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" && len(c.Args) > 1 && slices.Equal(c.Args[1:], want) {
			ran = true
		}
	}
	if !ran {
		t.Fatalf("replace=1 must write the credentials with --replace:\n%v", fx.fake.Snapshot())
	}
}

// Validation: a token is required for a fresh non-none tracker write,
// the host for own GitLab / others (bare hostname), unknown kinds are
// refused — the fragment re-renders with the error, nothing runs.
func TestWsVCSCredentialsValidation(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)

	for name, form := range map[string]map[string]string{
		"token-required": {"kind": "github", "token": ""},
		"host-required":  {"kind": "other", "host": "", "token": "t"},
		"bad-host":       {"kind": "other", "host": "not a host", "token": "t"},
		"bad-kind":       {"kind": "ftp", "token": "t"},
	} {
		resp := postForm(t, fx.s.URL()+"workspaces/alpha/vcs-credentials", form)
		if doc := body(t, resp); !strings.Contains(doc, `role="alert"`) {
			t.Fatalf("%s: refusal must render the form error:\n%s", name, doc)
		}
	}
	waitForNoOp(t, fx)
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" {
			t.Fatalf("refused save must not run the script: %v", c.Args)
		}
	}
	cfg, _ := sandboxcfg.Read(sandboxcfg.Path(root))
	if cfg.VCSTracking != "none" {
		t.Fatalf("refused save must not move vcs_tracking: %q", cfg.VCSTracking)
	}
}

// waitForNoOp polls (bounded) until the workspace's queue drains — the
// op completion signal for the config/credential writes.
func waitForNoOp(t *testing.T, fx createFixture) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !fx.s.Busy() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("op never drained")
}

// --- POST /workspaces/{name}/providers (§9: add|edit|remove) -------------------

// seedProviderEntry writes one stored provider entry: the opencode.json
// v2 section plus its credential file (ADR 0025).
func seedProviderEntry(t *testing.T, root, id, name, endpoint, token string) {
	t.Helper()
	dir := filepath.Join(root, ".opencode_config")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := `{"providers": {"` + id + `": {"name": "` + name + `", "settings": {"baseURL": "` + endpoint + `"}}}}`
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
	if token != "" {
		credDir := filepath.Join(root, ".opencode_data", "credentials")
		if err := os.MkdirAll(credDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(credDir, id), []byte(token), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// The add flow writes the provider exactly as the TUI does — ai-provider
// (token) + opencode-config (merge) — and the entry list renders from
// the stored files once the op drained.
func TestWsProvidersAddFresh(t *testing.T) {
	var root string
	fx := newCreateFixture(t, func(name string, args []string) (string, error) {
		if name != "bash" || len(args) < 3 || root == "" {
			return "", nil
		}
		switch args[1] {
		case "ai-provider": // the credential file lands on disk
			id, tok := "", ""
			for i, a := range args {
				if a == "--name" {
					id = providerid.Slug(args[i+1])
				}
				if a == "--token" {
					tok = args[i+1]
				}
			}
			credDir := filepath.Join(root, ".opencode_data", "credentials")
			if err := os.MkdirAll(credDir, 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(credDir, id), []byte(tok), 0o644); err != nil {
				return "", err
			}
		case "opencode-config": // the opencode.json section lands on disk
			id, pName, endpoint := "", "", ""
			for i, a := range args {
				switch a {
				case "--id":
					id = args[i+1]
				case "--name":
					pName = args[i+1]
				case "--endpoint":
					endpoint = args[i+1]
				}
			}
			seedProviderEntry(t, root, id, pName, endpoint, "")
		}
		return "provider configured\n", nil
	})
	root = seedWorkspace(t, fx.home, "alpha", "swdev", nil)

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/providers", map[string]string{
		"action": "add", "name": "GWDG SAIA", "endpoint": "https://chat.gwdg.de/v1", "token": "tok123",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d:\n%s", resp.StatusCode, body(t, resp))
	}
	waitForNoOp(t, fx)

	var steps [][]string
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" && strings.HasSuffix(c.Args[0], "configure-workspace.sh") {
			steps = append(steps, c.Args[1:])
		}
	}
	if len(steps) != 2 ||
		!slices.Equal(steps[0], []string{"ai-provider", root, "--name", "GWDG SAIA", "--endpoint", "https://chat.gwdg.de/v1", "--token", "tok123"}) ||
		!slices.Equal(steps[1], []string{"opencode-config", root, "--id", "gwdg-saia", "--name", "GWDG SAIA", "--endpoint", "https://chat.gwdg.de/v1"}) {
		t.Fatalf("provider add vectors wrong:\n%v", steps)
	}

	doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	if !strings.Contains(doc, `data-entry="gwdg-saia"`) {
		t.Fatalf("entry list must render from the stored files:\n%s", doc)
	}
}

// A token-optional edit: saving an entry without a typed token merges
// the endpoint/name only — the stored credential stays untouched.
func TestWsProvidersEditTokenOptional(t *testing.T) {
	fx := newCreateFixture(t, func(name string, args []string) (string, error) {
		return "provider configured\n", nil
	})
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	seedProviderEntry(t, root, "gwdg-saia", "GWDG", "https://old/v1", "storedtok")

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/providers", map[string]string{
		"action": "edit", "orig": "gwdg-saia", "name": "GWDG SAIA", "endpoint": "https://new/v1", "token": "",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d:\n%s", resp.StatusCode, body(t, resp))
	}
	waitForNoOp(t, fx)
	var steps [][]string
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" && strings.HasSuffix(c.Args[0], "configure-workspace.sh") {
			steps = append(steps, c.Args[1:])
		}
	}
	if len(steps) != 1 ||
		!slices.Equal(steps[0], []string{"opencode-config", root, "--id", "gwdg-saia", "--name", "GWDG SAIA", "--endpoint", "https://new/v1"}) {
		t.Fatalf("token-optional edit must merge only:\n%v", steps)
	}
}

// The replace-credentials confirm gates an overwrite (§9): adding with a
// token when a credential exists renders the Keep/Replace round-trip and
// runs nothing; Keep merges the section only; Replace writes with
// --replace.
func TestWsProvidersReplaceConfirm(t *testing.T) {
	fx := newCreateFixture(t, func(name string, args []string) (string, error) {
		return "provider configured\n", nil
	})
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	seedProviderEntry(t, root, "gwdg-saia", "GWDG", "https://old/v1", "storedtok")

	form := map[string]string{
		"action": "add", "name": "GWDG SAIA", "endpoint": "https://new/v1", "token": "tok123",
	}
	resp := postForm(t, fx.s.URL()+"workspaces/alpha/providers", form)
	doc := body(t, resp)
	for _, want := range []string{
		`data-role="provider-confirm"`,
		`value="keep">Keep existing<`, `value="replace">Replace<`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("confirm block missing %q:\n%s", want, doc)
		}
	}
	waitForNoOp(t, fx)
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" {
			t.Fatalf("confirm must gate the write: %v", c.Args)
		}
	}

	// Keep existing: merge only, the stored token stays.
	form["decision"] = "keep"
	resp = postForm(t, fx.s.URL()+"workspaces/alpha/providers", form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("keep: status = %d", resp.StatusCode)
	}
	waitForNoOp(t, fx)
	var mergeOnly bool
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" && strings.HasSuffix(c.Args[0], "configure-workspace.sh") {
			mergeOnly = slices.Equal(c.Args[1:], []string{"opencode-config", root, "--id", "gwdg-saia", "--name", "GWDG SAIA", "--endpoint", "https://new/v1"})
		}
	}
	if !mergeOnly {
		t.Fatalf("keep must merge the section only:\n%v", fx.fake.Snapshot())
	}

	// Replace: the confirm-gated overwrite writes with --replace.
	form["decision"] = "replace"
	resp = postForm(t, fx.s.URL()+"workspaces/alpha/providers", form)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("replace: status = %d", resp.StatusCode)
	}
	waitForNoOp(t, fx)
	var replaced bool
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" && strings.HasSuffix(c.Args[0], "configure-workspace.sh") &&
			slices.Equal(c.Args[1:], []string{"ai-provider", root, "--name", "GWDG SAIA", "--endpoint", "https://new/v1", "--token", "tok123", "--replace"}) {
			replaced = true
		}
	}
	if !replaced {
		t.Fatalf("replace must write with --replace:\n%v", fx.fake.Snapshot())
	}
}

// Adding without a token and without an existing credential refuses —
// the TUI's empty-token rule carried 1:1.
func TestWsProvidersAddTokenRequired(t *testing.T) {
	fx := newCreateFixture(t, nil)
	seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	resp := postForm(t, fx.s.URL()+"workspaces/alpha/providers", map[string]string{
		"action": "add", "name": "Solo", "endpoint": "https://x/v1", "token": "",
	})
	if doc := body(t, resp); !strings.Contains(doc, "Token required") {
		t.Fatalf("token-required refusal missing:\n%s", doc)
	}
	waitForNoOp(t, fx)
	if n := len(fx.fake.Snapshot()); n != 0 {
		t.Fatalf("refused add must not run scripts: %d calls", n)
	}
}

// A rename moves the provider id: the new values are written first, then
// the old section is removed — a failure never leaves a half-renamed
// config half-eaten. The emptied token is read back from the stored
// credential (TUI #111 parity).
func TestWsProvidersRename(t *testing.T) {
	fx := newCreateFixture(t, func(name string, args []string) (string, error) {
		return "provider configured\n", nil
	})
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	seedProviderEntry(t, root, "gwdg-saia", "GWDG", "https://old/v1", "storedtok")

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/providers", map[string]string{
		"action": "edit", "orig": "gwdg-saia", "name": "Acme", "endpoint": "https://acme/v1", "token": "",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d:\n%s", resp.StatusCode, body(t, resp))
	}
	waitForNoOp(t, fx)
	var steps [][]string
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" && strings.HasSuffix(c.Args[0], "configure-workspace.sh") {
			steps = append(steps, c.Args[1:])
		}
	}
	want := [][]string{
		{"ai-provider", root, "--name", "Acme", "--endpoint", "https://acme/v1", "--token", "storedtok"},
		{"opencode-config", root, "--id", "acme", "--name", "Acme", "--endpoint", "https://acme/v1"},
		{"ai-provider-remove", root, "--id", "gwdg-saia"},
	}
	if len(steps) != len(want) {
		t.Fatalf("rename steps = %v, want %v", steps, want)
	}
	for i := range want {
		if !slices.Equal(steps[i], want[i]) {
			t.Fatalf("rename step %d = %v, want %v", i, steps[i], want[i])
		}
	}
}

// Remove deletes one entry behind its confirm popup — the ai-provider-remove
// vector; the last entry simply leaves the workspace with no providers.
func TestWsProvidersRemove(t *testing.T) {
	fx := newCreateFixture(t, func(name string, args []string) (string, error) {
		return "removed\n", nil
	})
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	seedProviderEntry(t, root, "gwdg-saia", "GWDG", "https://old/v1", "storedtok")

	doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	if !strings.Contains(doc, `data-confirm-title="Remove provider gwdg-saia?"`) {
		t.Fatalf("remove must carry its confirm popup:\n%s", doc)
	}

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/providers", map[string]string{
		"action": "remove", "orig": "gwdg-saia",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	waitForNoOp(t, fx)
	var ran bool
	for _, c := range fx.fake.Snapshot() {
		if c.Name == "bash" && strings.HasSuffix(c.Args[0], "configure-workspace.sh") &&
			slices.Equal(c.Args[1:], []string{"ai-provider-remove", root, "--id", "gwdg-saia"}) {
			ran = true
		}
	}
	if !ran {
		t.Fatalf("remove vector wrong:\n%v", fx.fake.Snapshot())
	}
	// The fake mirrors the script's outcome: entry + credential deleted.
	_ = os.Remove(filepath.Join(root, ".opencode_config", "opencode.json"))
	_ = os.Remove(filepath.Join(root, ".opencode_data", "credentials", "gwdg-saia"))
	doc = body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	if !strings.Contains(doc, "no entries — add a provider below") {
		t.Fatalf("the list must be empty after removal:\n%s", doc)
	}
}

// Catalog prefill: the AI-Provider select feeds from the global catalog
// (the prefill source, §8) — free-form add stays possible.
func TestWsProvidersCatalogPrefill(t *testing.T) {
	fx := newCreateFixture(t, nil)
	seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	d := catalogs.Read(fx.home)
	if err := d.AddProvider(catalogs.Provider{Name: "GWDG SAIA", Endpoint: "https://chat.gwdg.de/v1"}); err != nil {
		t.Fatal(err)
	}
	if err := catalogs.WriteFile(fx.home, d); err != nil {
		t.Fatal(err)
	}

	doc := body(t, get(t, fx.s.URL()+"workspaces/alpha/settings"))
	if !strings.Contains(doc, `data-endpoint="https://chat.gwdg.de/v1"`) {
		t.Fatalf("provider catalog prefill missing:\n%s", doc)
	}
}

// --- POST /workspaces/{name}/remove-from-list (§9) -----------------------------

// The red confirm's action deletes the registry entry only — files,
// configs and containers stay untouched (§7); a busy queue refuses.
func TestWsRemoveFromList(t *testing.T) {
	fx := newCreateFixture(t, nil)
	root := seedWorkspace(t, fx.home, "alpha", "swdev", nil)
	cfgPath := sandboxcfg.Path(root)

	resp := postForm(t, fx.s.URL()+"workspaces/alpha/remove-from-list", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	reg := &registry.Registry{Home: fx.home}
	if _, err := reg.ByName("alpha"); err == nil {
		t.Fatal("registry entry must be gone")
	}
	// Files, configs, containers untouched.
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("sandbox_config.json must stay: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("workspace root must stay: %v", err)
	}

	// A second removal (already gone) refuses with 404 — the name is the
	// URL identity.
	resp = postForm(t, fx.s.URL()+"workspaces/alpha/remove-from-list", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("gone workspace: status = %d, want 404", resp.StatusCode)
	}

	// A busy queue refuses the removal (§9) — nothing is deleted while
	// an op runs.
	release := make(chan struct{})
	fx2 := newCreateFixture(t, func(name string, args []string) (string, error) {
		if name == "bash" {
			<-release
		}
		return "", nil
	})
	seedWorkspace(t, fx2.home, "beta", "web", nil)
	postMap(t, fx2.s.URL()+"workspaces/beta/start", nil)
	waitForBusyCard(t, fx2.s, "beta")
	resp = postForm(t, fx2.s.URL()+"workspaces/beta/remove-from-list", nil)
	if doc := body(t, resp); !strings.Contains(doc, "queued or running") {
		t.Fatalf("busy refusal must render the notice:\n%s", doc)
	}
	if _, err := (&registry.Registry{Home: fx2.home}).ByName("beta"); err != nil {
		t.Fatalf("busy refusal must keep the entry: %v", err)
	}
	close(release)
}

// --- frontend wiring (#157) -----------------------------------------------------

// The shell script wires the settings screen: card settings icon jump,
// menu entry (the name-less alias), the selector dropdown, the reactive
// display-mode re-render, the setup-retry stub (#161) and both catalog
// prefill selects.
func TestFrontendWsSettingsWiring(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`workspace-settings`,  // the menu alias route
		`/settings`,           // the per-workspace screen route
		`ws-selector`,         // the dropdown drives the screen
		`displaychange`,       // the matrix re-renders on a mode switch
		`setup-retry`,         // the #161 link stub
		`provider-prefill`,    // AI-Provider catalog prefill
		`vcs-prefill`,         // VCS catalog prefill
		`"settings"`,          // the card settings icon jump
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q", want)
		}
	}
	css := body(t, get(t, s.URL()+"css/main.css"))
	for _, want := range []string{
		`.wssettings`, `.avatar`, `.selrow`, `.confirm-block`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("main.css missing %q", want)
		}
	}
}
