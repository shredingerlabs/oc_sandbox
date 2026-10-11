// Package testutil holds test-support helpers shared by the view
// packages: draining task-manager cmd trees the way the running shell
// would deliver them to a view.
package testutil

import (
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// View is the minimal surface Drain drives.
type View interface {
	Update(tea.Msg) tea.Cmd
}

// Drain drains cmd the way the running shell delivers task-manager
// events: every message goes to the shared manager first (its
// continuation keeps the queue chaining) and to the view second. View
// continuations are drained as well, except an in-flight spinner frame
// (spinner.TickMsg reschedules itself forever in a test drain).
func Drain(t testing.TB, view View, mgr *taskmgr.Manager, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			cmd = nil
			for _, c := range []tea.Cmd(msg) {
				Drain(t, view, mgr, c)
			}
		default:
			cmd = nil
			if next := mgr.Update(msg); next != nil {
				Drain(t, view, mgr, next)
			}
			viewNext := view.Update(msg)
			if _, tick := msg.(spinner.TickMsg); tick {
				return
			}
			if viewNext != nil {
				Drain(t, view, mgr, viewNext)
			}
		}
	}
}
