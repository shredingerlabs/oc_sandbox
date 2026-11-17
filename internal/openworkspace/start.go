package openworkspace

import (
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// StartArgs rebuilds the start.sh argument vector from stored fields
// (data-map §3, bash start_container): the workspace root first, then
// --edition, --container-id, one --<mode> flag per stored mode,
// --use_proxy, --use_stt, --search_mode <m>, --start_web for the web
// start option (starts detached, so --start_opencode and the
// foreground --start_web env path never fire), and --detach last.
func StartArgs(projectPath, edition, containerID string, modes []string, startOption string, useProxy, useStt bool, searchMode string) []string {
	args := []string{projectPath, "--edition", edition, "--container-id", containerID}
	for _, mode := range modes {
		args = append(args, "--"+mode)
	}
	if useProxy {
		args = append(args, "--use_proxy")
	}
	if useStt {
		args = append(args, "--use_stt")
	}
	args = append(args, "--search_mode", searchMode)
	if startOption == "web" {
		args = append(args, "--start_web")
	}
	return append(args, "--detach")
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
