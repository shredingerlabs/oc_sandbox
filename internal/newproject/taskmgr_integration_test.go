package newproject

import (
	"fmt"
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
	createCmd(t, m) // build prompt up (image missing)
	cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
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
