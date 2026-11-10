package webserver

// Op plumbing: the WebUI launches its long operations through the
// process-wide task manager shared with the TUI (spec §9 — taskmgr
// semantics mirrored: queue per key, one-at-a-time per key, parallel
// across keys). The manager speaks tea.Cmd/tea.Msg; the server drains
// that stream on a goroutine per op, relaying log lines and state
// changes onto the SSE hub so open tabs stream the op's output.

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// launchOp queues one op on the shared task manager and drains its
// event stream in the background. The HTTP handler returns at once —
// the screen fragment reports the queued state, the SSE stream carries
// the lines.
func (s *Server) launchOp(op taskmgr.Op) {
	if s.deps.Tasks == nil {
		return
	}
	go s.drain(s.deps.Tasks.Launch(op))
}

// drain executes the manager's message stream: flatten batches, relay
// LineEvents to the hub (the log-pane transport), publish workspace on
// terminal messages (busy-state re-render), and keep Update's successor
// commands running. Runs on one goroutine per op — Update is safe for
// concurrent callers.
func (s *Server) drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	stack := []tea.Cmd{cmd}
	for len(stack) > 0 {
		cmd := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cmd == nil {
			continue
		}
		msg := cmd()
		if msg == nil {
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			stack = append(stack, []tea.Cmd(batch)...)
			continue
		}
		switch m := msg.(type) {
		case taskmgr.LineEvent:
			s.hub.PublishLine(m.ID, m.Text)
		case taskmgr.DoneMsg:
			s.hub.PublishWorkspace() // busy state changed — tabs re-render
		}
		if next := s.deps.Tasks.Update(msg); next != nil {
			stack = append(stack, next)
		}
	}
}
