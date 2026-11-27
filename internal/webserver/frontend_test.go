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
		`stt-link`,            // mic button resolves the live sidecar port
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

// The dictation mic button's wiring: the tab opens under the click
// gesture (popup blockers, the pair-link pattern), navigates once the
// stt-link resolution lands, and the failure paths fall back to a card
// note.
func TestFrontendSttMicWiring(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`btn.dataset.act === "stt"`,
		`post(card, "stt-link")`,
		`openSidecarTab`,                   // tab under the click gesture
		`dictation sidecar failed — retry`, // fetch-failure note copy
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q (mic button)", want)
		}
	}
}

// The wizard's toolchain-set wiring (toolchain sets #205, spec §9 WP-3):
// a bundle chip overwrites the units multi-select, editing the
// multi-select flips the bundle row to Custom (never auto-back), the pkm
// pin hides the rows and restores the prior selection on un-pin (the
// hard-code-back dies), and the HIL checkbox presets from the §3
// hardware units — a preset, never a pin. The old edition rule
// (applyEditionPreset) is deleted.
func TestFrontendWizardToolchainWiring(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`data-role="wizard-bundle"`, // the bundle chip row handler
		`setUnits`,                  // a bundle pick overwrites the multi-select
		`data-role="wizard-units"`,  // the multi-select reads back
		`"custom"`,                  // the Custom flip
		`applyHILPreset`,            // the HIL preset rule
		`"arm-embedded"`,            // the §3 hardware unit list
		`"picoscope"`, `"pyvisa"`,   // …its members
		`wizardPriorUnits`,        // the pkm un-pin restores the prior selection
		`data-role="units-count"`, // the "n of m selected" header updates
		`syncBundleRow`,           // draft replay re-derives the chips
		`syncUnitsCount`,          // …and the header
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q (toolchain wiring)", want)
		}
	}
	for _, gone := range []string{"applyEditionPreset", `setEdition(q(form, '[data-role="wizard-edition"]'), "swdev")`} {
		if strings.Contains(js, gone) {
			t.Fatalf("main.js still carries the deleted edition rule %q", gone)
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
		// The card icon grid: 3×2 fixed slots anchored top-right, the
		// star's slot pinned top-right (an absent mic never shifts it).
		`.icongrid`,
		`.card .icongrid .btn.icon[data-act="favorite"] { grid-area:1 / 3`,
		`.card .icongrid .btn.icon[data-act="stt"] { grid-area:1 / 2`,
		// The card star's two states (#174): outline default, the
		// filled/accent treatment carried by the stored flag marker.
		`.toprow .btn.icon[data-favorite="true"]`,
		`.toprow .btn.icon[data-favorite="true"] .ic { fill:currentColor`,
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("main.css missing %q", want)
		}
	}
}

// The card star's click delegation (#175): the favorite POST rides the
// card-action family's post/render path — one click flips the flag, the
// workspace SSE event re-renders the sections (no confirmation, no
// toast).
func TestFrontendFavoriteToggleWiring(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`btn.dataset.act === "favorite"`,   // the star's click branch
		`runWorkspaceOp(card, "favorite")`, // the action family's post/render path
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q (favorite toggle)", want)
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
		`btn.dataset.confirm === "stop"`,  // the Stop button's click branch
		`Stop this workspace?`,            // the stop confirm row's question
		`runWorkspaceOp(card, "stop")`,    // the stop confirm row's op (ADR-0033 update)
		`post(card, "adopt")`,             // the adopt confirm's action
		`mismatch`,                        // the #115 verdict handling in the start chain
		`v.running`,                       // the mismatch prompt names what runs (WP-6 re-key)
		`v.body`,                          // the prompt body is the member delta (WP-6)
		`an agent may be mid-task`,        // the adopt confirm copy
		`variant=`,                        // the requested access rides the start POST
		`v.build`,                         // the missing-set sidecar (WP-6)
		`build=1`,                         // Build now re-POSTs with the build flag
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

// screenDirty guards the SSE-triggered re-render: while the loaded
// screen holds user-modified form values the swap is skipped (the
// 769c146 paste-path fix). HTMLSelectElement has no defaultValue
// property, so the generic `el.value !== el.defaultValue` comparison
// made every select read as modified — the re-render was permanently
// skipped and a stopped container's login warning (disabled button +
// hint) never re-rendered in. Guard: screenDirty compares selects
// against their HTML default-selected option, not defaultValue.
func TestFrontendScreenDirtyHandlesSelects(t *testing.T) {
	s := startTestServer(t)
	js := body(t, get(t, s.URL()+"js/main.js"))
	start := strings.Index(js, "function screenDirty()")
	if start < 0 {
		t.Fatal("main.js has no screenDirty() guard")
	}
	end := strings.Index(js[start:], "\n  }")
	if end < 0 {
		t.Fatal("screenDirty() body not delimited")
	}
	body := js[start : start+end]
	if !strings.Contains(body, `el.tagName === "SELECT"`) {
		t.Fatal("screenDirty() must branch on SELECT before the defaultValue comparison")
	}
	if !strings.Contains(body, "option[selected]") {
		t.Fatal("selects must compare against the default-selected option (HTMLSelectElement has no defaultValue)")
	}
	if strings.Contains(body, "el.defaultValue") && strings.Index(body, `el.tagName === "SELECT"`) > strings.Index(body, "el.defaultValue") {
		t.Fatal("the defaultValue comparison must not run for selects — every select reads as dirty and the SSE re-render never lands")
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

	// A GET render (SSE workspace event path) carries the op id; the
	// restart stays accessible while the op is queued/running
	// (ADR-0037 — the server's busy refusal is the gate).
	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, `data-op-id="`) {
		t.Fatal("busy card lost its op id")
	}
	if !strings.Contains(doc, `data-confirm="restart" title="Restart this workspace"`) {
		t.Fatal("restart must stay accessible while an op is queued/running")
	}
	if strings.Contains(doc, `data-confirm="restart" disabled`) {
		t.Fatal("restart must not disable while an op is queued/running")
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
