package openworkspace

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/identity"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// StartArgs rebuilds the start.sh argument vector from the stored config
// (data-map §3, bash start_container): the workspace root, then the
// toolchain-selection flags (SelectionFlags), --container-id, one
// --<mode> flag per stored mode, --use_proxy, --use_stt, --search_mode
// <m>, --start_web for the web start option (starts detached, so
// --start_opencode and the foreground --start_web env path never fire),
// and --detach last. startOption is the caller's access variant (the
// stored start_option is retired — ADR-0033).
func StartArgs(projectPath string, cfg sandboxcfg.Config, containerID string, startOption string) []string {
	args := append([]string{projectPath}, SelectionFlags(cfg)...)
	args = append(args, "--container-id", containerID)
	for _, mode := range cfg.ContainerModes {
		args = append(args, "--"+mode)
	}
	if cfg.UseProxy {
		args = append(args, "--use_proxy")
	}
	if cfg.UseStt {
		args = append(args, "--use_stt")
	}
	args = append(args, "--search_mode", cfg.SearchMode)
	if startOption == "web" {
		args = append(args, "--start_web")
	}
	return append(args, "--detach")
}

// SelectionFlags is the toolchain-selection prefix of a start.sh
// invocation (spec §8/§10): a materialized (new-world) config emits the
// sorted ids via --toolchains, a legacy one the stored edition via
// --edition — honest in both worlds until the cutover. start.sh re-keys
// with #200; until then a new-world vector is refused by the script's
// flag parser with a visible reason.
func SelectionFlags(cfg sandboxcfg.Config) []string {
	if cfg.ToolchainsStored {
		ids := slices.Clone(cfg.Toolchains)
		slices.Sort(ids)
		return []string{"--toolchains", strings.Join(ids, ",")}
	}
	return []string{"--edition", cfg.LegacyEdition}
}

// SetTagFor derives the content-addressed set tag for a member id list
// against live discovery over the toolchains dir (spec toolchain-sets
// §6/§9 WP-6): every id must resolve to a valid discovered unit (its
// recipe feeds the content hash) and the base recipe must exist.
// ok=false names an underivable tag — the honest missing-set-image
// state every start path renders.
func SetTagFor(toolchainsDir string, ids []string) (string, bool) {
	cat := discovery.Discover(toolchainsDir)
	var members []identity.Member
	for _, id := range ids {
		u, ok := cat.Unit(id)
		if !ok || !u.Valid {
			return "", false
		}
		members = append(members, identity.Member{ID: id, RecipePath: u.Path})
	}
	tag, err := identity.SetTag(filepath.Join(toolchainsDir, "base.Dockerfile"), members)
	if err != nil {
		return "", false
	}
	return tag, true
}

// ValidModes rejects the mutually exclusive offline + cbm_ui combination
// (bash validate_container_modes; start.sh exits on it).
func ValidModes(modes []string) bool {
	hasOffline, hasCBM := false, false
	for _, m := range modes {
		switch m {
		case "offline":
			hasOffline = true
		case "cbm_ui":
			hasCBM = true
		}
	}
	return !(hasOffline && hasCBM)
}

// ValidSearchCombination rejects the ADR-0026 combinations that start.sh
// hard-exits on: SearXNG search with the squid proxy or with the offline
// mode.
func ValidSearchCombination(searchMode string, modes []string, useProxy bool) bool {
	if searchMode != sandboxcfg.SearchSearxng {
		return true
	}
	return !useProxy && !hasMode(modes, "offline")
}

// hasMode reports whether modes contains mode.
func hasMode(modes []string, mode string) bool {
	for _, m := range modes {
		if m == mode {
			return true
		}
	}
	return false
}
