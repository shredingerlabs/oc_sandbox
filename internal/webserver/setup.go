// First-run setup interaction in the WebUI (#161, spec §11): the
// unchanged setupflow machine drives the pipeline (container start →
// clone → CBM) as ONE streamed op; when the interactive skills stage
// arrives the server auto-opens a host terminal window (§3 machinery)
// running the exact podman-exec prompt wrapped with a status-file exit
// relay (`…; printf '%s' $? > <status-file>`). A poller feeds the exit
// code into the machine — 0 → flag patch, 2 → user abort (no flags),
// else granular recovery — followed by the SSE push. A hard-closed
// window stays pending-stale until the user acts: Cancel setup marks
// the session abandoned (no flags, partial progress preserved,
// recovery actions reappear) and a late status file for a cancelled
// session is ignored (session guard). Headless: the slot shows the
// exact copyable manual command and stays pending — completion flows
// through the same relay. While a session holds, it blocks the
// workspace's other actions under the one-at-a-time rule; the pending
// session itself is NOT a task-manager task.
package webserver

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/setupflow"
)

// setupPollInterval is the status-file poll cadence — the relay latency
// between the terminal's exit and the SSE push.
var setupPollInterval = 100 * time.Millisecond

// setupSession is one first-run setup run per workspace (registry-name
// keyed): the machine behind the streamed op plus, once the skills
// stage arrives, the pending interactive session state.
type setupSession struct {
	name       string // registry name (URL identity)
	key        string // queue key: the workspace's container name
	opID       int    // the streamed op — relay lines key on it
	machine    *setupflow.Machine
	stager     setupflow.Stager // the per-root stage runner the machine drives
	command    string           // the exact wrapper command (manual-command copy)
	statusFile string           // the relay file the wrapper's exit code lands in
	window     bool             // a host terminal window opened (false = headless)
	pending    bool             // the skills stage is awaiting the status file
}

// setupState is the server-side session table. Sessions are removed on
// completion, abort, failure and cancel — removal IS the guard: a
// status file arriving for a session no longer in the table is ignored.
type setupState struct {
	mu     sync.Mutex
	setups map[string]*setupSession
}

// sessionFor returns the live session for name (nil when none).
func (st *setupState) sessionFor(name string) *setupSession {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.setups[name]
}

// put registers a session.
func (st *setupState) put(sess *setupSession) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.setups[sess.name] = sess
}

// remove drops the session; the removed session is returned when it
// was still the live one (the cancel/relay guard).
func (st *setupState) remove(name string) *setupSession {
	st.mu.Lock()
	defer st.mu.Unlock()
	sess, ok := st.setups[name]
	if ok {
		delete(st.setups, name)
	}
	return sess
}

// setPending marks the skills stage of the live session as awaiting the
// status file, capturing the wrapper command and its relay path.
func (st *setupState) setPending(name, command, statusFile string, window bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if sess := st.setups[name]; sess != nil {
		sess.command = command
		sess.statusFile = statusFile
		sess.window = window
		sess.pending = true
	}
}

// view snapshots the card-render fields of the live session (lock-held:
// the poller mutates them when the skills stage opens).
func (st *setupState) view(name string) (pending, window bool, command string, opID int) {
	st.mu.Lock()
	defer st.mu.Unlock()
	sess := st.setups[name]
	if sess == nil {
		return false, false, "", 0
	}
	return sess.pending, sess.window, sess.command, sess.opID
}

// launchSetup starts one setup run for the workspace: the machine op is
// queued per workspace key (one-at-a-time per workspace, parallel
// across workspaces); it refuses while a setup session holds or the
// workspace's queue slot is busy. The returned message is the refusal
// ("" on success).
func (s *Server) launchSetup(ws registry.Workspace) string {
	if s.setups.sessionFor(ws.Name) != nil {
		return "refused — a first-run setup session is already in progress for " + ws.Name
	}
	key := registry.ContainerName(ws.ContainerID)
	if s.queue.BusyKey(key) || s.queue.BusyKey(ws.Name) {
		return "refused — an operation is already queued or running for " + ws.Name
	}
	stager := s.setupStager()(ws.Path)
	machine, err := setupflow.NewMachine(setupflow.Deps{
		Home: s.home(), Root: ws.Path, Reg: s.regView(), Stager: stager,
	})
	if err != nil {
		return "refused — first-run setup could not start: " + err.Error()
	}
	if machine.Phase() == setupflow.PhaseDone {
		return "refused — first-run setup is already complete for " + ws.Name
	}
	opID := s.queue.reserve()
	sess := &setupSession{name: ws.Name, key: key, opID: opID, machine: machine, stager: stager}
	s.setups.put(sess)
	s.queue.Launch(webOp{
		ID:  opID,
		Key: key,
		Run: func(emit func(string)) error { return s.runSetupStages(sess, emit) },
		Done: func(err error) {
			if err == nil || errors.Is(err, setupflow.ErrAbort) {
				s.hub.PublishWorkspaceOp(opID, "done")
			} else {
				s.hub.PublishWorkspaceOp(opID, "failed")
			}
		},
	})
	return ""
}

// setupStager returns the per-root stage-runner factory for the wired
// home (deps-first script paths, ADR-0007 — the TUI invokes the same
// scripts).
func (s *Server) setupStager() setupflow.RunnerFactory {
	layout := app.LayoutFor(s.opts.Home)
	start, configure := layout.StartScript, layout.ConfigureScript
	if s.deps != nil {
		if s.deps.StartScript != "" {
			start = s.deps.StartScript
		}
		if s.deps.ConfigureScript != "" {
			configure = s.deps.ConfigureScript
		}
	}
	return setupflow.NewStagerFor(s.runnerSeam(), s.home(), s.regView(), start, configure)
}

// runSetupStages drives the machine through its non-interactive stages,
// streaming every stage into the op's log lines. When the skills stage
// arrives it hands off to the host-terminal session and the op ends —
// the pending session is NOT a task-manager task (§11).
func (s *Server) runSetupStages(sess *setupSession, emit func(string)) error {
	for {
		if s.setups.sessionFor(sess.name) != sess {
			return nil // cancelled/abandoned mid-run: no further stages
		}
		stage := sess.machine.Stage()
		if stage == "" {
			return nil // done (setup_complete written) or terminal
		}
		if sess.machine.Interactive() { // StageSkills only (ADR 0014)
			return s.openSkillsSession(sess, emit)
		}
		emit("==> " + sess.machine.Title())
		err := sess.stager.Run(stage, emit)
		if doneErr := sess.machine.Done(err); doneErr != nil {
			return doneErr
		}
	}
}

// openSkillsSession auto-opens the host terminal window running the
// exact podman-exec prompt wrapped with the status-file relay — or, on
// the headless fallback (§3), leaves the exact copyable manual command
// in the slot and stays pending. Either way completion flows through
// the same relay poller.
func (s *Server) openSkillsSession(sess *setupSession, emit func(string)) error {
	statusDir, err := os.MkdirTemp("", "oc-sandbox-setup-")
	if err != nil {
		return err
	}
	statusFile := filepath.Join(statusDir, "exit")
	command := fmt.Sprintf("%s; printf '%%s' $? > %s", setupflow.SkillsShellCommand(sess.key), statusFile)

	err = s.terminalSpawn()(command)
	window := err == nil
	if window {
		emit("Skills setup — interactive session open in a terminal window")
	} else {
		emit("No terminal window available — run the manual command below in your own terminal to complete the skills setup.")
	}
	emit(command)
	s.setups.setPending(sess.name, command, statusFile, window)
	go s.pollSkillsExit(sess)
	return nil
}

// pollSkillsExit polls the status file until it appears (the wrapper's
// printf) and relays the exit code into the machine. A session that
// left the table (cancel, server close) stops the poller — no status
// file is ever read for a cancelled session (session guard).
func (s *Server) pollSkillsExit(sess *setupSession) {
	for {
		select {
		case <-s.closed:
			return
		case <-time.After(setupPollInterval):
		}
		if s.setups.sessionFor(sess.name) != sess {
			return // cancelled/abandoned: late status files are ignored
		}
		data, err := os.ReadFile(sess.statusFile)
		if err != nil {
			continue // no status file yet — pending-stale until the user acts
		}
		code, perr := strconv.Atoi(strings.TrimSpace(string(data)))
		if perr != nil {
			code = 1 // malformed relay → granular recovery
		}
		s.relaySkillsExit(sess, code)
		return
	}
}

// relaySkillsExit feeds the exit code into the UNCHANGED setup machine
// — 0 → flag patch (setup_skills_complete, then setup_complete last),
// 2 → user abort with no flags (ADR 0011), else the error phase whose
// recovery actions reappear — and pushes the SSE re-render.
func (s *Server) relaySkillsExit(sess *setupSession, code int) {
	if s.setups.remove(sess.name) != sess {
		return // session guard: cancelled between the read and the relay
	}
	var outcome error
	switch code {
	case 0:
		outcome = nil
	case 2:
		outcome = setupflow.ErrAbort
	default:
		outcome = fmt.Errorf("skills setup exited %d", code)
	}
	err := sess.machine.Done(outcome)
	switch {
	case code == 0 && err == nil:
		s.hub.PublishLine(sess.opID, "Skills setup complete — first-run setup finished.")
	case errors.Is(outcome, setupflow.ErrAbort):
		s.hub.PublishLine(sess.opID, "Skills setup aborted by the user — no flags written; retry from the workspace card.")
	default:
		s.hub.PublishLine(sess.opID, "Skills setup failed — partial progress is preserved; retry from the workspace card.")
	}
	s.hub.PublishWorkspace()
}

// handleSetupRetry serves POST /workspaces/{name}/setup/retry (§9,
// first-run setup recovery): it (re-)starts the machine from the stored
// flags — partial progress is preserved, completed stages do not rerun.
func (s *Server) handleSetupRetry(w http.ResponseWriter, r *http.Request) {
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if msg := s.launchSetup(ws); msg != "" {
		http.Error(w, msg, http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleSetupCancel serves the card's Cancel setup action: the pending
// session is marked abandoned — no flags, partial progress preserved,
// recovery actions reappear; a late status file is ignored (session
// guard).
func (s *Server) handleSetupCancel(w http.ResponseWriter, r *http.Request) {
	ws, err := s.lookupWorkspace(r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sess := s.setups.remove(ws.Name)
	if sess == nil {
		http.Error(w, "no pending first-run setup session for "+ws.Name, http.StatusConflict)
		return
	}
	s.hub.PublishLine(sess.opID, "Skills setup cancelled — partial progress is preserved; retry from the workspace card.")
	s.hub.PublishWorkspace()
	w.WriteHeader(http.StatusOK)
}

// setupHolds reports whether a setup session (running op or pending
// skills stage) holds the workspace — the one-at-a-time gate the
// workspace's other action POSTs refuse under.
func (s *Server) setupHolds(name string) bool {
	return s.setups.sessionFor(name) != nil
}
