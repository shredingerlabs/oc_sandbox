package webserver

import (
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// Deps carries the seams the container/image screens run on (spec §9:
// business logic stays in registry/discovery/sandboxcfg; scripts are
// invoked exactly as the TUI invokes them, ADR-0007). Nil Deps leaves
// the server as the #150 shell-only skeleton. #156 adds the New
// Workspace wizard's seams: the init/configure scripts and the derived
// root's base directory.
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
