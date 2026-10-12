package taskmgr

// Regression: two views launch ops carrying the SAME caller-owned ID
// (every view keeps its own counter starting at 1). The manager's byID
// map is keyed by that caller ID, so the later Launch overwrites the
// entry and the first task's DoneMsg settles the WRONG task — the real
// one stays "running" in its key bucket forever, wedging the bucket:
// every later op on that key queues silently and never runs.

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCrossViewIDCollisionDoesNotWedgeKeys(t *testing.T) {
	m := New()
	release := make(chan struct{})
	viewAran := make(chan struct{}, 1)
	viewBran := make(chan struct{}, 1)

	// View A (e.g. openproject build): long op, caller ID 1.
	cmdA := m.Launch(Op{
		ID: 1, Key: "container-x", Title: "A build",
		Run: func(emit func(string)) error { <-release; return nil },
	})
	// Give A's goroutine time to reach its blocking point.
	time.Sleep(50 * time.Millisecond)

	// View B (e.g. newproject create): ALSO caller ID 1, own key.
	cmdB := m.Launch(Op{
		ID: 1, Key: "newproject", Title: "B init",
		Run: func(emit func(string)) error { viewBran <- struct{}{}; return nil },
	})
	msgsB := drainAll(t, m, cmdB)
	if !containsDone(msgsB, 1) {
		t.Fatalf("view B op did not complete: %+v", msgsB)
	}

	// View A's op finishes: its DoneMsg must settle A's task.
	close(release)
	msgsA := drainAll(t, m, cmdA)
	if !containsDone(msgsA, 1) {
		t.Fatalf("view A op did not complete: %+v", msgsA)
	}
	select {
	case <-viewAran:
	case <-time.After(time.Second):
		// A's Run unblocked via release; nothing to read here.
	default:
	}

	// The manager must be fully idle: no wedged buckets, both keys free.
	deadline := time.Now().Add(time.Second)
	for m.Busy() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if m.Busy() {
		t.Fatal("manager wedged busy after both ops completed (cross-view ID collision)")
	}
	if m.BusyKey("container-x") || m.BusyKey("newproject") {
		t.Fatal("key bucket still busy after its op completed")
	}

	// A follow-up op on each key must start immediately (not queue).
	var _ = viewBran
	late := m.Launch(Op{ID: 1, Key: "newproject", Run: func(emit func(string)) error { return nil }})
	msgs := drainAll(t, m, late)
	if !containsDone(msgs, 1) {
		t.Fatalf("follow-up op on the freed key did not run: %+v", msgs)
	}
}

var _ = tea.Batch
