package webserver

import (
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// uninstallServer starts a Server whose uninstall.sh calls land on a
// recorded Fake runner (Options.Runner seam).
func uninstallServer(t *testing.T) (*Server, *cmdrun.Fake) {
	t.Helper()
	home := t.TempDir()
	fake := &cmdrun.Fake{}
	s := New(Options{
		BasePort:    0,
		Home:        home,
		InstallRoot: filepath.Join(home, ".oc-sandbox"),
		Deps:        &Deps{Run: fake},
	})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, fake
}

// bashCalls filters the recorded calls to the uninstall script
// invocations (the screen renders also query the runner for running
// containers).
func bashCalls(fake *cmdrun.Fake) []cmdrun.Call {
	var out []cmdrun.Call
	for _, c := range fake.Calls {
		if c.Name == "bash" {
			out = append(out, c)
		}
	}
	return out
}

// waitIdle waits for the server's op queue to drain (the uninstall op
// runs on a goroutine — tests poll instead of racing it).
func waitIdle(t *testing.T, s *Server) {
	t.Helper()
	for i := 0; i < 500 && s.busy(); i++ {
		sleepMillis(2)
	}
}

func sleepMillis(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

// GET /uninstall renders the uninstall screen: warning, the three option
// toggles, the typed confirmation input and the popup-confirmed button.
func TestUninstallScreen(t *testing.T) {
	s, _ := uninstallServer(t)
	resp := get(t, s.URL()+"uninstall")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	for _, want := range []string{
		"Uninstall oc-sandbox",
		"Workspace roots are never touched",
		`name="remove_config"`,
		`name="remove_symlinks"`,
		`name="remove_shortcuts"`,
		`name="confirm_text"`,
		`data-confirm-popup`,
		`hx-post="uninstall"`,
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("uninstall screen missing %q:\n%s", want, doc)
		}
	}
}

// POST /uninstall without the typed `uninstall` confirmation refuses and
// invokes nothing (the TUI's typed confirm, server-enforced).
func TestUninstallRequiresTypedConfirm(t *testing.T) {
	s, fake := uninstallServer(t)
	for _, confirm := range []string{"", "yes", "UNINSTALL  ", " uninstall "} {
		// case-insensitive trimmed match is armed; whitespace-only and
		// wrong text are not ("UNINSTALL  " trimmed IS armed — see map)
		_, doc := postFormDoc(t, s.URL()+"uninstall", url.Values{
			"confirm_text": {confirm},
		})
		armed := strings.EqualFold(strings.TrimSpace(confirm), "uninstall")
		if armed && !strings.Contains(doc, "Uninstall started") {
			t.Fatalf("armed confirm %q refused:\n%s", confirm, doc)
		}
		if !armed && (strings.Contains(doc, "Uninstall started") || len(fake.Calls) != 0) {
			t.Fatalf("unarmed confirm %q not refused:\n%s", confirm, doc)
		}
	}
}

// POST /uninstall maps the wizard toggles onto uninstall.sh flags exactly
// like the TUI does and always runs with --force: !remove_symlinks →
// --no-symlinks, remove_shortcuts → --remove-shortcuts, remove_config →
// --remove-config. The script is invoked as the TUI invokes it
// (bash <script> <flags>) through the runner seam.
func TestUninstallFlagMapping(t *testing.T) {
	s, fake := uninstallServer(t)
	for _, c := range []struct {
		name string
		vals url.Values
		want []string
	}{
		{"defaults (keep symlinks)", url.Values{"confirm_text": {"uninstall"}},
			[]string{"--force", "--no-symlinks"}},
		{"keep symlinks, drop shortcuts+config", url.Values{
			"confirm_text": {"uninstall"}, "remove_symlinks": {"1"},
			"remove_shortcuts": {"1"}, "remove_config": {"1"}},
			[]string{"--force", "--remove-shortcuts", "--remove-config"}},
		{"symlinks kept is the flag-off default", url.Values{
			"confirm_text": {"uninstall"}, "remove_symlinks": {"on"}},
			[]string{"--force"}},
	} {
		fake.Calls = nil
		_, doc := postFormDoc(t, s.URL()+"uninstall", c.vals)
		if !strings.Contains(doc, "Uninstall started") {
			t.Fatalf("%s: start notice missing:\n%s", c.name, doc)
		}
		waitIdle(t, s)
		bash := bashCalls(fake)
		if len(bash) != 1 {
			t.Fatalf("%s: script not invoked exactly once: %+v", c.name, fake.Calls)
		}
		call := bash[0]
		if call.Name != "bash" || len(call.Args) == 0 || call.Args[0] != s.uninstallScript() {
			t.Fatalf("%s: invocation = %v %v, want bash <uninstall.sh>", c.name, call.Name, call.Args)
		}
		if got := call.Args[1:]; strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Fatalf("%s: flags = %v, want %v", c.name, got, c.want)
		}
	}
}

// Uninstall refuses while an op is queued/running, before invoking the
// script.
func TestUninstallRefusesWhileBusy(t *testing.T) {
	s, fake := uninstallServer(t)
	release := make(chan struct{})
	defer close(release)
	s.queue.Launch(blockedOp(release))
	for !s.busy() {
	}
	_, doc := postFormDoc(t, s.URL()+"uninstall", url.Values{"confirm_text": {"uninstall"}})
	if !strings.Contains(doc, "queued or running") {
		t.Fatalf("busy refusal missing:\n%s", doc)
	}
	if len(bashCalls(fake)) != 0 {
		t.Fatalf("script invoked despite busy refusal")
	}
}
