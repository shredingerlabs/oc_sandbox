package webserver

import (
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Deps is the business wiring one Server needs (call Wire before Start,
// normally from the launch path). Business logic stays in
// config/registry/taskmgr/discovery (spec §9); the handlers compose it.
type Deps struct {
	Home string // the oc-sandbox home ($HOME) — config, registry, backups

	// UninstallScript is the resolved uninstall.sh path (app layout);
	// invoked exactly as the TUI invokes it (bash <script> <flags>).
	UninstallScript string

	// Runner is the command-runner seam (podman/scripts calls).
	Runner cmdrun.Runner

	// Tasks is the process-wide task manager shared with the TUI:
	// ops launched here queue behind the same per-key semantics, and
	// confirmations refuse while any op is queued or running.
	Tasks *taskmgr.Manager
}

// busy reports whether any op is queued or running — confirmations and
// op-launching POSTs refuse while it holds (taskmgr semantics mirrored).
func (s *Server) busy() bool {
	return s.deps.Tasks != nil && s.deps.Tasks.Busy()
}

// Busy is the exported busy query — screens and harnesses render/mirror
// the queue's state off it.
func (s *Server) Busy() bool { return s.busy() }
