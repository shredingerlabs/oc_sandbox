package webserver

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// Presentation-matrix harness (#152, spec §6): the Standard/Advanced
// matrix as a data table — one entry per (screen, mode) listing the row
// keys shown and hidden, plus whether the presets hint closes the view.
// Per-screen tickets (#153, #155–#157) extend the table and the seed
// matrix without new test code.
var matrixCases = []struct {
	screen     string
	mode       string
	wantShown  []string
	wantHidden []string
	wantHint   bool
}{
	{"ws-settings", config.DisplayStandard, nil,
		[]string{"edition", "start_option", "proxy", "search_mode", "offline", "cbm_ui", "hil"},
		true},
	{"ws-settings", config.DisplayAdvanced,
		[]string{"edition", "start_option", "proxy", "search_mode", "offline", "cbm_ui", "hil"},
		nil, false},
}

func TestPresentationMatrix(t *testing.T) {
	for _, tc := range matrixCases {
		for _, key := range tc.wantShown {
			if !RowVisible(tc.mode, tc.screen, key) {
				t.Errorf("%s/%s: row %q hidden, want shown", tc.screen, tc.mode, key)
			}
		}
		for _, key := range tc.wantHidden {
			if RowVisible(tc.mode, tc.screen, key) {
				t.Errorf("%s/%s: row %q shown, want hidden", tc.screen, tc.mode, key)
			}
		}
		hint := tc.mode == config.DisplayStandard && len(HiddenByStandard(tc.screen)) > 0
		if hint != tc.wantHint {
			t.Errorf("%s/%s: presets hint = %v, want %v", tc.screen, tc.mode, hint, tc.wantHint)
		}
	}
}

// Presets, not pinning: Standard hides rows but never re-imposes
// values — rows unknown to the matrix stay visible (default-open), and
// unknown modes behave as Standard (fail safe to the simpler view).
func TestMatrixDefaultOpenAndModeFallback(t *testing.T) {
	if !RowVisible(config.DisplayStandard, "ws-settings", "ai_provider") {
		t.Error("unregistered row must stay visible in Standard")
	}
	if !RowVisible(config.DisplayAdvanced, "ws-settings", "ai_provider") {
		t.Error("unregistered row must stay visible in Advanced")
	}
	if RowVisible("bogus", "ws-settings", "search_mode") {
		t.Error("unknown mode must behave as Standard (hidden advanced-only row)")
	}
}

// Helper wording (spec §6): rows Standard hides point at Advanced —
// never "fixed"/"only embedded".
// Presets, not pinning (spec §6/#152): Advanced edits of
// Standard-hidden options are sticky — a display-mode switch goes
// through global_config.json only and never rewrites the stored
// sandbox_config.json, so Standard never re-imposes hidden values. The
// sandbox_config legacy read rule rides the same fixture: an absent
// search_mode reads as websearch (pre-#137 workspaces).
func TestPresetsNotPinningModeSwitchNeverTouchesWorkspaceConfig(t *testing.T) {
	home := t.TempDir()
	if err := config.CreateGlobalConfig(home, "/w"); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(home, "ws", ".opencode_config", "sandbox_config.json")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	// A workspace as an Advanced user left it: Standard-hidden options
	// edited off their presets (searxng search, proxy on).
	if err := sandboxcfg.Create(home, cfgPath, "swdev", nil, "web", "", sandboxcfg.SearchSearxng); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.PatchBool(home, cfgPath, "use_proxy", true); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	s := New(Options{BasePort: 0, Home: home})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if resp := postForm(t, s.URL()+"settings", url.Values{"display_mode": {"standard"}}); resp.StatusCode != http.StatusOK {
		t.Fatalf("POST status = %d, want 200", resp.StatusCode)
	}
	after, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("mode switch rewrote sandbox_config.json:\n%s", after)
	}

	// The view-only matrix: hidden in Standard, revealed in Advanced —
	// with the stored values intact either way.
	if RowVisible(config.DisplayStandard, "ws-settings", "search_mode") ||
		RowVisible(config.DisplayStandard, "ws-settings", "proxy") {
		t.Error("Standard must hide advanced-only rows")
	}
	if !RowVisible(config.DisplayAdvanced, "ws-settings", "search_mode") ||
		!RowVisible(config.DisplayAdvanced, "ws-settings", "proxy") {
		t.Error("Advanced must reveal stored values")
	}
	cfg, err := sandboxcfg.Read(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SearchMode != sandboxcfg.SearchSearxng || !cfg.UseProxy {
		t.Fatalf("stored values did not survive: %+v", cfg)
	}

	// Legacy read rule (absent search_mode reads as websearch) on the
	// same machinery the matrix rows will render from.
	legacyPath := filepath.Join(home, "legacy", ".opencode_config", "sandbox_config.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath,
		[]byte("{\n  \"container_edition\": \"swdev\",\n  \"version\": \"1.0\"\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	legacy, err := sandboxcfg.Read(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.SearchMode != sandboxcfg.SearchWebsearch {
		t.Fatalf("absent search_mode reads %q, want websearch", legacy.SearchMode)
	}
}

func TestPresetsHintWording(t *testing.T) {
	if PresetsHint != "presets, see Advanced" {
		t.Fatalf("PresetsHint = %q", PresetsHint)
	}
}
