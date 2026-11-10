package webserver

import (
	"errors"
	"fmt"
	"sync"

	"github.com/shredingerlabs/oc-sandbox/internal/openworkspace"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// webOp is one queued background operation of the WebUI — taskmgr.Op's
// shape reconciled onto the SSE transport (spec §9): the update run
// today; workspace ops join through the same queue in later tickets.
// Run blocks until the operation finishes, reporting output lines
// through emit as they appear; Done reports the outcome on the server's
// own goroutine (never with a lock held).
type webOp struct {
	ID        int // caller-owned handle; 0 = queue-assigned serial. The SSE `line` events key on it
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
	r.hub.PublishWorkspace() // queued/running state changes re-render the busy markers (spec §9)
	r.pump()
}

// BusyKey reports whether the key has an op queued or running — the
// confirm-refusal gate ("confirms refuse while an op is queued or
// running", spec §9) and the rows' busy markers.
func (r *opRunner) BusyKey(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, busy := r.running[key]; busy {
		return true
	}
	for _, p := range r.pending {
		if p.Key == key {
			return true
		}
	}
	return false
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

// Busy reports whether any op is queued or running — the §9/§12
// confirmation refusal: confirm-gated POSTs refuse while it holds.
func (r *opRunner) Busy() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.running) > 0 || len(r.pending) > 0
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
		r.hub.PublishWorkspace() // the key freed / successor started: rows re-render
		if op.Done != nil {
			op.Done(err)
		}
	}()
}

// --- container/image op helpers (#158): thin adapters over
// sandboxcfg/openworkspace so the web handlers mirror the TUI's start
// validations (openworkspace + containerview semantics). ---

// readSandboxCfg loads the workspace's stored sandbox_config.json.
func readSandboxCfg(root string) (sandboxcfg.Config, error) {
	return sandboxcfg.Read(sandboxcfg.Path(root))
}

// validateStartCfg mirrors the TUI's pre-start validations: offline +
// cbm_ui are mutually exclusive, SearXNG search needs network and no
// proxy (ADR 0026) — start.sh would hard-exit on both.
func validateStartCfg(cfg sandboxcfg.Config) error {
	if !openworkspace.ValidModes(cfg.ContainerModes) {
		return errors.New("offline and cbm_ui are mutually exclusive — fix settings first")
	}
	if !openworkspace.ValidSearchCombination(cfg.SearchMode, cfg.ContainerModes, cfg.UseProxy) {
		return errors.New("searxng search requires network and no proxy (ADR 0026) — fix settings first")
	}
	return nil
}

// startScriptArgs rebuilds the start.sh argument vector exactly as the
// TUI does (openworkspace.StartArgs; always detached). The retired
// start_option reads as web (ADR-0033): the web start variant is the
// container center's and the restart's default — the cards grant the
// other access variants explicitly.
func startScriptArgs(root, containerID string, cfg sandboxcfg.Config) []string {
	return openworkspace.StartArgs(root, cfg.ContainerEdition, containerID,
		cfg.ContainerModes, "web", cfg.UseProxy, cfg.SearchMode)
}

func errImageMissing(edition string) error {
	return fmt.Errorf("image opencode-sandbox-%s is not built yet", edition)
}

func errNotRunning(name string) error {
	return fmt.Errorf("%s is not running", name)
}

func errStillRunning(name string) error {
	return fmt.Errorf("%s is still running", name)
}

func errStillExists(name string) error {
	return fmt.Errorf("%s still exists", name)
}

func errStopFirst(name string) error {
	return fmt.Errorf("stop it first: podman stop %s", name)
}

func errStopFirstPod(name string) error {
	return fmt.Errorf("stop it first: podman pod stop %s", name)
}
