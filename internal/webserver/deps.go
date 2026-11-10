package webserver

// Busy-query and home seams for the settings screens (#159). The ops
// ride the webserver's own opRunner queue (taskmgr semantics mirrored:
// one op at a time per key, parallel across keys) — confirmations and
// op-launching POSTs refuse while any op is queued or running.

import "path/filepath"

// busy reports whether any op is queued or running (§9: confirms refuse
// while an op is queued/running).
func (s *Server) busy() bool { return s.queue.Busy() }

// Busy is the exported busy query — screens and harnesses render/mirror
// the queue's state off it.
func (s *Server) Busy() bool { return s.busy() }

// home is the oc-sandbox home (~/.config/oc-sandbox holds the config
// files; workspace roots live under their registered paths).
func (s *Server) home() string { return s.opts.Home }

// uninstallScript resolves uninstall.sh from the install layout
// (app.LayoutFor): <install root>/scripts/uninstall.sh — the same file
// the TUI invokes.
func (s *Server) uninstallScript() string {
	return filepath.Join(s.opts.InstallRoot, "scripts", "uninstall.sh")
}
