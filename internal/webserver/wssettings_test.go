package webserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
