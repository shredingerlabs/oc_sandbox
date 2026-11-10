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
		`--ed`,     // edition hue tinting
		`.chip`,    // HIL + advanced chips
		`.dot`,     // running marker
		`.oplog`,   // streamed op log
		`.busy`,    // busy row
		`.confirm`, // stop/delete confirm row
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("main.css missing %q", want)
		}
	}
}

// The frontend wiring of the workspace ops (#154): the confirm rows
// post the real stop/delete ops, the Open chain carries the variant and
// handles the mismatch verdict, and the web-native adopt confirm gates
// both running-container replacements.
func TestFrontendWorkspaceOpsWiring(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`runWorkspaceOp(card, "stop")`,   // the Stop confirm row's op
		`runWorkspaceOp(card, "delete")`, // the Delete confirm row's op (danger confirm stays)
		`post(card, "adopt")`,            // the adopt confirm's action
		`mismatch`,                       // the #115 verdict handling in the Open chain
		`running_edition`,                // the mismatch prompt names what runs
		`an agent may be mid-task`,       // the adopt confirm copy
		`variant=`,                       // the requested access rides the start POST
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q", want)
		}
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
	// the confirm buttons refuse while the op runs.
	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, `data-op-id="`) {
		t.Fatal("busy card lost its op id")
	}
	if !strings.Contains(doc, `data-confirm="stop" disabled`) {
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
