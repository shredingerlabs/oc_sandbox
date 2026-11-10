package webserver

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// --- first-run setup interaction (#161, spec §11) test harness -------------

// errNoTerminalTest is the decisive no-window signal the headless
// fallback rides (§3).
var errNoTerminalTest = &setupSpawnError{}

type setupSpawnError struct{}

func (*setupSpawnError) Error() string { return "no terminal emulator available" }

// setupFixture seeds one workspace (cloned source) with the script paths
// and the seams the setup machine needs: a cmdrun.Fake for the
// start/clone/CBM invocations and a TerminalSpawn capture.
type setupFixture struct {
	s      *Server
	fake   *cmdrun.Fake
	home   string
	root   string
	mu     sync.Mutex
	spawn  func() string // the command the host terminal got ("" none)
	spawns int
}

func newSetupFixture(t *testing.T, headless bool) *setupFixture {
	t.Helper()
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	// cloned source with a repo URL — the plan gains the clone stage.
	if err := sandboxcfg.PatchString(home, sandboxcfg.Path(root), "workspace_source", "cloned"); err != nil {
		t.Fatalf("patch source: %v", err)
	}
	if err := sandboxcfg.PatchString(home, sandboxcfg.Path(root), "repo_url", "https://github.com/u/demo.git"); err != nil {
		t.Fatalf("patch repo_url: %v", err)
	}

	f := &setupFixture{home: home, root: root}
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && len(args) > 0 && args[0] == "exec" {
			// the in-container probe/clone/CBM bash: probe fails once
			// (nothing cloned yet), clone and CBM succeed.
			for _, a := range args {
				if strings.Contains(a, "remote.origin.url") {
					return "", errTestImageMissing // probe: not cloned
				}
			}
			return "in-container ok\n", nil
		}
		if name == "podman" && len(args) > 0 && args[0] == "ps" {
			return "", nil // nothing running yet
		}
		return "script ok\n", nil
	}}
	f.fake = fake

	s := New(Options{
		BasePort: 0,
		Home:     home,
		Deps: &Deps{
			Run:             fake,
			Registry:        &registry.Registry{Home: home},
			StartScript:     filepath.Join(home, ".oc-sandbox", "scripts", "start.sh"),
			ConfigureScript: filepath.Join(home, ".oc-sandbox", "scripts", "configure-workspace.sh"),
		},
		TerminalSpawn: func(command string) error {
			f.mu.Lock()
			f.spawns++
			f.spawn = func() string { return command } // recorded even headless
			f.mu.Unlock()
			if headless {
				return errNoTerminalTest
			}
			return nil
		},
	})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	f.s = s
	return f
}

// postSetup posts one of the setup verbs.
func postSetup(t *testing.T, s *Server, name, verb string) *http.Response {
	t.Helper()
	resp, err := http.PostForm(s.URL()+"workspaces/"+name+"/setup/"+verb, url.Values{})
	if err != nil {
		t.Fatalf("POST setup/%s: %v", verb, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// waitSpawn polls (bounded) until the terminal spawn fired.
func (f *setupFixture) waitSpawn(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		spawn := f.spawn
		f.mu.Unlock()
		if spawn != nil {
			return spawn()
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("terminal window never spawned")
	return ""
}

// waitFlags polls (bounded) until the sandbox config carries flag=true.
func waitFlag(t *testing.T, root, flag string, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
		if err == nil && cfgField(cfg, flag) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("sandbox flag %s never reached %v", flag, want)
}

func cfgField(cfg sandboxcfg.Config, flag string) bool {
	switch flag {
	case "setup_clone_complete":
		return cfg.SetupCloneComplete
	case "setup_cbm_complete":
		return cfg.SetupCBMComplete
	case "setup_skills_complete":
		return cfg.SetupSkillsComplete
	case "setup_complete":
		return cfg.SetupComplete
	}
	return false
}

// statusFileOf extracts the status-file path from the spawned wrapper
// command (`…; printf '%s' $? > <status-file>`).
func statusFileOf(command string) string {
	i := strings.LastIndex(command, "> ")
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(command[i+2:])
}

// cardOf slices one card out of the workspaces fragment.
func cardOf(t *testing.T, s *Server, name string) string {
	t.Helper()
	doc := body(t, get(t, s.URL()+"workspaces"))
	i := strings.Index(doc, `data-ws="`+name+`"`)
	if i < 0 {
		t.Fatalf("card %s missing:\n%s", name, doc)
	}
	end := strings.Index(doc[i:], "</article>")
	if end < 0 {
		t.Fatalf("card %s unterminated:\n%s", name, doc)
	}
	return doc[i : i+end]
}

// POST …/setup/retry (#161, spec §11): the machine runs start → clone →
// CBM as streamed task lines; when the skills stage arrives the host
// terminal window auto-opens running the exact podman-exec prompt
// wrapped with the status-file exit relay — no non-interactive variant.
func TestSetupRetryRunsMachineAndAutoOpensSkills(t *testing.T) {
	f := newSetupFixture(t, false)

	resp := postSetup(t, f.s, "alpha", "retry")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup/retry status = %d, want 200", resp.StatusCode)
	}

	cmd := f.waitSpawn(t)
	for _, want := range []string{
		"podman exec -it --user dev opencode-sandbox-",
		`opencode --prompt "run the skill /setup-matt-pocock-skills using the skill tool"`,
		"; printf '%s' $? > ",
	} {
		if !strings.Contains(cmd, want) {
			t.Fatalf("spawned command %q misses %q", cmd, want)
		}
	}

	// streamed stages: start.sh ran once; probe failed once, clone and
	// CBM succeeded (the fake podman exec answers).
	calls := f.fake.Snapshot()
	var bashStarts int
	for _, c := range calls {
		if c.Name == "bash" && len(c.Args) > 0 && strings.HasSuffix(c.Args[0], "start.sh") {
			bashStarts++
		}
	}
	if bashStarts != 1 {
		t.Fatalf("start.sh ran %d times, want 1", bashStarts)
	}

	// partial progress flags written; skills pending (no flags yet).
	waitFlag(t, f.root, "setup_clone_complete", true)
	waitFlag(t, f.root, "setup_cbm_complete", true)
	waitFlag(t, f.root, "setup_skills_complete", false)
	waitFlag(t, f.root, "setup_complete", false)

	// the skills slot renders on the card: status line, persistent chip
	// and the two actions.
	card := cardOf(t, f.s, "alpha")
	for _, want := range []string{
		`data-setup-slot`,
		`data-chip="setup"`,
		"interactive session open in a terminal window",
		`data-act="setup-copy"`,
		"Copy manual command",
		`data-act="setup-cancel"`,
		"Cancel setup",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("skills slot missing %q, card:\n%s", want, card)
		}
	}
}

// Exit relay: the webserver polls the status file and feeds the exit
// code into the unchanged setup machine — 0 → flag patch and setup
// completes; the slot disappears.
func TestSetupRelayExitZeroCompletes(t *testing.T) {
	f := newSetupFixture(t, false)
	if resp := postSetup(t, f.s, "alpha", "retry"); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup/retry status = %d", resp.StatusCode)
	}
	cmd := f.waitSpawn(t)

	if err := os.WriteFile(statusFileOf(cmd), []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFlag(t, f.root, "setup_skills_complete", true)
	waitFlag(t, f.root, "setup_complete", true)

	// slot gone — the workspace is set up.
	for tries := 0; tries < 50; tries++ {
		if !strings.Contains(cardOf(t, f.s, "alpha"), `data-setup-slot`) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("skills slot must disappear once the setup completed")
}

// Exit 2 is the explicit user abort (ADR 0011): no flags written,
// partial progress preserved, the recovery action reappears.
func TestSetupRelayExitTwoAbortsFlagFree(t *testing.T) {
	f := newSetupFixture(t, false)
	if resp := postSetup(t, f.s, "alpha", "retry"); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup/retry status = %d", resp.StatusCode)
	}
	cmd := f.waitSpawn(t)
	if err := os.WriteFile(statusFileOf(cmd), []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var card string
	for time.Now().Before(deadline) {
		card = cardOf(t, f.s, "alpha")
		if !strings.Contains(card, `data-setup-slot`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitFlag(t, f.root, "setup_skills_complete", false)
	waitFlag(t, f.root, "setup_complete", false)
	waitFlag(t, f.root, "setup_cbm_complete", true) // partial progress preserved
	if !strings.Contains(card, `data-act="setup-retry"`) {
		t.Fatalf("recovery action must reappear after abort, card:\n%s", card)
	}
}

// Any other exit opens granular recovery: no flags, retry reappears.
func TestSetupRelayExitOtherRecovers(t *testing.T) {
	f := newSetupFixture(t, false)
	if resp := postSetup(t, f.s, "alpha", "retry"); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup/retry status = %d", resp.StatusCode)
	}
	cmd := f.waitSpawn(t)
	if err := os.WriteFile(statusFileOf(cmd), []byte("70"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var card string
	for time.Now().Before(deadline) {
		card = cardOf(t, f.s, "alpha")
		if !strings.Contains(card, `data-setup-slot`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitFlag(t, f.root, "setup_skills_complete", false)
	waitFlag(t, f.root, "setup_complete", false)
	if !strings.Contains(card, `data-act="setup-retry"`) {
		t.Fatalf("recovery action must reappear after failure, card:\n%s", card)
	}
}

// Cancel setup marks a pending session abandoned: no flags, partial
// progress preserved, recovery actions reappear — and a status file
// arriving for the cancelled session is ignored (session guard).
func TestSetupCancelAbandonsAndIgnoresLateStatus(t *testing.T) {
	f := newSetupFixture(t, false)
	if resp := postSetup(t, f.s, "alpha", "retry"); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup/retry status = %d", resp.StatusCode)
	}
	cmd := f.waitSpawn(t)

	resp := postSetup(t, f.s, "alpha", "cancel")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup/cancel status = %d, want 200", resp.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Second)
	var card string
	for time.Now().Before(deadline) {
		card = cardOf(t, f.s, "alpha")
		if !strings.Contains(card, `data-setup-slot`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	waitFlag(t, f.root, "setup_skills_complete", false)
	waitFlag(t, f.root, "setup_complete", false)
	waitFlag(t, f.root, "setup_cbm_complete", true)
	if !strings.Contains(card, `data-act="setup-retry"`) {
		t.Fatalf("recovery action must reappear after cancel, card:\n%s", card)
	}

	// the late status file for the cancelled session is ignored.
	if err := os.WriteFile(statusFileOf(cmd), []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	waitFlag(t, f.root, "setup_skills_complete", false)
	waitFlag(t, f.root, "setup_complete", false)
}

// Headless fallback (§3): no window opens — the slot shows the exact
// copyable manual command, stays pending, and completion flows through
// the same relay.
func TestSetupHeadlessShowsManualCommand(t *testing.T) {
	f := newSetupFixture(t, true)
	if resp := postSetup(t, f.s, "alpha", "retry"); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup/retry status = %d", resp.StatusCode)
	}

	deadline := time.Now().Add(2 * time.Second)
	var card string
	for time.Now().Before(deadline) {
		card = cardOf(t, f.s, "alpha")
		if strings.Contains(card, `data-setup-slot`) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, want := range []string{
		`data-setup-slot`,
		`data-act="setup-copy"`,
		`opencode --prompt`,
		"printf",
		"/setup-matt-pocock-skills using the skill tool",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("headless slot missing %q, card:\n%s", want, card)
		}
	}

	// the manual command carries the same relay — completion flows
	// through the identical status file (raw command from the spawn
	// seam; the rendered copy is HTML-escaped but decodes to it).
	cmd := f.waitSpawn(t)
	if err := os.WriteFile(statusFileOf(cmd), []byte("0"), 0o644); err != nil {
		t.Fatal(err)
	}
	waitFlag(t, f.root, "setup_skills_complete", true)
	waitFlag(t, f.root, "setup_complete", true)
}

// While a setup session is pending it behaves like a busy op for the
// one-at-a-time-per-workspace rule: the workspace's other actions are
// disabled with a disabled-reason, and the action POSTs refuse.
func TestSetupPendingBlocksWorkspaceActions(t *testing.T) {
	f := newSetupFixture(t, false)
	if resp := postSetup(t, f.s, "alpha", "retry"); resp.StatusCode != http.StatusOK {
		t.Fatalf("setup/retry status = %d", resp.StatusCode)
	}
	f.waitSpawn(t)

	card := cardOf(t, f.s, "alpha")
	for _, marker := range []string{`data-act="open"`, `data-act="web"`, `data-confirm="stop"`, `data-confirm="delete"`} {
		i := strings.Index(card, marker)
		if i < 0 {
			t.Fatalf("card missing %s:\n%s", marker, card)
		}
		btn := card[i:]
		if len(btn) > 300 {
			btn = btn[:300]
		}
		if !strings.Contains(btn, "disabled") || !strings.Contains(btn, "first-run setup") {
			t.Fatalf("%s must be disabled with a setup reason:\n%s", marker, btn)
		}
	}

	// server-side: start and access refuse while the session holds.
	for _, route := range []string{"start", "access"} {
		var form url.Values
		if route == "access" {
			form = url.Values{"variant": {"console"}}
		}
		resp, err := http.PostForm(f.s.URL()+"workspaces/alpha/"+route, form)
		if err != nil {
			t.Fatal(err)
		}
		b := body(t, resp)
		if resp.StatusCode != http.StatusConflict || !strings.Contains(b, "first-run setup") {
			t.Fatalf("POST %s during pending setup = %d %q, want 409 with the setup reason", route, resp.StatusCode, b)
		}
	}
}
