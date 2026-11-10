package webserver

import (
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// Deps carries the seams the container/image screens run on (spec §9:
// business logic stays in registry/discovery/sandboxcfg; scripts are
// invoked exactly as the TUI invokes them, ADR-0007). Nil Deps leaves
// the server as the #150 shell-only skeleton.
type Deps struct {
	Run              cmdrun.Runner
	Registry         *registry.Registry
	BuildScript      string // scripts/build-container.sh
	StartScript      string // scripts/start.sh
	Dockerfile       string // edition registry (ADR 0019)
	DockerfileCustom string // "" when the install has none
}
