package webserver

import (
	"sync"
)

// webOp is one queued background operation of the WebUI — taskmgr.Op's
// shape reconciled onto the SSE transport (spec §9): the update run
// today; workspace ops join through the same queue in later tickets.
// Run blocks until the operation finishes, reporting output lines
// through emit as they appear; Done reports the outcome on the server's
// own goroutine (never with a lock held).
type webOp struct {
	ID        int  // caller-owned handle; 0 = queue-assigned serial. The SSE `line` events key on it
	Key       string
	Exclusive bool // §12: the update run — while queued or running, nothing else starts
	Run       func(emit func(string)) error
	Done      func(err error)
}

// opRunner is the WebUI's background-op queue. It mirrors taskmgr's
// conflict semantics — one op at a time per key, parallel across keys —
// and adds the §12 global exclusivity: while an exclusive op (the
// update run) is queued or running the queue accepts nothing else
// across workspaces; queued ops wait behind it. Read-only actions are
// not ops and never touch the queue.
type opRunner struct {
	hub *Hub

	mu      sync.Mutex
	nextID  int
	pending []*webOp // launch order
	running map[string]*webOp
}

func newOpRunner(hub *Hub) *opRunner {
	return &opRunner{hub: hub, running: map[string]*webOp{}}
}

// Launch enqueues op and starts it at once when the queue rules allow
// (its key idle, nothing exclusive queued or running). Output lines
// stream to the hub as `line` events keyed by the op ID; Done(err) fires
// after the op's bookkeeping, so completion handlers may enqueue again.
func (r *opRunner) Launch(op webOp) {
	if op.ID == 0 {
		op.ID = r.reserve()
	}
	r.mu.Lock()
	r.pending = append(r.pending, &op)
	r.mu.Unlock()
	r.pump()
}

// reserve returns a globally unique op ID — callers that must publish
// op-keyed events before enqueuing (the update phase push) pre-assign it.
func (r *opRunner) reserve() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	return r.nextID
}

// pump starts every op the queue rules allow, oldest first, until none
// is startable.
func (r *opRunner) pump() {
	for {
		r.mu.Lock()
		var start *webOp
		for i, p := range r.pending {
			if r.canStartLocked(p) {
				start = p
				r.pending = append(r.pending[:i:i], r.pending[i+1:]...)
				r.running[p.Key] = p
				break
			}
		}
		r.mu.Unlock()
		if start == nil {
			return
		}
		r.run(start)
	}
}

// canStartLocked reports whether op may start right now (mu held):
// an exclusive op only on a fully idle queue; a plain op only when its
// key is idle AND no exclusive op is queued or running anywhere
// (§12 — the update run blocks everything across workspaces).
func (r *opRunner) canStartLocked(op *webOp) bool {
	if op.Exclusive {
		return len(r.running) == 0
	}
	if _, busy := r.running[op.Key]; busy {
		return false
	}
	for _, x := range r.running {
		if x.Exclusive {
			return false
		}
	}
	for _, x := range r.pending {
		if x.Exclusive {
			return false
		}
	}
	return true
}

// run executes the op: streams its lines to the hub, then clears its
// queue slot and pumps the successors before handing the outcome to
// Done — so a Done handler may launch again without deadlocking.
func (r *opRunner) run(op *webOp) {
	go func() {
		err := op.Run(func(line string) {
			r.hub.PublishLine(op.ID, line)
		})
		r.mu.Lock()
		delete(r.running, op.Key)
		r.mu.Unlock()
		r.pump()
		if op.Done != nil {
			op.Done(err)
		}
	}()
}
