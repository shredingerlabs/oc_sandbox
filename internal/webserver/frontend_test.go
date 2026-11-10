package webserver

import (
	"strings"
	"testing"
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
		`--ed`,      // edition hue tinting
		`.chip`,     // HIL + advanced chips
		`.dot`,      // running marker
		`.oplog`,    // streamed op log
		`.busy`,     // busy row
		`.confirm`,  // stop/delete confirm row
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("main.css missing %q", want)
		}
	}
}

// The card fragment carries the op id on the busy card so the streamed
// line events land in the right card's log.
func TestCardFragmentCarriesOpID(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	var bashCalls []string
	fake := plainStartFake(&bashCalls)
	s := startServerWith(t, home, fake)
	defer func() { _ = s.Close() }()

	resp := post(t, s.URL()+"workspaces/alpha/start", nil)
	if frag := body(t, resp); !strings.Contains(frag, `data-op-id="`) {
		t.Fatalf("busy card missing data-op-id, got:\n%s", frag)
	}
}
