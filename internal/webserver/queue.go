package webserver

import (
	"sync"
)

// opQueue is the web-side background-op manager (#158): it mirrors the
// taskmgr conflict semantics exactly (spec §9) — queue per workspace
// key, one-at-a-time per key, parallel across keys, no refusal path —
// in a thread-safe shape the HTTP handlers can call from any
// goroutine. (taskmgr.Manager is bound to the bubbletea model thread;
// the semantics, not the instance, are shared.) Every op's lines
// stream onto the SSE hub as Op.ID-keyed `line` events; every state
// change asks open tabs to re-render.
type opQueue struct {
	hub     *Hub
	mu      sync.Mutex
	seq     int
	running map[string]int              // key → running op id
	queued  map[string][]queuedOp       // key → FIFO of queued ops
}

// queuedOp is one enqueued or running operation.
type queuedOp struct {
	id   int
	key  string
	run  func(emit func(string)) error
}

func newOpQueue(hub *Hub) *opQueue {
	return &opQueue{
		hub:     hub,
		running: map[string]int{},
		queued:  map[string][]queuedOp{},
	}
}

// Launch enqueues the op behind its key's pending ops and returns the
// Op.ID the SSE line events carry. A free key starts at once; a busy
// key queues (never refuses).
func (q *opQueue) Launch(key string, run func(emit func(string)) error) int {
	q.mu.Lock()
	q.seq++
	id := q.seq
	op := queuedOp{id: id, key: key, run: run}
	if _, live := q.running[key]; live {
		q.queued[key] = append(q.queued[key], op)
		q.mu.Unlock()
		q.hub.PublishWorkspace()
		return id
	}
	q.running[key] = id
	q.mu.Unlock()
	q.hub.PublishWorkspace()
	go q.run(op)
	return id
}

// run executes one op, streaming its lines onto the hub, then hands
// the key to the queued successor (one-at-a-time per key).
func (q *opQueue) run(op queuedOp) {
	err := op.run(func(line string) { q.hub.PublishLine(op.id, line) })
	_ = err // failures surface in the streamed lines and the fresh render
	q.mu.Lock()
	delete(q.running, op.key)
	next, more := q.pop(op.key)
	if more {
		q.running[op.key] = next.id
	}
	q.mu.Unlock()
	q.hub.PublishWorkspace()
	if more {
		go q.run(next)
	}
}

// pop returns the head of the key's queue.
func (q *opQueue) pop(key string) (queuedOp, bool) {
	fifo := q.queued[key]
	if len(fifo) == 0 {
		return queuedOp{}, false
	}
	q.queued[key] = fifo[1:]
	return fifo[0], true
}

// BusyKey reports whether the key has an op queued or running — the
// confirm-refusal gate ("confirms refuse while an op is queued or
// running") and the rows' busy markers.
func (q *opQueue) BusyKey(key string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, live := q.running[key]; live {
		return true
	}
	return len(q.queued[key]) > 0
}
