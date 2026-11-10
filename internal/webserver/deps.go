package webserver

import (
	"path/filepath"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// Deps carries the seams the screens run on (spec §9: business logic
// stays in registry/discovery/sandboxcfg; scripts are invoked exactly as
// the TUI invokes them, ADR-0007). Nil Deps leaves the server as the
// #150 shell-only skeleton. #156 adds the New Workspace wizard's seams:
// the init/configure scripts and the derived root's base directory.
type Deps struct {
	Run              cmdrun.Runner
	Registry         *registry.Registry
	BuildScript      string // scripts/build-container.sh
	StartScript      string // scripts/start.sh
	InitScript       string // scripts/init-workspace.sh (#156)
	ConfigureScript  string // scripts/configure-workspace.sh (#156)
	Dockerfile       string // edition registry (ADR 0019)
	DockerfileCustom string // "" when the install has none
	// DefaultWorkspacePath is the base of the derived workspace root
	// (#156 — the wizard has no path row); "" → Home/oc-sandbox.
	DefaultWorkspacePath string
}

// busy reports whether any op is queued or running — the §9/§12
// confirmation refusal: confirm-gated POSTs refuse while it holds. The
// ops ride the webserver's own opRunner queue (taskmgr semantics
// mirrored: one op at a time per key, parallel across keys).
func (s *Server) busy() bool { return s.queue.Busy() }

// Busy is the exported busy query — screens and harnesses render/mirror
// the queue's state off it.
func (s *Server) Busy() bool { return s.busy() }

// runnerSeam returns the command-runner seam: Deps.Run when wired, else
// the exec runner.
func (s *Server) runnerSeam() cmdrun.Runner {
	if s.deps != nil && s.deps.Run != nil {
		return s.deps.Run
	}
	return cmdrun.ExecRunner{}
}

// regView returns the workspace registry: Deps.Registry when wired, else
// one for the wired home.
func (s *Server) regView() *registry.Registry {
	if s.deps != nil && s.deps.Registry != nil {
		return s.deps.Registry
	}
	return &registry.Registry{Home: s.opts.Home}
}

// home is the oc-sandbox home (~/.config/oc-sandbox holds the config
// files; workspace roots live under their registered paths).
func (s *Server) home() string { return s.opts.Home }

// uninstallScript resolves uninstall.sh from the install layout
// (app.LayoutFor): <install root>/scripts/uninstall.sh — the same file
// the TUI invokes.
func (s *Server) uninstallScript() string {
	return filepath.Join(s.opts.InstallRoot, "scripts", "uninstall.sh")
}
