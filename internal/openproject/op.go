package openproject

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Op is the canonical taskmgr.Op re-exported: one long-running
// operation (container start or image build) launched through the
// Launcher seam. Run blocks until the operation finishes, reporting
// output lines through line as they appear; Key buckets the per-project
// queue (container identity, #82).
type Op = taskmgr.Op

// OpDoneMsg = taskmgr.DoneMsg reports a finished long operation
// (streamed lines went out as taskmgr.LineEvents while it ran; Lines
// carries the whole output for batch launchers).
type OpDoneMsg = taskmgr.DoneMsg

// Launcher is the single seam through which long operations leave the
// Open Project view (#77): every build/start runs through Launch, the
// view itself never spawns goroutines. The task manager (#82) implements
// it for wires everywhere; without one DefaultLauncher keeps the
// synchronous batch behavior.
type Launcher = taskmgr.Launcher

// DefaultLauncher runs Op.Run inside the returned tea.Cmd and reports
// its result as one OpDoneMsg (the pre-#82 behavior, kept as the
// fallback for one-off wiring and tests).
type DefaultLauncher struct{}

// Launch implements Launcher.
func (DefaultLauncher) Launch(op Op) tea.Cmd {
	return func() tea.Msg {
		var lines []string
		err := op.Run(func(l string) { lines = append(lines, l) })
		return OpDoneMsg{ID: op.ID, Lines: lines, Err: err}
	}
}
