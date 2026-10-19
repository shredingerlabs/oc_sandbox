package openproject

import (
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
