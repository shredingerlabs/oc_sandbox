package webserver

import (
	"sync"
	"testing"
	"time"
)

// The web op queue mirrors taskmgr's semantics (spec §9): one op at a
// time per key, parallel across keys — plus the §12 global exclusivity:
// the update run is an exclusive op, and while it is queued or running
// the queue accepts nothing else across workspaces (queued ops wait
// behind it).

func TestOpQueueParallelAcrossKeys(t *testing.T) {
	r := newOpRunner(newHub())
	aRunning := make(chan struct{})
	bRunning := make(chan struct{})
	aDone := make(chan struct{})
	bDone := make(chan struct{})
	r.Launch(webOp{
		Key: "ws-a",
		Run: func(emit func(string)) error {
			close(aRunning)
			<-bRunning // hold until the other key's op is provably running
			return nil
		},
		Done: func(err error) { close(aDone) },
	})
	r.Launch(webOp{
		Key: "ws-b",
		Run: func(emit func(string)) error {
			close(bRunning)
			return nil
		},
		Done: func(err error) { close(bDone) },
	})
	select {
	case <-aDone:
	case <-time.After(2 * time.Second):
		t.Fatal("ops on different keys did not run in parallel")
	}
	<-bDone
}

func TestOpQueueOneAtATimePerKey(t *testing.T) {
	r := newOpRunner(newHub())
	var mu sync.Mutex
	overlapping := 0
	maxOverlap := 0
	run := func(emit func(string)) error {
		mu.Lock()
		overlapping++
		if overlapping > maxOverlap {
			maxOverlap = overlapping
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		overlapping--
		mu.Unlock()
		return nil
	}
	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		r.Launch(webOp{Key: "ws-a", Run: run, Done: func(err error) { done <- struct{}{} }})
	}
	for i := 0; i < 3; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d of 3 same-key ops finished", i)
		}
	}
	if maxOverlap != 1 {
		t.Fatalf("same-key ops overlapped %d-wide, want strictly one at a time", maxOverlap)
	}
}

// The update run is exclusive: while it runs, an op launched for another
// key must not start — it waits behind the update.
func TestOpQueueExclusiveUpdateQueuesEverythingElse(t *testing.T) {
	r := newOpRunner(newHub())
	release := make(chan struct{})
	updDone := make(chan struct{})
	r.Launch(webOp{
		Key:       "update",
		Exclusive: true,
		Run: func(emit func(string)) error {
			emit("downloading release…")
			<-release
			return nil
		},
		Done: func(err error) { close(updDone) },
	})
	otherDone := make(chan struct{})
	r.Launch(webOp{
		Key:  "ws-a",
		Run:  func(emit func(string)) error { return nil },
		Done: func(err error) { close(otherDone) },
	})
	select {
	case <-otherDone:
		t.Fatal("op started while the exclusive update runs")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-updDone
	select {
	case <-otherDone:
	case <-time.After(2 * time.Second):
		t.Fatal("queued op did not start after the update finished")
	}
}

// An exclusive op launched while something else runs waits for idle —
// it never preempts a running op.
func TestOpQueueExclusiveWaitsForRunningOp(t *testing.T) {
	r := newOpRunner(newHub())
	release := make(chan struct{})
	aDone := make(chan struct{})
	r.Launch(webOp{
		Key:  "ws-a",
		Run:  func(emit func(string)) error { <-release; return nil },
		Done: func(err error) { close(aDone) },
	})
	updRunning := make(chan struct{})
	updDone := make(chan struct{})
	r.Launch(webOp{
		Key:       "update",
		Exclusive: true,
		Run: func(emit func(string)) error {
			close(updRunning)
			return nil
		},
		Done: func(err error) { close(updDone) },
	})
	select {
	case <-updRunning:
		t.Fatal("exclusive op preempted a running op")
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-aDone
	<-updDone
}

// Op output streams as line events keyed by the op's ID — the SSE
// `line` class the log pane filters on.
func TestOpQueueStreamsLinesWithOpID(t *testing.T) {
	hub := newHub()
	r := newOpRunner(hub)
	ch := make(chan event, 16)
	hub.mu.Lock()
	hub.subs[ch] = struct{}{}
	hub.mu.Unlock()
	done := make(chan struct{})
	r.Launch(webOp{
		Key: "update",
		ID:  7,
		Run: func(emit func(string)) error {
			emit("line one")
			emit("line two")
			return nil
		},
		Done: func(err error) { close(done) },
	})
	<-done
	var got []string
	for _, ev := range hub.buf {
		if ev.class == "line" {
			got = append(got, ev.data)
		}
	}
	if len(got) != 2 || got[0] != `{"op":7,"text":"line one"}` || got[1] != `{"op":7,"text":"line two"}` {
		t.Fatalf("line events = %v, want op-7 lines in order", got)
	}
}
