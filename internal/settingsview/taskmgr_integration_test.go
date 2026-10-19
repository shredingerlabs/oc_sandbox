package settingsview

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
	"github.com/shredingerlabs/oc-sandbox/internal/testutil"
)

// #82: with the shared task manager wired, the uninstall / export /
// restore ops run as queued tasks and their continuations fire from
// taskmgr.DoneMsg. Without a manager they stay synchronous (all the
// other tests in this package).

func queueSettings(t *testing.T, home string, run *cmdrun.Fake) (*View, *taskmgr.Manager) {
	v := newSettings(t, home, run)
	mgr := taskmgr.New()
	v.Tasks = mgr
	return v, mgr
}

func TestUninstallThroughSharedQueue(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v, mgr := queueSettings(t, home, run)
	v.Update(shell.SubTabMsg{Index: subUninstall})
	for i := 0; i < 3; i++ {
		key(v, "down")
	}
	typeText(v, "UnInStAlL")
	cmd := key(v, "enter")
	cmd = key(v, "enter") // confirm field → button → popup (ADR-0021)
	cmd = key(v, "tab")   // popup: switch to the Confirm button
	cmd = key(v, "enter")
	if !v.Busy() || !mgr.BusyKey("settings") {
		t.Fatal("uninstall op not queued / not busy")
	}
	testutil.Drain(t, v, mgr, cmd)
	time.Sleep(50 * time.Millisecond)
	if v.Busy() || mgr.BusyKey("settings") {
		t.Fatal("still busy after completion")
	}
	if n := len(bashCalls(run)); n != 1 {
		t.Fatalf("uninstall ran %d times, want 1 (%v)", n, run.Calls)
	}
	if !strings.Contains(v.toast, "Uninstall script finished") {
		t.Fatalf("continuation toast missing: %q", v.toast)
	}
}

func TestExportThroughSharedQueue(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v, mgr := queueSettings(t, home, run)
	pp := config.Paths{Home: home}.ProjectsPath()
	writeFile(t, pp, "{\n  \"projects\": [],\n  \"version\": \"1.0\"\n}")
	dest := filepath.Join(home, "exported-projects.json")
	v.Update(shell.SubTabMsg{Index: subExport})
	clickLabel(t, v, "Export Config…")
	clearField(v, len(home)+129)
	typeText(v, dest)
	key(v, "down")
	cmd := key(v, "enter") // Save
	if !mgr.BusyKey("settings") {
		t.Fatal("export op not queued")
	}
	testutil.Drain(t, v, mgr, cmd)
	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(v.toast, "Exported") {
		t.Fatalf("continuation toast missing: %q", v.toast)
	}
	if got := readFile(t, dest); !strings.Contains(got, "projects") {
		t.Fatalf("snapshot not written: %q", got)
	}
}

func TestRestoreThroughSharedQueueWritesActiveFile(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v, mgr := queueSettings(t, home, run)
	pp := config.Paths{Home: home}.ProjectsPath()
	const before = "{\n    \"projects\": []\n  }"
	writeFile(t, pp, before)
	writeBackup(t, home, "projects.json.20260101T000000000.aaa111", "{\n  \"projects\": [\n    { \"restored\": true }\n  ],\n  \"version\": \"1.0\"\n}")
	v.Update(shell.SubTabMsg{Index: subRestore})
	cmd := v.doRestore() // restore action directly (keyboard parity tested elsewhere)
	if !mgr.BusyKey("settings") {
		t.Fatal("restore op not queued")
	}
	testutil.Drain(t, v, mgr, cmd)
	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(v.toast, "Restored") {
		t.Fatalf("continuation toast missing: %q", v.toast)
	}
	if got := readFile(t, pp); !strings.Contains(got, "restored") {
		t.Fatalf("active file untouched after queued restore: %q", got)
	}
}

func TestForeignTaskDoneUnhandled(t *testing.T) {
	home := homeFixture(t)
	v, mgr := queueSettings(t, home, fakeRunner())
	mgr.Launch(taskmgr.Op{
		ID: 42, Key: "unrelated",
		Run: func(emit func(string)) error { return nil },
	})
	if cmd := v.Update(taskmgr.DoneMsg{ID: 42, Key: "unrelated"}); cmd != nil {
		t.Fatalf("foreign task touched the view: %v", cmd)
	}
}
