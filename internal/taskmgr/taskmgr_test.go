package taskmgr

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// One shared instance (shell + all center views). Tests drain only
// through the public tea.Cmd/message seam — external behavior only.

// drainAll executes the cmd, feeds each message back through Update (the
// model round-trip), and returns every message in delivery order.
func drainAll(t *testing.T, m *Manager, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	var out []tea.Msg
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			break
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			cmd = nil
			for _, c := range []tea.Cmd(msg) {
				out = append(out, drainAll(t, m, c)...)
			}
		default:
			out = append(out, msg)
			cmd = m.Update(msg)
		}
	}
	return out
}

func TestLaunchRunsOpStreamsLinesThenDone(t *testing.T) {
	m := New()
	cmd := m.Launch(Op{
		ID:    7,
		Key:   "c1",
		Title: "building",
		Run: func(emit func(string)) error {
			emit("step 1")
			emit("step 2")
			return nil
		},
	})
	if !m.Busy() || !m.BusyKey("c1") {
		t.Fatal("op not marked running")
	}
	if title, ok := m.RunningTitle("c1"); !ok || title != "building" {
		t.Fatalf("RunningTitle = %q, %v", title, ok)
	}
	msgs := drainAll(t, m, cmd)
	var lines []string
	var done DoneMsg
	for _, msg := range msgs {
		switch msg := msg.(type) {
		case LineEvent:
			if msg.ID != 7 {
				t.Fatalf("LineEvent from wrong op id: %+v", msg)
			}
			lines = append(lines, msg.Text)
		case DoneMsg:
			done = msg
		}
	}
	if len(lines) != 2 || lines[0] != "step 1" || lines[1] != "step 2" {
		t.Fatalf("streamed lines = %v", lines)
	}
	if done.ID != 7 || done.Err != nil || len(done.Lines) != 2 {
		t.Fatalf("DoneMsg = %+v", done)
	}
	if m.Busy() || m.BusyKey("c1") {
		t.Fatal("manager still busy after completion")
	}
	if _, ok := m.RunningTitle("c1"); ok {
		t.Fatal("RunningTitle still reports after completion")
	}
}

func TestDoneCarriesFailure(t *testing.T) {
	m := New()
	msgs := drainAll(t, m, m.Launch(Op{
		ID: 1, Key: "c1",
		Run: func(emit func(string)) error { return errors.New("boom") },
	}))
	var done DoneMsg
	for _, msg := range msgs {
		if d, ok := msg.(DoneMsg); ok {
			done = d
		}
	}
	if done.Err == nil || done.Err.Error() != "boom" {
		t.Fatalf("failure not surfaced: %+v", done)
	}
	// the failed task frees its key (recovery affordance); not Busy.
	if m.Busy() {
		t.Fatal("manager still busy after failure")
	}
}

func TestQueuedSecondRunsAfterFirstFinished(t *testing.T) {
	m := New()
	release := make(chan struct{})
	ran := make(chan string, 2)
	blocking := Op{
		ID: 1, Key: "c1", Title: "first",
		Run: func(emit func(string)) error {
			emit("first ran")
			<-release
			return nil
		},
	}
	second := Op{
		ID: 2, Key: "c1", Title: "second",
		Run: func(emit func(string)) error {
			ran <- "second ran"
			return nil
		},
	}
	firstCmd := m.Launch(blocking) // starts (blocks on release)
	enqueueMsgs := drainAll(t, m, m.Launch(second))
	var sawQueued bool
	for _, msg := range enqueueMsgs {
		if st, ok := msg.(StatusMsg); ok && st.State == StateQueued && st.ID == 2 {
			sawQueued = true
		}
	}
	if !sawQueued {
		t.Fatalf("no queued StatusMsg delivered: %+v", enqueueMsgs)
	}
	if !m.Busy() || !m.BusyKey("c1") {
		t.Fatal("queue not visible via Busy/BusyKey")
	}

	// a DIFFERENT key runs concurrently while c1's op is still in flight
	msgs := drainAll(t, m, m.Launch(Op{
		ID: 3, Key: "c2",
		Run: func(emit func(string)) error { ran <- "c2 ran"; return nil },
	}))
	if !containsDone(msgs, 3) {
		t.Fatalf("c2 op did not complete: %+v", msgs)
	}
	select {
	case got := <-ran:
		if got != "c2 ran" {
			t.Fatalf("c2 ran = %q", got)
		}
	case <-time.After(time.Second):
	}

	close(release) // finish the blocking op
	msgs = drainAll(t, m, firstCmd)
	var firstDone DoneMsg
	for _, msg := range msgs {
		if d, ok := msg.(DoneMsg); ok && d.ID == 1 {
			firstDone = d
		}
	}
	if firstDone.ID != 1 || firstDone.Err != nil ||
		len(firstDone.Lines) != 1 || firstDone.Lines[0] != "first ran" {
		t.Fatalf("first DoneMsg = %+v", firstDone)
	}
	select {
	case got := <-ran:
		if got != "second ran" {
			t.Fatalf("second ran = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("queued op did not start after predecessor finished")
	}
	if !containsDone(msgs, 2) {
		t.Fatalf("successor op never completed: %+v", msgs)
	}
	if m.Busy() || m.BusyKey("c1") {
		t.Fatal("manager busy after every task completed")
	}
}

// containsDone reports whether msgs carried a DoneMsg with that id.
func containsDone(msgs []tea.Msg, id int) bool {
	for _, msg := range msgs {
		if d, ok := msg.(DoneMsg); ok && d.ID == id {
			return true
		}
	}
	return false
}

func TestAutoAssignIDsEchoed(t *testing.T) {
	m := New()
	msgs := drainAll(t, m, m.Launch(Op{Key: "ci", Run: func(emit func(string)) error { return nil }}))
	var status StatusMsg
	for _, msg := range msgs {
		if st, ok := msg.(StatusMsg); ok {
			status = st
		}
	}
	if status.State != StateRunning || status.ID == 0 {
		t.Fatalf("running status missing or id not echoed: %+v", status)
	}
	if !containsDone(msgs, status.ID) {
		t.Fatalf("auto-assigned id not echoed in DoneMsg: %+v", msgs)
	}
}

func TestCancelQueuedDropsPending(t *testing.T) {
	m := New()
	release := make(chan struct{})
	firstCmd := m.Launch(Op{
		ID: 1, Key: "c1",
		Run: func(emit func(string)) error { <-release; return nil },
	})
	m.Launch(Op{
		ID: 2, Key: "c1",
		Run: func(emit func(string)) error { return nil },
	})
	m.CancelQueued("c1")
	// finish the running op: nothing queued, no restart
	close(release)
	drainAll(t, m, firstCmd)
	time.Sleep(50 * time.Millisecond)
	if m.Busy() {
		t.Fatal("busy after running op finished with queue cancelled")
	}
}
