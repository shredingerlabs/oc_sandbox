package openproject

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// #95 acceptance: the ExecProcess handoff restores the TUI state
// cleanly after a console session exits — same view, selection, focus,
// toast row intact.

// execHarness injects an Exec seam that captures the tea.ExecCallback
// instead of really running the child, simulating a handoff the test
// controls.
type execHarness struct {
	cmd   *exec.Cmd
	cb    tea.ExecCallback
	fired bool
}

func TestConsoleHandoffRestoresPreHandoffState(t *testing.T) {
	e := newEnv(t, nil)
	var h execHarness
	e.m = New(Deps{
		Home:             e.home,
		Runner:           e.run,
		StartScript:      e.startScript(),
		BuildScript:      e.home + "/build-container.sh",
		Dockerfile:       e.home + "/Dockerfile",
		DockerfileCustom: "",
		Exec: func(c *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
			h.cmd, h.cb, h.fired = c, cb, true
			return func() tea.Msg { return nil }
		},
	})
	bodyOf(t, e.m, bodyW, bodyH)

	// Pre-handoff list state: selected alpha, card-grid focus, no toast.
	e.step(keyMsg("enter")) // start selected project
	e.finishOps()           // start op completes → access prompt
	if _, ok := findLine(bodyOf(t, e.m, bodyW, bodyH), "Console (bash)"); !ok {
		t.Fatal("access prompt not reached")
	}
	toastBefore := e.m.toast

	// Simulate the console session exiting: the Exec callback fires with
	// nil (clean exit), the follow-up message arrives at Update.
	e.step(keyMsg("enter")) // pick Console (bash)
	if !h.fired || h.cb == nil {
		t.Fatal("exec handoff did not fire")
	}
	if !strings.Contains(strings.Join(h.cmd.Args, " "), "exec") {
		t.Errorf("handoff cmd = %v", h.cmd.Args)
	}
	msg := h.cb(nil)
	if msg == nil {
		t.Fatal("exec callback produced no resume message")
	}
	e.step(msg)

	// Restored state: back on the card grid, same selection, focus, and
	// an intact (empty, non-error) toast row.
	if e.m.state != stateList {
		t.Errorf("state after handoff = %v, want stateList", e.m.state)
	}
	if e.m.selected != 0 {
		t.Errorf("selection after handoff = %d, want 0 (alpha)", e.m.selected)
	}
	if e.m.focus != 0 {
		t.Errorf("focus after handoff = %d, want 0 (card grid)", e.m.focus)
	}
	if e.m.toast != toastBefore {
		t.Errorf("toast row after clean session exit = %q, want pre-handoff %q intact", e.m.toast, toastBefore)
	}
	lines := bodyOf(t, e.m, bodyW, bodyH)
	if !someLine(lines, "alpha") || someLine(lines, "Console (bash)") {
		t.Errorf("card grid not restored after handoff:\n%s", strings.Join(lines, "\n"))
	}
}

// A session that exits with an error surfaces the recovery toast while
// still returning to the list state (#95).
func TestConsoleHandoffErrorSurfacesRecoveryToast(t *testing.T) {
	e := newEnv(t, nil)
	var h execHarness
	e.m = New(Deps{
		Home:             e.home,
		Runner:           e.run,
		StartScript:      e.startScript(),
		BuildScript:      e.home + "/build-container.sh",
		Dockerfile:       e.home + "/Dockerfile",
		DockerfileCustom: "",
		Exec: func(c *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
			h.cmd, h.cb, h.fired = c, cb, true
			return func() tea.Msg { return nil }
		},
	})
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	e.step(keyMsg("enter"))
	if !h.fired {
		t.Fatal("exec handoff did not fire")
	}
	e.step(h.cb(exec.ErrNotFound))

	if e.m.state != stateList {
		t.Errorf("state after failed handoff = %v, want stateList", e.m.state)
	}
	if !strings.Contains(e.m.toast, "Container session ended") {
		t.Errorf("recovery toast missing: %q", e.m.toast)
	}
}

// A console/opencode session returning via tea.ExecProcess leaves the
// terminal without mouse support: bubbletea's RestoreTerminal never
// re-enables mouse mode after releasing it for the child process. The
// handoff-resume command must re-send the mouse-enable sequences.
func TestConsoleHandoffRestoresMouse(t *testing.T) {
	e := newEnv(t, nil)
	var h execHarness
	e.m = New(Deps{
		Home:             e.home,
		Runner:           e.run,
		StartScript:      e.startScript(),
		BuildScript:      e.home + "/build-container.sh",
		Dockerfile:       e.home + "/Dockerfile",
		DockerfileCustom: "",
		Exec: func(c *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
			h.cmd, h.cb, h.fired = c, cb, true
			return func() tea.Msg { return nil }
		},
	})
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	e.step(keyMsg("enter"))
	if !h.fired || h.cb == nil {
		t.Fatal("exec handoff did not fire")
	}
	cmd := e.step(h.cb(nil))
	if cmd == nil {
		t.Fatal("handoff resume produced no commands")
	}
	var sawMouse bool
	var types []string
	switch msg := cmd(); msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range []tea.Cmd(msg) {
			m := c()
			types = append(types, fmt.Sprintf("%T", m))
			if fmt.Sprintf("%T", m) == "tea.enableMouseCellMotionMsg" {
				sawMouse = true
			}
		}
	}
	if !sawMouse {
		t.Errorf("handoff resume did not re-enable mouse mode; msgs = %v",
			strings.Join(types, ", "))
	}
}
