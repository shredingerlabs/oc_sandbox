package webserver

import (
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// The frontend wiring of the Open screen (#153): the shell script loads
// the card grid via HTMX, re-renders reactively from the SSE workspace
// events (no polling), streams op lines into the busy card, opens the
// pair link in a new tab under the click gesture, and renders the
// Stop/Delete confirm rows.
func TestFrontendOpenScreenWiring(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`base + "workspaces"`, // ws-open loads the card grid fragment
		`"workspace"`,         // SSE re-render trigger
		`displaychange`,       // mode switch re-renders the cards
		`data-op-id`,          // line events match the busy card by op id
		`pair-link`,           // fresh one-time link per access
		`window.open`,         // new tab under the click gesture
		`workspaces/`,         // POST start + access targets
		`"access"`,            // console/OpenCode variants
		`data-confirm`,        // Stop/Delete confirm rows
		`clipboard`,           // copyable command fallback
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q", want)
		}
	}
}

// The card grid stylesheet carries the Signal card language (#153, §5).
func TestFrontendCardStyles(t *testing.T) {
	s := startTestServer(t)
	css := body(t, get(t, s.URL()+"css/main.css"))
	for _, want := range []string{
		`.grid`,
		`.card`,
		`.tile`,
		`--ed`,                      // edition hue tinting
		`.chip`,                     // HIL + advanced chips
		`.dot`,                      // running marker
		`.oplog`,                    // streamed op log
		`.busy`,                     // busy row
		`.confirm`,                  // stop/delete confirm row
		`.confirm-backdrop[hidden]`, // display:flex must not beat the hidden attribute
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("main.css missing %q", want)
		}
	}
}

// The frontend wiring of the workspace ops (ADR-0033 cards): the
// restart confirm row posts the real restart op, the start chain
// carries the variant and handles the mismatch verdict, and the
// web-native adopt confirm gates both running-container replacements.
func TestFrontendWorkspaceOpsWiring(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`runWorkspaceOp(card, "restart")`, // the Restart confirm row's op (ADR-0033)
		`post(card, "adopt")`,             // the adopt confirm's action
		`mismatch`,                        // the #115 verdict handling in the start chain
		`running_edition`,                 // the mismatch prompt names what runs
		`an agent may be mid-task`,        // the adopt confirm copy
		`variant=`,                        // the requested access rides the start POST
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q", want)
		}
	}
}

// The §12 crash path: a hard crash pushes no shutdown — the SSE error
// handler starts a bounded reconnect watch that renders the same
// server-stopped screen the shutdown push shows once exhausted, and a
// successful open (reconnect) clears the watch.
func TestFrontendServerStoppedCrashPath(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`addEventListener("error"`, // EventSource's reconnect-failure signal
		`showServerStopped`,        // one screen for both causes (§12)
		`setTimeout`,               // bounded reconnect watch
		`clearTimeout`,             // a successful open clears the watch
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q (§12 crash path)", want)
		}
	}
	if strings.Count(js, "showServerStopped") < 2 {
		t.Fatal("showServerStopped must be shared by the shutdown push and the crash path")
	}
}

// The SSE hub's `es` is local to connect() — a stray es.* reference
// outside it throws a ReferenceError at load time, killing the whole
// IIFE (no SSE, no popup dismissal). Guard: every es.addEventListener
// call sits inside the connect() body.
func TestFrontendSSEListenersStayInsideConnect(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	start := strings.Index(js, "function connect()")
	if start < 0 {
		t.Fatal("main.js has no connect() hub")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("connect() body not delimited")
	}
	connectBody := js[start : start+end]
	if strings.Contains(js[:start], "es.addEventListener") ||
		strings.Contains(js[start+end:], "es.addEventListener") {
		t.Fatal("stray es.addEventListener outside connect() — ReferenceError at load")
	}
	if strings.Count(connectBody, "es.addEventListener") != 6 {
		t.Fatalf("connect() registers %d es.addEventListener calls, want 6 (line, workspace, update, shutdown, open, error)", strings.Count(connectBody, "es.addEventListener"))
	}
}

// The busy card carries the op id so the streamed line events land in
// the right card's log — across re-renders too (GET re-renders the op
// id from the queue), and confirms refuse while an op is
// queued/running (§9).
func TestCardFragmentCarriesOpID(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	release := make(chan struct{})
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "bash" {
			<-release
		}
		return "", nil
	}}
	s := startServerWith(t, home, fake)

	postMap(t, s.URL()+"workspaces/alpha/start", nil)
	waitForBusyCard(t, s, "alpha")

	// A GET render (SSE workspace event path) carries the op id, and
	// the restart confirm refuses while the op is queued/running.
	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, `data-op-id="`) {
		t.Fatal("busy card lost its op id")
	}
	if !strings.Contains(doc, `data-confirm="restart" disabled`) {
		t.Fatal("confirms must refuse while an op is queued/running")
	}
	close(release)
}

// waitForBusyCard polls (bounded) until the workspace renders busy —
// the op runs on a server goroutine.
func waitForBusyCard(t *testing.T, s *Server, name string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(body(t, get(t, s.URL()+"workspaces")), `data-busy`) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("card never rendered busy")
}
