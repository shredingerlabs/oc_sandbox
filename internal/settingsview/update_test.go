package settingsview

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
	"github.com/shredingerlabs/oc-sandbox/internal/testutil"
)

// Update menu (Settings → Update, left of Uninstall): version display,
// the startup update check, the confirmation-popup-gated install-script
// run and the footer badge.

// ---- version comparison and source --------------------------------------

func TestUpdateAvailable(t *testing.T) {
	cases := []struct {
		installed, latest string
		want              bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.2.1", "v0.2.0", false},
		{"v0.10.0", "v0.9.0", false},
		{"dev", "v0.2.0", true},  // unparseable counts as outdated
		{"v0.2.0", "junk", true}, // …in either tag
		{"", "", false},          // no information → no update claim
		{"v0.2.0", "", false},    // check failed → silent
		{"v1.2.3", "v1.3.0", true},
	}
	for _, c := range cases {
		if got := updateAvailable(c.installed, c.latest); got != c.want {
			t.Errorf("updateAvailable(%q, %q) = %v, want %v", c.installed, c.latest, got, c.want)
		}
	}
}

func TestInstalledVersionPrefersVersionFile(t *testing.T) {
	home := homeFixture(t)
	v := newUpdateSettings(t, home)
	if got := v.installedVersion(); got != "v0.1.0" {
		t.Fatalf("installed = %q, want the compiled version v0.1.0", got)
	}
	writeFile(t, v.installRoot+"/VERSION", "v0.3.0\n")
	if got := v.installedVersion(); got != "v0.3.0" {
		t.Fatalf("installed = %q, want the VERSION file's v0.3.0", got)
	}
}

func TestInstalledVersionUnknownWithoutSource(t *testing.T) {
	home := homeFixture(t)
	v := New(home)
	if got := v.installedVersion(); got != "unknown" {
		t.Fatalf("installed = %q, want unknown", got)
	}
}

// ---- pane content ---------------------------------------------------------

func TestUpdateMenuSitsLeftOfUninstall(t *testing.T) {
	home := homeFixture(t)
	v := newUpdateSettings(t, home)
	items, _ := v.SubTabs()
	if len(items) != 6 {
		t.Fatalf("tabs = %v, want 6 entries", items)
	}
	if items[subUpdate] != "Update" || items[subUninstall] != "Uninstall" {
		t.Fatalf("tab order wrong: Update must sit directly left of Uninstall: %v", items)
	}
}

func TestUpdatePaneShowsVersions(t *testing.T) {
	home := homeFixture(t)
	v := newUpdateSettings(t, home)
	v.HookMsg(UpdateCheckMsg{Latest: "v0.2.0"})
	v.Update(shell.SubTabMsg{Index: subUpdate})
	b := body(v)
	for _, want := range []string{
		"Installed version: v0.1.0",
		"Latest version: v0.2.0",
		"Update available!",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("update pane missing %q:\n%s", want, b)
		}
	}
}

func TestUpdatePaneUpToDate(t *testing.T) {
	home := homeFixture(t)
	v := newUpdateSettings(t, home)
	v.Version = "v0.2.0"
	v.installed = v.installedVersion()
	v.HookMsg(UpdateCheckMsg{Latest: "v0.2.0"})
	v.Update(shell.SubTabMsg{Index: subUpdate})
	if b := body(v); !strings.Contains(b, "You are up to date.") || strings.Contains(b, "Update available!") {
		t.Errorf("same tag must read as up to date:\n%s", b)
	}
}

func TestUpdatePaneCheckFailedSilent(t *testing.T) {
	home := homeFixture(t)
	v := newUpdateSettings(t, home)
	v.HookMsg(UpdateCheckMsg{Err: errors.New("offline")})
	v.Update(shell.SubTabMsg{Index: subUpdate})
	b := body(v)
	if !strings.Contains(b, "not available") {
		t.Errorf("failed check must say so in the pane:\n%s", b)
	}
	if text, tab := v.UpdateBadge(); text != "" || tab != 0 {
		t.Errorf("failed check must leave no badge, got (%q, %d)", text, tab)
	}
}

func TestUpdatePaneStillChecking(t *testing.T) {
	home := homeFixture(t)
	v := newUpdateSettings(t, home)
	v.Update(shell.SubTabMsg{Index: subUpdate})
	if b := body(v); !strings.Contains(b, "checking") {
		t.Errorf("pre-check pane must say it is checking:\n%s", b)
	}
}

// ---- startup check --------------------------------------------------------

func TestStartupCheckRunsAndAppliesViaHook(t *testing.T) {
	home := homeFixture(t)
	v := newUpdateSettings(t, home)
	v.check = func() (string, error) { return "v0.9.0", nil }
	msg := v.StartupCheck()()
	m, ok := msg.(UpdateCheckMsg)
	if !ok || m.Latest != "v0.9.0" || m.Err != nil {
		t.Fatalf("StartupCheck msg = %+v, want Latest v0.9.0", msg)
	}
	consumed, cmd := v.HookMsg(m)
	if consumed || cmd != nil {
		t.Fatalf("hook must stay passive (consumed=%v cmd=%v)", consumed, cmd)
	}
	if v.upLatest != "v0.9.0" {
		t.Fatalf("hook did not apply the check result")
	}
}

func TestStartupContinuationTimesOutFast(t *testing.T) {
	// The startup check must never hang the shell: the HTTP client uses a
	// short timeout (guarded here so a refactor cannot drop it).
	if checkTimeout <= 0 || checkTimeout > 10*time.Second {
		t.Fatalf("check timeout = %v, want a short startup-friendly bound", checkTimeout)
	}
}

// ---- badge ----------------------------------------------------------------

func TestBadgeStates(t *testing.T) {
	home := homeFixture(t)
	v := newUpdateSettings(t, home)
	if text, tab := v.UpdateBadge(); text != "" || tab != 0 {
		t.Fatalf("unchecked view must show no badge, got (%q, %d)", text, tab)
	}
	v.HookMsg(UpdateCheckMsg{Latest: "v0.2.0"})
	if text, tab := v.UpdateBadge(); text != "update available: v0.2.0" || tab != subUpdate {
		t.Fatalf("badge = (%q, %d), want update-available badge to the Update menu", text, tab)
	}
	// Up-to-date via the compiled version only (no tab visited yet, no
	// VERSION file): the badge must resolve installed lazily, never claim
	// an update for an equal tag.
	v.Version = "v0.2.0"
	v.installed = ""
	if text, tab := v.UpdateBadge(); text != "" || tab != 0 {
		t.Fatalf("up-to-date view must show no badge, got (%q, %d)", text, tab)
	}
}

// ---- button, popup and invocation -----------------------------------------

func TestUpdateButtonPopupConfirmRunsInstallScript(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v := newUpdateSettingsWithRunner(t, home, run)
	script := stubFetchedScript(t, v)
	v.HookMsg(UpdateCheckMsg{Latest: "v0.2.0"})
	v.Update(shell.SubTabMsg{Index: subUpdate})
	key(v, "down") // sole focusable: the Update button
	if b := body(v); !strings.Contains(b, "Update") {
		t.Fatalf("Update button missing:\n%s", b)
	}
	key(v, "enter") // → popup (ADR-0021)
	if !v.ConfirmOpen() {
		t.Fatal("Update button did not open the confirmation popup")
	}
	b := body(v)
	for _, want := range []string{"Update oc-sandbox?", "install script"} {
		if !strings.Contains(b, want) {
			t.Errorf("popup missing %q:\n%s", want, b)
		}
	}
	key(v, "esc")
	if len(bashCalls(run)) != 0 {
		t.Fatalf("cancelled popup ran the update: %v", run.Calls)
	}
	key(v, "enter") // button again → popup
	key(v, "tab")   // popup: switch to the Confirm button
	key(v, "enter")
	calls := bashCalls(run)
	if len(calls) != 1 {
		t.Fatalf("confirmed popup did not run the install script (%d calls)", len(calls))
	}
	assertCall(t, calls[0], "bash", script, "--yes")
	if b := body(v); !strings.Contains(b, "restart oc-sandbox to apply") {
		t.Errorf("successful update must note the restart:\n%s", b)
	}
	// A VERSION file the (faked) install script left behind is picked up
	// when the tab is re-entered.
	writeFile(t, v.installRoot+"/VERSION", "v0.2.0")
	v.Update(shell.SubTabMsg{Index: subUninstall})
	v.Update(shell.SubTabMsg{Index: subUpdate})
	if v.installed != "v0.2.0" {
		t.Fatalf("installed = %q, want re-resolved v0.2.0", v.installed)
	}
}

func TestUpdateNonInteractiveYesOnly(t *testing.T) {
	// Non-interactive update = fetch the install script to a temp file,
	// then run it with --yes only: allowlist preservation is the install
	// script's default, so nothing else may ride along.
	home := homeFixture(t)
	run := fakeRunner()
	v := newUpdateSettingsWithRunner(t, home, run)
	script := stubFetchedScript(t, v)
	v.Update(shell.SubTabMsg{Index: subUpdate})
	key(v, "down")
	key(v, "enter")
	key(v, "tab")
	key(v, "enter")
	calls := bashCalls(run)
	if len(calls) != 1 {
		t.Fatalf("update invocation count = %d, want 1 (%v)", len(calls), run.Calls)
	}
	assertCall(t, calls[0], "bash", script, "--yes")
}

func TestUpdateFetchFailureShowsPersistentWarning(t *testing.T) {
	// The fetch (install script never ships in the install tree) failing
	// must leave a warning in the pane that survives later frames — the
	// toast alone vanishes with the next Update message.
	home := homeFixture(t)
	run := fakeRunner()
	v := newUpdateSettingsWithRunner(t, home, run)
	v.fetchInstallScript = func() (string, error) { return "", errors.New("no route to host") }
	v.Update(shell.SubTabMsg{Index: subUpdate})
	key(v, "down")
	key(v, "enter")
	key(v, "tab")
	key(v, "enter")
	if v.upErr == nil {
		t.Fatal("fetch failure did not record upErr")
	}
	v.Update(tea.QuitMsg{}) // later frame: toast cleared, warning must persist
	if len(bashCalls(run)) != 0 {
		t.Fatalf("failed fetch still ran a script: %v", run.Calls)
	}
	b := body(v)
	if !strings.Contains(b, "Update failed:") || !strings.Contains(b, "no route to host") {
		t.Errorf("pane must keep the failure warning:\n%s", b)
	}
}

func TestUpdateRunFailureShowsPersistentWarning(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	run.Script = func(name string, args []string) (string, error) {
		return "bash: boom: command not found", errors.New("exit status 127")
	}
	v := newUpdateSettingsWithRunner(t, home, run)
	stubFetchedScript(t, v)
	v.Update(shell.SubTabMsg{Index: subUpdate})
	key(v, "down")
	key(v, "enter")
	key(v, "tab")
	key(v, "enter")
	v.Update(tea.QuitMsg{}) // later frame: toast cleared, warning must persist
	if b := body(v); !strings.Contains(b, "Update failed: exit status 127") {
		t.Errorf("pane must keep the failure warning:\n%s", b)
	}
}

func TestUpdateSuccessClearsPreviousWarning(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v := newUpdateSettingsWithRunner(t, home, run)
	v.upErr = errors.New("exit status 1") // a previous failed attempt
	stubFetchedScript(t, v)
	v.Update(shell.SubTabMsg{Index: subUpdate})
	key(v, "down")
	key(v, "enter")
	key(v, "tab")
	key(v, "enter")
	if b := body(v); strings.Contains(b, "Update failed:") {
		t.Errorf("successful update must clear the old warning:\n%s", b)
	}
}

func TestUpdateThroughSharedQueue(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v := newUpdateSettingsWithRunner(t, home, run)
	mgr := taskmgr.New()
	v.Tasks = mgr
	stubFetchedScript(t, v)
	v.Update(shell.SubTabMsg{Index: subUpdate})
	key(v, "down")
	cmd := key(v, "enter") // → popup
	cmd = key(v, "tab")
	cmd = key(v, "enter")
	if !v.Busy() || !mgr.BusyKey("settings") {
		t.Fatal("update op not queued / not busy")
	}
	testutil.Drain(t, v, mgr, cmd)
	time.Sleep(50 * time.Millisecond)
	if v.Busy() || mgr.BusyKey("settings") {
		t.Fatal("still busy after completion")
	}
	if n := len(bashCalls(run)); n != 1 {
		t.Fatalf("update ran %d times, want 1 (%v)", n, run.Calls)
	}
	if !strings.Contains(v.toast, "restart oc-sandbox to apply") {
		t.Fatalf("continuation toast missing: %q", v.toast)
	}
}

// ---- helpers ----------------------------------------------------------------

// assertCall checks one runner invocation's name and joined args.
func assertCall(t *testing.T, c cmdrun.Call, name string, want ...string) {
	t.Helper()
	if c.Name != name {
		t.Fatalf("invoked %q, want %q", c.Name, name)
	}
	if strings.Join(c.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v, want %v", c.Args, want)
	}
}

// stubFetchedScript replaces the install-script fetch seam with one
// returning a real temp file (the path the runner must be invoked on),
// so tests neither need network nor the default raw-GitHub fetch.
func stubFetchedScript(t *testing.T, v *View) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\necho stub\n"), 0o700); err != nil {
		t.Fatalf("stub script: %v", err)
	}
	v.fetchInstallScript = func() (string, error) { return path, nil }
	return path
}

func newUpdateSettings(t *testing.T, home string) *View {
	t.Helper()
	v := newSettings(t, home, nil)
	v.Version = "v0.1.0"
	return v
}

func newUpdateSettingsWithRunner(t *testing.T, home string, run cmdrun.Runner) *View {
	t.Helper()
	v := newSettings(t, home, run)
	v.Version = "v0.1.0"
	return v
}
