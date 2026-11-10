package webserver

import (
	"errors"
	"fmt"

	"github.com/shredingerlabs/oc-sandbox/internal/openworkspace"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// ops.go: shared seams and errors the container/image ops use — thin
// adapters over sandboxcfg/openworkspace so the web handlers mirror the
// TUI's start validations (openworkspace + containerview semantics).

// readSandboxCfg loads the workspace's stored sandbox_config.json.
func readSandboxCfg(root string) (sandboxcfg.Config, error) {
	return sandboxcfg.Read(sandboxcfg.Path(root))
}

// validateStartCfg mirrors the TUI's pre-start validations: offline +
// cbm_ui are mutually exclusive, SearXNG search needs network and no
// proxy (ADR 0026) — start.sh would hard-exit on both.
func validateStartCfg(cfg sandboxcfg.Config) error {
	if !openworkspace.ValidModes(cfg.ContainerModes) {
		return errors.New("offline and cbm_ui are mutually exclusive — fix settings first")
	}
	if !openworkspace.ValidSearchCombination(cfg.SearchMode, cfg.ContainerModes, cfg.UseProxy) {
		return errors.New("searxng search requires network and no proxy (ADR 0026) — fix settings first")
	}
	return nil
}

// startScriptArgs rebuilds the start.sh argument vector exactly as the
// TUI does (openworkspace.StartArgs; always detached).
func startScriptArgs(root, containerID string, cfg sandboxcfg.Config) []string {
	return openworkspace.StartArgs(root, cfg.ContainerEdition, containerID,
		cfg.ContainerModes, cfg.StartOption, cfg.UseProxy, cfg.SearchMode)
}

func errImageMissing(edition string) error {
	return fmt.Errorf("image opencode-sandbox-%s is not built yet", edition)
}

func errNotRunning(name string) error {
	return fmt.Errorf("%s is not running", name)
}

func errStillRunning(name string) error {
	return fmt.Errorf("%s is still running", name)
}

func errStillExists(name string) error {
	return fmt.Errorf("%s still exists", name)
}

func errStopFirst(name string) error {
	return fmt.Errorf("stop it first: podman stop %s", name)
}

func errStopFirstPod(name string) error {
	return fmt.Errorf("stop it first: podman pod stop %s", name)
}
