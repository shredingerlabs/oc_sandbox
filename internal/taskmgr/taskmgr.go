// Package taskmgr is the background-task manager (#82): every long
// container operation (build, start, stop, first-run stage,
// script-delegating config flow) runs as a goroutine-owned tea.Cmd in
// ONE manager instance shared by the shell and all center views. One op
// at a time per project (others queue on the op's Key — the container
// identity); navigation stays free; states surface on the reserved
// status/toast rows via StatusMsg/DoneMsg and the shared streaming log
// transport via LineEvent (views filter by the op ID and feed
// shell.LineMsg into their embedded log pane).
//
// Metadata lives on the model thread: Launch, Update, CancelQueued and
// the queries are called from Update paths; the goroutines only execute
// Op.Run and push events over their channel.
package taskmgr

import (
	tea "github.com/charmbracelet/bubbletea"
)

// State is the lifecycle state of one task: queued → running →
// done/failed.
type State int

const (
	StateQueued State = iota + 1
	StateRunning
	StateDone
	StateFailed
)

// Op is the canonical descriptor of one long operation. It reconciles
// openproject.Op (ID/Title/Run), containerview's func-typed Op, and the
// newproject.Ops / settingsview.Ops seams — views adapt onto this one
// shape. Run blocks until the operation finishes, reporting output
// lines through emit as they appear.
//
// ID is opaque and caller-owned: views assign their own handle and get
// it echoed in every LineEvent, StatusMsg and DoneMsg so they filter
// messages of ops they did not start. A zero ID is replaced by a
// manager-assigned serial.
type Op struct {
	ID    int
	Key   string // queue bucket — one running op per key (container identity; "" = its own bucket)
	Title string // status/toast surface
	Run   func(emit func(string)) error
}

// DoneMsg reports a finished op. Streamed lines already went out as
// LineEvents while it ran; Lines again carries the full output for
// batch launchers (a view flushes them only when its pane is empty).
type DoneMsg struct {
	ID     int
	Key    string
	Err    error
	Lines  []string
	Serial int // manager-internal task handle; views filter by ID only
}

// StatusMsg surfaces a lifecycle state change for the reserved
// status/toast rows.
type StatusMsg struct {
	ID     int
	Key    string
	Title  string
	State  State
	Serial int // manager-internal task handle; views filter by ID only
}

// LineEvent is one streamed output line of the op with the given ID.
// Views filter on the ID of the op they started and convert to
// shell.LineMsg for their log pane — one shared streaming transport
// across build/start/stop/first-run screens.
type LineEvent struct {
	ID     int
	Text   string
	Serial int // manager-internal task handle; views filter by ID only
	Key    string
}

// Launcher is the single seam long operations leave the views through
// (openproject.Launcher's shape, reconciled).
type Launcher interface {
	Launch(op Op) tea.Cmd
}

// Manager launches, tracks and cancels background ops.
type Manager struct {
	nextID   int
	tasks    map[int]*task // internal serial → task
	perKey   map[string][]*task
	bySerial map[int]*task // internal serial → task (message routing)
}

// task is one enqueued or running operation.
type task struct {
	op    Op
	id    int // internal serial
	state State
	out   chan tea.Msg
	lines []string
}

// New returns an empty manager.
func New() *Manager {
	return &Manager{
		tasks:    map[int]*task{},
		perKey:   map[string][]*task{},
		bySerial: map[int]*task{},
	}
}

// Busy reports whether any task is queued or running — the guarded-exit
// query the shell makes before quitting.
func (m *Manager) Busy() bool {
	for key := range m.perKey {
		for _, t := range m.perKey[key] {
			if t.state == StateRunning || t.state == StateQueued {
				return true
			}
		}
	}
	return false
}

// BusyKey reports whether the key has a running or queued op (the
// still-running indicator on the owning project's cards/checklists).
func (m *Manager) BusyKey(key string) bool { return len(m.perKey[key]) > 0 }

// RunningTitle returns the title of the key's running op, ok=false when
// the key is idle — the still-running indicator label.
func (m *Manager) RunningTitle(key string) (string, bool) {
	for _, t := range m.perKey[key] {
		if t.state == StateRunning {
			return t.op.Title, true
		}
	}
	return "", false
}

// CancelQueued drops every queued op of the key (cancel-on-navigate);
// an already-running op keeps running to completion — its owning view
// discards the DoneMsg by its own bookkeeping.
func (m *Manager) CancelQueued(key string) {
	q := m.perKey[key]
	var keep []*task
	for _, t := range q {
		if t.state == StateRunning {
			keep = append(keep, t)
			continue
		}
		delete(m.tasks, t.id)
		delete(m.bySerial, t.id)
	}
	m.perKey[key] = keep
}

// Launch implements Launcher: start the op at once when its key is
// idle, otherwise queue it after the key's pending ops. It returns the
// tea.Cmd that yields the op's first event — or the queued StatusMsg
// cmd for an op that had to wait (the successor's start cmd is returned
// from Update when the running op finishes).
func (m *Manager) Launch(op Op) tea.Cmd {
	if op.ID == 0 {
		m.nextID++
		op.ID = m.nextID
	}
	m.nextID++
	id := m.nextID // unique internal serial — the ONLY routing key
	t := &task{op: op, id: id, state: StateQueued}
	m.tasks[id] = t
	m.bySerial[id] = t

	key := op.Key
	first := len(m.perKey[key]) == 0
	for _, x := range m.perKey[key] {
		if x.state == StateRunning {
			first = false
			break
		}
	}
	m.perKey[key] = append(m.perKey[key], t)
	if !first {
		return statusCmd(op.ID, key, op.Title, StateQueued, id)
	}
	return m.start(t)
}

// start flips the head task to running and returns the cmd that yields
// its first message; the goroutine streams lines and sends the final
// DoneMsg through the task's channel (which closes after it).
func (m *Manager) start(t *task) tea.Cmd {
	t.state = StateRunning
	t.out = make(chan tea.Msg)
	go func() {
		err := t.op.Run(func(line string) {
			t.lines = append(t.lines, line)
			t.out <- LineEvent{ID: t.op.ID, Text: line, Serial: t.id, Key: t.op.Key}
		})
		t.out <- DoneMsg{ID: t.op.ID, Key: t.op.Key, Err: err, Lines: t.lines, Serial: t.id}
		close(t.out)
	}()
	return tea.Batch(statusCmd(t.op.ID, t.op.Key, t.op.Title, StateRunning, t.id), func() tea.Msg {
		if msg, ok := <-t.out; ok {
			return msg
		}
		return nil
	})
}

// Update consumes the manager's own messages. The shell calls it exactly
// once per message, before broadcasting the message to every view. When
// a key's level frees up it returns the successor's start cmd batched
// with its Running StatusMsg.
func (m *Manager) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case LineEvent: // one more streaming event: wait for the next one
		t := m.bySerial[msg.Serial]
		if t == nil {
			return nil
		}
		return waitNext(t)
	case DoneMsg: // terminal: free the key, hand over the queued successor
		return m.done(msg)
	}
	return nil // StatusMsg etc.: state applied at Launch/start
}

// waitNext returns the cmd that yields the task's next event (nil after
// the channel closed — i.e. after the DoneMsg).
func waitNext(t *task) tea.Cmd {
	return func() tea.Msg {
		if msg, ok := <-t.out; ok {
			return msg
		}
		return nil
	}
}

// done marks the task terminal and starts the queued successor (the
// batch carries its Running StatusMsg plus its first streamed event).
func (m *Manager) done(done DoneMsg) tea.Cmd {
	t := m.bySerial[done.Serial]
	if t == nil {
		return nil
	}
	if t.state == StateDone || t.state == StateFailed {
		return nil // already terminal (duplicate delivery)
	}
	if done.Err != nil {
		t.state = StateFailed
	} else {
		t.state = StateDone
	}
	key := done.Key
	q := m.perKey[key]
	for i, x := range q {
		if x.id == t.id {
			q = append(q[:i:i], q[i+1:]...)
			break
		}
	}
	m.perKey[key] = q
	delete(m.bySerial, t.id)
	if len(q) > 0 && q[0].state == StateQueued {
		return m.start(q[0])
	}
	return nil
}

func statusCmd(id int, key, title string, state State, serial int) tea.Cmd {
	return func() tea.Msg {
		return StatusMsg{ID: id, Key: key, Title: title, State: state, Serial: serial}
	}
}
