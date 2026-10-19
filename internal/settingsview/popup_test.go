package settingsview

import (
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Confirmation popup content (ADR-0021): the restore and uninstall popups
// show the action's one-line title plus a context line; esc/q cancels
// without running anything.

func TestRestorePopupContentAndCancel(t *testing.T) {
	home := homeFixture(t)
	pp := config.Paths{Home: home}.ProjectsPath()
	const before = "{\n    \"projects\": []\n  }"
	writeFile(t, pp, before)
	const backup1 = "{\n  \"projects\": [\n    { \"restored\": true }\n  ]\n}"
	writeBackup(t, home, "projects.json.20260101T000000000.aaa111", backup1)

	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: subRestore})
	key(v, "down") // focus the Restore button → popup (ADR-0021)
	key(v, "enter")

	if !v.ConfirmOpen() {
		t.Fatal("Restore button did not open the popup")
	}
	b := body(v)
	for _, want := range []string{"Restore this config backup?", "projects.json.20260101T000000000.aaa111", "→ projects.json", "safety backup"} {
		if !strings.Contains(b, want) {
			t.Errorf("popup missing %q:\n%s", want, b)
		}
	}
	key(v, "esc") // cancel
	if v.ConfirmOpen() {
		t.Fatal("esc did not cancel the popup")
	}
	key(v, "tab")
	key(v, "enter") // back on the Restore button would run — gone
	if got := readFile(t, pp); got != before {
		t.Errorf("cancelled popup ran the restore: %q", got)
	}
}

func TestUninstallPopupContentCancelAndConfirm(t *testing.T) {
	home := homeFixture(t)
	run := fakeRunner()
	v := newSettings(t, home, run)
	v.Update(shell.SubTabMsg{Index: subUninstall})
	key(v, " ") // option 0 on for the echo
	key(v, "down")
	key(v, "down")
	key(v, "down")
	typeText(v, "uninstall")
	key(v, "enter") // confirm field → button
	key(v, "enter") // → popup (ADR-0021)

	if !v.ConfirmOpen() {
		t.Fatal("Uninstall button did not open the popup after the typed confirm")
	}
	b := body(v)
	for _, want := range []string{
		"Uninstall oc-sandbox?",
		"This removes the oc-sandbox installation.",
		"Remove config: yes",
		"Remove symlinks",
		"Remove desktop shortcuts",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("popup missing %q:\n%s", want, b)
		}
	}
	key(v, "esc")
	if len(bashCalls(run)) != 0 {
		t.Errorf("cancelled popup ran uninstall: %v", run.Calls)
	}
	key(v, "enter") // button again → popup
	key(v, "tab")   // switch to the red Uninstall confirm
	key(v, "enter")
	if n := len(bashCalls(run)); n != 1 {
		t.Fatalf("confirmed popup did not run uninstall (%d calls)", n)
	}
}

func TestRestorePopupConfirmRunsRestore(t *testing.T) {
	home := homeFixture(t)
	pp := config.Paths{Home: home}.ProjectsPath()
	const before = "{\n    \"projects\": []\n  }"
	writeFile(t, pp, before)
	writeBackup(t, home, "projects.json.20260101T000000000.aaa111", "{\n  \"projects\": []\n}")
	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: subRestore})
	key(v, "down") // Restore button → popup
	key(v, "enter")
	key(v, "tab")
	_ = key(v, "enter")

	if got := readFile(t, pp); string(got) == before {
		t.Errorf("confirmed popup did not restore")
	}
}
