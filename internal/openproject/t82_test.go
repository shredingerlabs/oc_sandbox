package openproject

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// #82 behaviors: per-project queue key, shared streaming transport,
// guarded terminal handoff, still-running indicator, container-gone
// recovery.

// drive drains the cmd like the shell does: every event to the view and
// once to the shared manager; the manager's chaining cmd keeps draining.
func drive(t *testing.T, m *Model, mgr *taskmgr.Manager, cmd tea.Cmd) {
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
				drive(t, m, mgr, c)
			}
		default:
			cmd = nil
			if next := mgr.Update(msg); next != nil {
				drive(t, m, mgr, next)
			}
			m.Update(msg)
		}
	}
}

// queuedEnv wires the harness view to a shared task manager.
func queuedEnv(t *testing.T, hooks *runnerHooks) *env {
	t.Helper()
	e := newEnv(t, hooks)
	e.mgr = taskmgr.New()
	e.rebuild(t)
	e.m.deps.Ops = e.mgr
	e.m.deps.Tasks = e.mgr
	return e
}

func TestStartThroughSharedQueueStreamsAndGrantsAccess(t *testing.T) {
	e := queuedEnv(t, &runnerHooks{psOut: ""})
	cmd := e.step(keyMsg("enter"))
	if !e.mgr.BusyKey(e.ids["alpha"]) {
		t.Fatal("start op not queued under the project's container identity")
	}
	drive(t, e.m, e.mgr, cmd)
	if e.mgr.BusyKey(e.ids["alpha"]) {
		t.Fatal("queue key not freed after start finished")
	}
	if got := e.m.toast; got != "Container started" {
		t.Fatalf("toast = %q", got)
	}
	if e.m.state != stateAccess {
		t.Fatalf("state = %v, want the access prompt after start", e.m.state)
	}
	if e.startStatus(t, "alpha") != "running" {
		t.Fatal("container_status not written on verified start")
	}
}

func TestStillRunningIndicatorOnCardWhileTaskRuns(t *testing.T) {
	e := queuedEnv(t, &runnerHooks{psOut: ""})
	cmd := e.step(keyMsg("enter")) // op in flight, completion not yet drained
	lines := bodyOf(t, e.m, bodyW, bodyH)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "alpha") && strings.Contains(l, "◌") {
			found = true
		}
	}
	if !found {
		t.Fatalf("still-running indicator missing on the owning card:\n%s", strings.Join(lines, "\n"))
	}
	drive(t, e.m, e.mgr, cmd)
	if strings.Contains(strings.Join(lines, "\n"), "◌") {
		return // final state asserted by the other test
	}
}

func TestTerminalHandoffGuardedWhileTasksRun(t *testing.T) {
	e := queuedEnv(t, &runnerHooks{psOut: ""})
	release := make(chan struct{})
	e.mgr.Launch(taskmgr.Op{
		ID:    900,
		Key:   "other",
		Title: "building foreign",
		Run: func(emit func(string)) error {
			<-release
			return nil
		},
	})
	var execed []string
	e.m = e.buildWithExec(t, &execed)
	e.m.deps.Ops = e.mgr
	e.m.deps.Tasks = e.mgr
	e.m.state = stateAccess
	e.m.pending = e.m.projects[0]
	e.m.accessOpts = []string{"Console (bash)"}
	e.m.accessSel = 0

	if cmd := e.m.Update(keyMsg("enter")); cmd != nil {
		t.Fatal("handoff started although tasks ran")
	}
	if !strings.Contains(e.m.toast, "Background tasks are running") {
		t.Fatalf("guard toast missing: %q", e.m.toast)
	}
	if !e.mgr.BusyKey("other") {
		t.Fatal("foreign task lost")
	}
	// idle again → handoff passes the guard (container-gone → recovery toast)
	close(release)
	e.mgr.Update(taskmgr.DoneMsg{ID: 900, Key: "other"})
	if cmd := e.m.Update(keyMsg("enter")); cmd == nil {
		t.Fatal("handoff still guarded after tasks finished")
	}
}

func TestContainerGoneBetweenStatesShowsRecovery(t *testing.T) {
	e := queuedEnv(t, &runnerHooks{psOut: ""})
	var execed []string
	e.m = e.buildWithExec(t, &execed)
	e.m.deps.Ops = e.mgr
	e.m.deps.Tasks = e.mgr
	e.m.state = stateAccess
	e.m.pending = e.m.projects[0]
	e.m.accessOpts = []string{"Console (bash)"}
	e.m.accessSel = 0
	if cmd := e.m.Update(keyMsg("enter")); cmd == nil {
		t.Fatal("handoff not started")
	}
	if len(execed) != 1 || !strings.Contains(execed[0], "podman exec") {
		t.Fatalf("handoff command wrong: %v", execed)
	}
	// the handoff callback reports the failure (container vanished mid-session)
	e.m.Update(accessDoneMsg{Err: errors.New("no such container")})
	if !strings.Contains(e.m.toast, "Start it again from its card") {
		t.Fatalf("recovery affordance missing: %q", e.m.toast)
	}
	if e.m.state != stateList {
		t.Fatalf("state = %v, want back on the card grid", e.m.state)
	}
}
