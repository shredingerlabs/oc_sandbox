package webserver

// Started-config evidence (web restart semantics, ADR-0033): a running
// container's workspace is "stale" — the card's Restart activates —
// when the saved config's start-relevant fields (edition + modes +
// proxy + search mode, exactly what start.sh consumes), the workspace
// image's ID, or both changed since the container last (re)started.
// The hash and image ID ride the registry (SetStartedEvidence, written
// right after a verified start); running containers from before the
// feature carry no evidence and read as stale (restart possible —
// otherwise long-running legacy workspaces could never restart).

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sort"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// configFingerprint hashes the start-relevant sandbox_config.json
// fields: the edition, the mode list (order-normalized), the proxy
// flag and the search mode. VCS/provider edits are not start-critical
// inputs and never enter the fingerprint.
func configFingerprint(cfg sandboxcfg.Config) string {
	modes := slices.Clone(cfg.ContainerModes)
	sort.Strings(modes)
	canonical := strings.Join([]string{
		"edition=" + cfg.ContainerEdition,
		"modes=" + strings.Join(modes, ","),
		"proxy=" + bool01(cfg.UseProxy),
		"search=" + cfg.SearchMode,
	}, "\x00")
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

func bool01(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// imageID reads the workspace image's ID (`podman inspect .Id`), ""
// when the inspect fails (missing or unreadable image reads as changed
// evidence at compare time).
func imageID(run cmdrun.Runner, edition string) string {
	out, err := run.Output("podman", "inspect", "--format", "{{.Id}}", "opencode-sandbox-"+edition)
	if err != nil {
		return ""
	}
	lines := cmdrun.Lines(out)
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}

// workspaceStale reports whether the running workspace's started
// evidence no longer matches the current state: no stored hash, a
// config fingerprint drift, or an image-ID drift (deleted/rebuilt
// image — rebuilds count: the running container keeps the old layers
// until it is replaced).
func workspaceStale(run cmdrun.Runner, ws registry.Workspace, cfg sandboxcfg.Config) bool {
	if ws.StartedConfigHash == "" {
		return true // no evidence: containers from before the feature read as stale
	}
	if ws.StartedConfigHash != configFingerprint(cfg) {
		return true
	}
	id := imageID(run, cfg.ContainerEdition)
	return id == "" || id != ws.StartedImageID
}

// recordStartedEvidence writes the fresh started-config fingerprint and
// image ID after a verified (re)start.
func (s *Server) recordStartedEvidence(ws registry.Workspace, cfg sandboxcfg.Config) error {
	return s.deps.Registry.SetStartedEvidence(ws.Path,
		configFingerprint(cfg), imageID(s.deps.Run, cfg.ContainerEdition))
}
