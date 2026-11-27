package openworkspace

import (
	"slices"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// The start.sh argument vector the WebUI's start/adopt/restart chains
// hand to the script (data-map §3): root, the toolchain selection
// flags, container identity, stored modes, toggles, search mode,
// --detach last.
func TestStartArgsVectorFromStoredFields(t *testing.T) {
	cfg := sandboxcfg.Config{Resolved: true, LegacyEdition: "base",
		ContainerModes: []string{"skills"}, UseProxy: true, UseStt: true, SearchMode: "websearch"}
	got := StartArgs("/home/u/workspaces/alpha", cfg, "abc123def456", "console")
	want := []string{
		"/home/u/workspaces/alpha", "--edition", "base", "--container-id", "abc123def456",
		"--skills", "--use_proxy", "--use_stt", "--search_mode", "websearch", "--detach",
	}
	if !slices.Equal(got, want) {
		t.Errorf("StartArgs = %v, want %v", got, want)
	}
}

// A materialized (new-world) config emits the sorted toolchain ids via
// --toolchains — order-free input, one canonical vector (spec §8, WP-2).
func TestStartArgsEmitsSortedToolchains(t *testing.T) {
	cfg := sandboxcfg.Config{Resolved: true, ToolchainsStored: true,
		Toolchains: []string{"go", "cpp", "playwright"},
		SearchMode: "websearch"}
	got := StartArgs("/home/u/workspaces/alpha", cfg, "abc123def456", "web")
	want := []string{
		"/home/u/workspaces/alpha", "--toolchains", "cpp,go,playwright", "--container-id", "abc123def456",
		"--search_mode", "websearch", "--start_web", "--detach",
	}
	if !slices.Equal(got, want) {
		t.Errorf("StartArgs = %v, want %v", got, want)
	}
}

// The base-only set (∅ — chat/pkm) emits the empty --toolchains value.
func TestStartArgsEmptyToolchainsVector(t *testing.T) {
	cfg := sandboxcfg.Config{Resolved: true, ToolchainsStored: true, Toolchains: []string{}}
	got := StartArgs("/home/u/workspaces/alpha", cfg, "abc123def456", "console")
	if !slices.Contains(got, "--toolchains") || !slices.Contains(got, "") {
		t.Errorf("StartArgs = %v, want --toolchains with the empty value", got)
	}
	if slices.Contains(got, "--edition") {
		t.Errorf("StartArgs = %v, the new world never emits --edition", got)
	}
}

// The web start option carries --start_web before --detach; the other
// start options never do (the foreground env path never fires).
func TestStartArgsWebOptionAddsStartWebFlag(t *testing.T) {
	cfg := sandboxcfg.Config{Resolved: true, LegacyEdition: "base", SearchMode: "none"}
	got := StartArgs("/home/u/workspaces/alpha", cfg, "abc123def456", "web")
	if !slices.Contains(got, "--start_web") {
		t.Errorf("StartArgs(web) = %v, want --start_web present", got)
	}
	if got[len(got)-1] != "--detach" {
		t.Errorf("StartArgs must end with --detach, got %v", got)
	}
}

// offline + cbm_ui are mutually exclusive (bash validate_container_modes;
// start.sh exits on the combination).
func TestValidModesRejectsOfflineWithCBM(t *testing.T) {
	if ValidModes([]string{"offline", "cbm_ui"}) {
		t.Error("offline + cbm_ui accepted")
	}
	for _, ok := range [][]string{nil, {"offline"}, {"cbm_ui", "hil_mode"}, {"skills", "offline"}} {
		if !ValidModes(ok) {
			t.Errorf("ValidModes(%v) refused", ok)
		}
	}
}

// SearXNG with the squid proxy or with offline is the ADR-0026
// combination start.sh hard-exits on.
func TestValidSearchCombinationRejectsProxyAndOfflineSearxng(t *testing.T) {
	if ValidSearchCombination(sandboxcfg.SearchSearxng, nil, true) {
		t.Error("searxng + proxy accepted")
	}
	if ValidSearchCombination(sandboxcfg.SearchSearxng, []string{"offline"}, false) {
		t.Error("searxng + offline accepted")
	}
	for _, ok := range []struct {
		mode  string
		modes []string
		proxy bool
	}{
		{sandboxcfg.SearchSearxng, nil, false},
		{sandboxcfg.SearchWebsearch, nil, true},
		{sandboxcfg.SearchNone, []string{"offline"}, true},
	} {
		if !ValidSearchCombination(ok.mode, ok.modes, ok.proxy) {
			t.Errorf("ValidSearchCombination(%q, %v, %v) refused", ok.mode, ok.modes, ok.proxy)
		}
	}
}
