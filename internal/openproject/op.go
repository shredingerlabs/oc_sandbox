package openproject

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Op is one long-running operation (container start or image build)
// launched through the Launcher seam. Run blocks until the operation
// finishes, reporting output lines through line as they appear.
type Op struct {
	ID    int
	Title string
	Run   func(line func(string)) error
}

// OpDoneMsg reports a finished long operation. Lines carries the output
// collected while it ran; a streaming upgrade replaces the batch with
// LineMsgs as they occur (#82).
type OpDoneMsg struct {
	ID    int
	Lines []string
	Err   error
}

// Launcher is the single seam through which long operations leave the
// Open Project view (#77): every build/start runs through Launch, the
// view itself never spawns goroutines. The returned tea.Cmd — executed
// by the BubbleTea runtime — drives Op.Run and yields one OpDoneMsg.
// The background-task manager (#82) swaps this seam for queued,
// streaming, cancellable tasks without touching the view.
type Launcher interface {
	Launch(op Op) tea.Cmd
}

// DefaultLauncher runs Op.Run inside the returned tea.Cmd and reports
// its result as one OpDoneMsg.
type DefaultLauncher struct{}

// Launch implements Launcher.
func (DefaultLauncher) Launch(op Op) tea.Cmd {
	return func() tea.Msg {
		var lines []string
		err := op.Run(func(l string) { lines = append(lines, l) })
		return OpDoneMsg{ID: op.ID, Lines: lines, Err: err}
	}
}
