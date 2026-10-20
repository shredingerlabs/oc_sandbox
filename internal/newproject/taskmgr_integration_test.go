package newproject

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
	"github.com/shredingerlabs/oc-sandbox/internal/testutil"
)

// #82: with the shared task manager wired, every script op is a queued
// task and the create flow advances via taskmgr.DoneMsg continuations
// (navigation stays free meanwhile). Without the manager the legacy
// synchronous path runs (all other tests in this package).

func queuedCreateModel(t *testing.T, mods ...func(*recOps, *cmdrun.Fake)) (*Model, *recOps, *registry.Registry, *taskmgr.Manager) {
	m, ops, reg := newTestModel(t, mods...)
	mgr := taskmgr.New()
	m.Tasks = mgr
	return m, ops, reg, mgr
}

func createCmd(t *testing.T, m *Model) tea.Cmd {
	t.Helper()
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // confirm popup (ADR-0021)
	if !m.confirm.IsOpen() {
		t.Fatalf("create popup missing:\n%s", plain(t, m))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // popup: switch to Create
	return m.Update(tea.KeyMsg{Type: tea.KeyEnter})
}

func settle(t *testing.T) { time.Sleep(50 * time.Millisecond) }

func TestAsyncCreateFlowsThroughTaskQueue(t *testing.T) {
	m, ops, reg, mgr := queuedCreateModel(t)
	typeKeys(t, m, "asyncproj")
	cmd := createCmd(t, m)
	if !mgr.BusyKey("newproject") {
		t.Fatal("init op not queued through the shared manager")
	}
	testutil.Drain(t, m, mgr, cmd)
	settle(t)
	if mgr.BusyKey("newproject") {
		t.Fatal("queue key not freed after the create chain finished")
	}
	if !ops.has("init-project.sh") {
		t.Fatalf("init op missing: %v", ops.Calls)
	}
	if p, err := reg.ByName("asyncproj"); err != nil || p.ContainerStatus != "stopped" {
		t.Fatalf("registry write missing: %+v %v", p, err)
	}
	if !m.Created() {
		t.Fatal("Created() false after task-driven creation")
	}
	if m.textVal(fName) != "" || m.textVal(fPath) != "" || m.source != srcBlank {
		t.Fatal("async create did not reset the form (unsuccessful create must keep entries)")
	}
}

func TestAsyncBuildNowQueuesBuildThenContinues(t *testing.T) {
	m, ops, reg, mgr := queuedCreateModel(t, func(ops *recOps, run *cmdrun.Fake) {
		builds := 0
		run.Script = func(name string, args []string) (string, error) {
			switch {
			case name == "bash" && len(args) >= 1 && args[0] == "start.sh":
				return "--offline --cbm_ui --hil_mode\n", nil
			case name == "podman" && len(args) >= 3 && args[0] == "image":
				if builds == 0 {
					return "", fmt.Errorf("no image")
				}
				return "", nil
			}
			return "", nil
		}
		ops.Fake = func(call []string) error {
			if call[0] == "build-container.sh" {
				builds++
			}
			return nil
		}
	})
	typeKeys(t, m, "asyncbuild")
	// the inline "Image build" radio defaults to Build now; confirming the
	// popup queues the build directly (no separate prompt step)
	cmd := createCmd(t, m)
	if !mgr.BusyKey("newproject") {
		t.Fatal("build op not queued")
	}
	testutil.Drain(t, m, mgr, cmd)
	settle(t)
	if !ops.has("build-container.sh", "swdev") {
		t.Fatalf("build op missing: %v", ops.Calls)
	}
	if !ops.has("init-project.sh") {
		t.Fatalf("continuation did not run init: %v", ops.Calls)
	}
	if _, err := reg.ByName("asyncbuild"); err != nil {
		t.Fatalf("chain did not finish: %v", err)
	}
}

func TestForeignTasksIgnoredInView(t *testing.T) {
	m, _, _, mgr := queuedCreateModel(t)
	mgr.Launch(taskmgr.Op{
		ID: 777, Key: "container-other", Title: "foreign",
		Run: func(emit func(string)) error { return nil },
	})
	done := taskmgr.DoneMsg{ID: 777, Key: "container-other"}
	if cmd := m.Update(done); cmd != nil {
		t.Fatalf("foreign done handled: %v", cmd)
	}
}

// The Build-now chain streams into an embedded log pane below the form
// (same as the Container center Build tab): each queued step announces
// itself as a header line, foreign keys/lines stay out, and the pane
// stays readable after a failure.

func buildThenExists(mods ...func(*recOps, *cmdrun.Fake)) func(*recOps, *cmdrun.Fake) {
	return func(ops *recOps, run *cmdrun.Fake) {
		builds := 0
		run.Script = func(name string, args []string) (string, error) {
			switch {
			case name == "bash" && len(args) >= 1 && args[0] == "start.sh":
				return "--offline --cbm_ui --hil_mode\n", nil
			case name == "podman" && len(args) >= 3 && args[0] == "image":
				if builds == 0 {
					return "", fmt.Errorf("no image")
				}
				return "", nil
			}
			return "", nil
		}
		ops.Fake = func(call []string) error {
			if call[0] == "build-container.sh" {
				builds++
			}
			return nil
		}
		for _, mod := range mods {
			mod(ops, run)
		}
	}
}

func TestLogPaneStreamsBuildNowChain(t *testing.T) {
	m, ops, reg, mgr := queuedCreateModel(t, buildThenExists())
	typeKeys(t, m, "streamy")
	cmd := createCmd(t, m)
	if !m.busy || !m.log.Spinning() {
		t.Fatal("chain not marked busy with a spinning pane")
	}
	if lines := m.log.Lines(); len(lines) != 1 || !strings.Contains(lines[0], "==> Building image swdev") {
		t.Fatalf("step header missing: %q", m.log.Lines())
	}
	if body := plain(t, m); !strings.Contains(body, "==> Building image swdev") {
		t.Fatalf("pane not rendered below the form while busy:\n%s", body)
	}
	// streamed output lines ride the pane; foreign ones do not
	m.Update(taskmgr.LineEvent{ID: 1, Key: "newproject", Text: "STEP 1/3"})
	m.Update(taskmgr.LineEvent{ID: 1, Key: "container-other", Text: "foreign"})
	got := strings.Join(m.log.Lines(), "\n")
	if !strings.Contains(got, "STEP 1/3") || strings.Contains(got, "foreign") {
		t.Fatalf("pane lines wrong: %q", got)
	}
	testutil.Drain(t, m, mgr, cmd)
	settle(t)
	if m.busy || m.log.Spinning() {
		t.Fatal("pane still spinning after the chain finished")
	}
	if m.chainFailed {
		t.Fatal("successful chain marked failed")
	}
	if !ops.has("init-project.sh") || !m.Created() {
		t.Fatalf("chain did not complete (created=%v, ops=%v)", m.Created(), ops.Calls)
	}
	if p, err := reg.ByName("streamy"); err != nil {
		t.Fatalf("project not registered: %v", err)
	} else if p.ContainerStatus != "stopped" {
		t.Fatalf("unexpected status: %+v", p)
	}
}

func TestFailedChainKeepsLogPane(t *testing.T) {
	m, _, _, mgr := queuedCreateModel(t, buildThenExists(func(ops *recOps, _ *cmdrun.Fake) {
		ops.Fake = func(call []string) error {
			if call[0] == "build-container.sh" {
				return fmt.Errorf("podman build exploded")
			}
			return nil
		}
	}))
	typeKeys(t, m, "broken")
	cmd := createCmd(t, m)
	testutil.Drain(t, m, mgr, cmd)
	settle(t)
	if m.busy || !m.chainFailed {
		t.Fatalf("chain-end state wrong: busy=%v failed=%v", m.busy, m.chainFailed)
	}
	if !strings.Contains(m.ToastText(), "Build failed") {
		t.Fatalf("failure toast missing: %q", m.ToastText())
	}
	body := plain(t, m)
	if !strings.Contains(body, "==> Building image swdev") {
		t.Fatalf("pane not readable after failure:\n%s", body)
	}
	// retrying starts a fresh pane
	cmd = createCmd(t, m)
	if lines := m.log.Lines(); len(lines) != 1 {
		t.Fatalf("retry did not reset the pane: %q", m.log.Lines())
	}
	testutil.Drain(t, m, mgr, cmd)
}

func TestBusyPaneSwallowsKeysAndClicks(t *testing.T) {
	m, _, _, mgr := queuedCreateModel(t, buildThenExists())
	typeKeys(t, m, "inert")
	cmd := createCmd(t, m)
	m.Update(tea.KeyMsg{Type: tea.KeyTab}) // must scroll the log, not move the focus
	if !m.busy {
		t.Fatal("chain lost mid-run")
	}
	testutil.Drain(t, m, mgr, cmd)
}
