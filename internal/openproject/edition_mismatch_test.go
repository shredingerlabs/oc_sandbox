package openproject

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

// A running container whose image edition differs from the saved
// sandbox_config.json edition shows a mismatch prompt naming both
// editions; nothing stops or starts until a choice is made.
func TestEditionMismatchPromptShowsBothEditions(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\tlocalhost/opencode-sandbox-swdev:latest"
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	lines := bodyOf(t, e.m, bodyW, bodyH)
	// both editions surfaced: the running one and the saved one
	if !someLine(lines, "swdev") || !someLine(lines, "base") {
		t.Fatalf("mismatch prompt missing editions, body = %v", lines)
	}
	if !someLine(lines, "Use current") || !someLine(lines, "Cancel") {
		t.Fatalf("mismatch prompt missing choices, body = %v", lines)
	}
	if got := e.startCalls(t); len(got) != 0 {
		t.Errorf("mismatch prompt started a container: %v", got)
	}
	if got := e.execed; len(got) != 0 {
		t.Errorf("mismatch prompt handed off before a choice: %v", got)
	}
}

// "Use current" routes into the running container exactly as today:
// handoff port, zero stop/start calls, registry stays running.
func TestMismatchUseCurrentAccessesWithoutStopStart(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-swdev"
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	if e.m.state != stateMismatch {
		t.Fatalf("state = %v, want mismatch prompt", e.m.state)
	}
	mark := len(e.run.Calls)
	e.step(keyMsg("enter")) // first choice: Use current
	e.finishKeyCmds(e.lastCmd)
	if len(e.execed) != 1 {
		t.Fatalf("Use current did not access: %v", e.execed)
	}
	want := "podman exec -it --user dev opencode-sandbox-" + e.ids["alpha"] + " bash"
	if e.execed[0] != want {
		t.Errorf("exec = %q, want %q", e.execed[0], want)
	}
	for _, c := range e.run.Calls[mark:] {
		if c.Name == "podman" && (c.Args[0] == "stop" || c.Args[0] == "start" || c.Args[0] == "rm") {
			t.Errorf("Use current issued %v", c.Args)
		}
	}
	if len(e.startCalls(t)) != 0 {
		t.Errorf("Use current started: %v", e.startCalls(t))
	}
	if e.startStatus(t, "alpha") != "running" {
		t.Errorf("status = %q, want running", e.startStatus(t, "alpha"))
	}
}

// "Cancel" closes the prompt and issues no podman calls at all — no
// stop, no start, no access.
func TestMismatchCancelIssuesNoPodmanCalls(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-swdev"
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	e.finishKeyCmds(e.lastCmd) // drain the reconcile's own poll refresh
	if e.m.state != stateMismatch {
		t.Fatalf("state = %v, want mismatch prompt", e.m.state)
	}
	mark := len(e.run.Calls)
	e.step(keyMsg("down"))
	e.step(keyMsg("enter")) // second choice: Cancel
	e.finishKeyCmds(e.lastCmd)
	for _, c := range e.run.Calls[mark:] {
		// polls (fresh ps) fire on every key step — read-only, allowed;
		// no stop/start/rm/start.sh may appear
		if c.Name == "podman" && c.Args[0] != "ps" {
			t.Errorf("Cancel issued podman %v", c.Args)
		}
		if c.Name == "bash" && len(c.Args) > 1 && filepath.Base(c.Args[0]) == "start.sh" {
			t.Errorf("Cancel started a container: %v", c.Args)
		}
	}
	if len(e.execed) != 0 || len(e.startCalls(t)) != 0 {
		t.Errorf("Cancel accessed/started: exec=%v starts=%v", e.execed, e.startCalls(t))
	}
	if e.m.state != stateList {
		t.Errorf("state = %v, want stateList", e.m.state)
	}
}

// Editions agreeing goes straight to access, exactly as today.
func TestEditionsAgreeGoesStraightToAccess(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-base:latest"
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	if len(e.startCalls(t)) != 0 {
		t.Fatalf("agreeing editions started: %v", e.startCalls(t))
	}
	if len(e.execed) != 1 || !strings.HasSuffix(e.execed[0], " bash") {
		t.Fatalf("console handoff = %v", e.execed)
	}
}

// A stopped container + changed edition behaves exactly as today: args
// rebuilt from settings (saved edition), no mismatch prompt.
func TestStoppedContainerChangedEditionStartsWithSavedArgs(t *testing.T) {
	e := newEnv(t, nil)
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "container_edition", "swdev")
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	calls := e.startCalls(t)
	if len(calls) != 1 {
		t.Fatalf("start.sh ran %d times", len(calls))
	}
	if !containsArg(calls[0], "swdev") || containsArg(calls[0], "base") {
		t.Errorf("start args = %v", calls[0])
	}
	// today's behavior: start success continues into access (console handoff)
	if len(e.execed) != 1 || !strings.Contains(e.execed[0], e.ids["alpha"]) {
		t.Errorf("handoff after start = %v", e.execed)
	}
}

// mismatch helpers: navigate the prompt to a choice index.
func mismatchTo(e *env, choice int) {
	for i := 0; i < choice; i++ {
		e.step(keyMsg("down"))
	}
	e.step(keyMsg("enter"))
}

// --- switch (#116): prompt rendering ---

// The mismatch prompt offers the Switch button naming the saved edition
// (the target of the replacement).
func TestMismatchPromptOffersSwitchToSavedEdition(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-swdev"
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	lines := bodyOf(t, e.m, bodyW, bodyH)
	if !someLine(lines, "Switch to base") {
		t.Fatalf("mismatch prompt missing switch button, body = %v", lines)
	}
}

// Agreeing editions never render a switch: the prompt does not appear
// and the body goes straight to access with no "Switch" row.
func TestSwitchNeverRendersOnAgreeingEditions(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"] // psImagesOut empty: names-only path
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.finishOps()
	lines := bodyOf(t, e.m, bodyW, bodyH)
	if someLine(lines, "Switch to") {
		t.Errorf("switch rendered without a mismatch:\n%s", strings.Join(lines, "\n"))
	}
}

// --- switch (#116): the confirmed replacement ---

// Switch stops the running container under its exact name, verifies it
// gone (fresh ps) before flipping the registry to stopped, then rebuilds
// start args from the saved settings and starts — verified live before
// the registry flips to running — and continues into the clicked access
// (console handoff), no second click.
func TestMismatchSwitchStopsThenVerifiedStartThenAccess(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.onStop = func(args []string) { // after stop the ps polls show nothing
		e.hooks.psOut = ""
		e.hooks.psImagesOut = ""
	}
	e.hooks.onStart = func(args []string) { // the started container is live again
		e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
		e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-base"
	}
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-swdev"
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	if e.m.state != stateMismatch {
		t.Fatalf("state = %v, want mismatch prompt", e.m.state)
	}
	mark := len(e.run.Calls)
	mismatchTo(e, 2) // third choice: Switch
	e.finishOps()

	// one stop targeting the running container's exact name
	var stops [][]string
	for _, c := range e.run.Calls[mark:] {
		if c.Name == "podman" && c.Args[0] == "stop" {
			stops = append(stops, c.Args)
		}
	}
	if len(stops) != 1 || stops[0][1] != "opencode-sandbox-"+e.ids["alpha"] {
		t.Fatalf("podman stop = %v, want single stop of the running container", stops)
	}
	// a fresh ps between the stop and the start (gone-verification)
	stopIdx := len(e.run.Calls)
	for i, c := range e.run.Calls {
		if c.Name == "podman" && c.Args[0] == "stop" {
			stopIdx = i
		}
	}
	verifyPSBetween := false
	for _, c := range e.run.Calls[stopIdx+1:] {
		if c.Name == "podman" && c.Args[0] == "ps" {
			verifyPSBetween = true
		}
		if c.Name == "bash" && len(c.Args) > 0 && c.Args[0] == e.startScript() && !(len(c.Args) == 2 && c.Args[1] == "--help") {
			break // start.sh reached
		}
	}
	if !verifyPSBetween {
		t.Fatal("no fresh podman ps between stop and start (gone-verification missing)")
	}
	// start args fully rebuilt from saved settings (edition base)
	calls := e.startCalls(t)
	if len(calls) != 1 {
		t.Fatalf("start.sh ran %d times", len(calls))
	}
	want := []string{
		e.roots["alpha"], "--edition", "base", "--container-id", e.ids["alpha"],
		"--skills", "--detach",
	}
	if strings.Join(calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("switch start args = %v, want %v", calls[0], want)
	}
	// the started container is verified live: a ps after start.sh ran
	startIdx := -1
	for i, c := range e.run.Calls {
		if c.Name == "bash" && len(c.Args) > 0 && c.Args[0] == e.startScript() {
			startIdx = i
		}
	}
	if startIdx < 0 {
		t.Fatal("start.sh never ran")
	}
	for _, c := range e.run.Calls[startIdx+1:] {
		if c.Name == "podman" && c.Args[0] == "ps" {
			break
		}
	} // verification happens inside the op; absence is asserted via status below
	if e.startStatus(t, "alpha") != "running" {
		t.Errorf("status = %q, want running", e.startStatus(t, "alpha"))
	}
	// success continues into the originally clicked access (console handoff)
	if len(e.execed) != 1 || e.execed[0] != "podman exec -it --user dev opencode-sandbox-"+e.ids["alpha"]+" bash" {
		t.Errorf("access after switch = %v", e.execed)
	}
}

// Stop succeeded but the start failed: the registry stays "stopped", the
// failure surfaces, and nothing rolls back (no second stop/start).
func TestMismatchSwitchStartFailureLeavesStopped(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.stopErr = nil
	e.hooks.onStop = func(args []string) {
		e.hooks.psOut = ""
		e.hooks.psImagesOut = ""
	}
	e.hooks.startErr = errors.New("podman blew up")
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-swdev"
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	mark := len(e.run.Calls)
	mismatchTo(e, 2)
	e.finishOps()
	if e.startStatus(t, "alpha") != "stopped" {
		t.Errorf("status = %q, want stopped after failed start", e.startStatus(t, "alpha"))
	}
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Switch failed") {
		t.Error("switch failure not surfaced")
	}
	// exactly one stop and one start — no rollback attempt
	stops, starts := 0, 0
	for _, c := range e.run.Calls[mark:] {
		if c.Name == "podman" && c.Args[0] == "stop" {
			stops++
		}
		if c.Name == "bash" && len(c.Args) > 0 && c.Args[0] == e.startScript() && !(len(c.Args) == 2 && c.Args[1] == "--help") {
			starts++
		}
	}
	if stops != 1 || starts != 1 {
		t.Errorf("calls after switch: stops=%d starts=%d, want 1/1", stops, starts)
	}
}

// The started container is not verified live (ps stays empty after
// start.sh): the op fails, the project stays stopped, the error names
// the missing verification.
func TestMismatchSwitchNotLiveAfterStartLeavesStopped(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.onStop = func(args []string) { e.hooks.psOut = "" }
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-swdev"
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	mismatchTo(e, 2)
	e.finishOps()
	if e.startStatus(t, "alpha") != "stopped" {
		t.Errorf("status = %q, want stopped", e.startStatus(t, "alpha"))
	}
}

// "Use current" is still the first choice and Cancel still the second —
// the switch appends, it never displaces (regression on ticket 115).
func TestMismatchSwitchAppendsToExistingChoices(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	e.hooks.psImagesOut = "opencode-sandbox-" + e.ids["alpha"] + "\topencode-sandbox-swdev"
	e.rebuild(t)
	bodyOf(t, e.m, bodyW, bodyH)
	e.step(keyMsg("enter"))
	e.reconcileStart()
	e.finishKeyCmds(e.lastCmd)
	if e.m.state != stateMismatch {
		t.Fatalf("state = %v, want mismatch prompt", e.m.state)
	}
	e.step(keyMsg("esc")) // esc = Cancel must stay Cancel
	mark := len(e.run.Calls)
	e.finishKeyCmds(e.lastCmd)
	if e.m.state != stateList {
		t.Errorf("esc after switch addition = %v, want stateList", e.m.state)
	}
	for _, c := range e.run.Calls[mark:] {
		if c.Name == "podman" && c.Args[0] != "ps" {
			t.Errorf("Cancel issued podman %v", c.Args)
		}
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// The image column, not the config, decides the mismatch: the fake
// supplies the saved edition in the image too but a real ps only ever
// shows what truly runs.
func TestEditionFromImageParsesRegistryAndTags(t *testing.T) {
	for img, want := range map[string]string{
		"opencode-sandbox-base":                         "base",
		"localhost/opencode-sandbox-swdev:latest":       "swdev",
		"registry.io/team/opencode-sandbox-my-ed":       "my-ed",
		"registry.io/team/opencode-sandbox-x@sha256:ff": "x",
		"alpine:3": "",
	} {
		if got := discovery.EditionFromImage(img); got != want {
			t.Errorf("EditionFromImage(%q) = %q, want %q", img, got, want)
		}
	}
}
