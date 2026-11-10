// Op queue (#153, spec §9 "mirror taskmgr exactly"): queued-not-refused
// background ops keyed per workspace — one at a time per workspace
// (others queue on the container identity), parallel across workspaces,
// no 409-refusal path. Ops run as plain goroutines (the webserver has
// no bubbletea loop); progress streams through the SSE hub as line
// events keyed by the op id, and every completion publishes the
// workspace event so open tabs re-render their cards reactively.
package webserver

import "sync"

// opFn is one queued operation: it reports its streamed output lines
// through emit (each lands as an SSE line event for the op id) and
// returns the final error, if any.
type opFn func(emit func(string)) error

// opQueue serializes ops per key.
type opQueue struct {
	hub    *Hub
	mu     sync.Mutex
	chains map[string]*opChain
	nextID int // op id serial — echoed in every line event
}

// opChain is one key's FIFO: an idle drain goroutine walks the queue.
type opChain struct {
	pending []queuedOp
	running bool
}

type queuedOp struct {
	id int
	fn opFn
}

func newOpQueue(hub *Hub) *opQueue {
	return &opQueue{hub: hub, chains: map[string]*opChain{}, nextID: 1000}
}

// Busy reports whether an op is queued or running for the key — the
// card's busy state and the confirm-refusal condition.
func (q *opQueue) Busy(key string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	c := q.chains[key]
	return c != nil && (c.running || len(c.pending) > 0)
}

// Enqueue queues the op under key and returns its op id (never zero).
// Queued-not-refused: an existing op on the key delays this one; ops on
// other keys run in parallel.
func (q *opQueue) Enqueue(key string, fn opFn) int {
	q.mu.Lock()
	q.nextID++
	id := q.nextID
	c := q.chains[key]
	if c == nil {
		c = &opChain{}
		q.chains[key] = c
	}
	c.pending = append(c.pending, queuedOp{id: id, fn: fn})
	idle := !c.running
	if idle {
		c.running = true
	}
	q.mu.Unlock()
	if idle {
		go q.drain(c)
	}
	return id
}

// drain runs the chain's ops one at a time until the queue empties:
// each op's emit lands as a line event; every completion (success or
// failure) re-renders the cards via the workspace event.
func (q *opQueue) drain(c *opChain) {
	for {
		q.mu.Lock()
		if len(c.pending) == 0 {
			c.running = false
			q.mu.Unlock()
			return
		}
		op := c.pending[0]
		c.pending = c.pending[1:]
		q.mu.Unlock()

		err := op.fn(func(line string) { q.hub.PublishLine(op.id, line) })
		q.hub.PublishWorkspace()
		_ = err // failures surface through the streamed log + re-render
	}
}
