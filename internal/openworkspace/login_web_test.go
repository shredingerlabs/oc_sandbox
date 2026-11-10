package openworkspace

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// The pinned Login WEB button (above the Workspace Settings heading):
// armed by a fresh one-time pair link (ADR 0024) on a web access, it
// opens the browser on click and is removed once the pair token is
// used; a failed opener falls back to the bottom toast with the button
// kept, and a stopped/replaced container expires the stale link.

func loginEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t, &runnerHooks{pairOut: "  URL  http://127.0.0.1:4096/auth/connect/abc123\n"})
	sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots["alpha"]), "start_option", "web")
	sandboxcfg.PatchBool(e.home, sandboxcfg.Path(e.roots["alpha"]), "setup_complete", true) // pair runs on non-first-run access
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]                                    // the started container stays live for the running polls
	e.rebuild(t)
	e.step(keyMsg("enter")) // configured web is the preselected variant
	e.finishOps()
	bodyOf(t, e.m, bodyW, bodyH) // a render flushes layout + the pinned hit rects
	return e
}

// Button armed right above the Workspace Settings heading after the web
// access announced the fresh pair link.
func TestLoginWEBButtonArmsOnPairLink(t *testing.T) {
	e := loginEnv(t)
	if e.m.loginLink == "" || e.m.loginPath != e.roots["alpha"] {
		t.Fatalf("pair link did not arm the Login WEB state: link %q path %q", e.m.loginLink, e.m.loginPath)
	}
	if len(e.m.loginRects) != 1 || e.m.loginRects[0].W == 0 {
		t.Fatalf("pinned Login WEB hit rects missing: %v", e.m.loginRects)
	}
	lines := bodyOf(t, e.m, bodyW, bodyH)
	if !someLine(lines, "Login WEB") {
		t.Fatalf("the pinned button is not rendered:\n%s", strings.Join(lines, "\n"))
	}
	// pinned above the Workspace Settings heading: the button line comes first
	btnIdx, btnOK := findLine(lines, "Login WEB")
	headIdx, headOK := findLine(lines, "Workspace Settings — alpha")
	if !btnOK || !headOK || btnIdx >= headIdx {
		t.Errorf("button not pinned above the heading (btn %d, heading %d)", btnIdx, headIdx)
	}
}

// Clicking the pinned button opens the browser with the pair link and
// removes the button once the one-time token is used.
func TestLoginWEBClickOpensBrowserAndClears(t *testing.T) {
	e := loginEnv(t)
	rect := e.m.loginRects[0]
	e.step(clickAt(rect.X+rect.W/2, rect.Y))
	e.finishOps()
	if len(e.execed) != 1 {
		t.Fatalf("the button did not open the browser: %v", e.execed)
	}
	got := e.execed[0]
	if !strings.Contains(got, "http://127.0.0.1:4096/auth/connect/abc123") {
		t.Errorf("browser opener did not get the pair link: %q", got)
	}
	if e.m.loginLink != "" || len(e.m.loginRects) != 0 {
		t.Fatalf("the used button did not clear: link %q rects %v", e.m.loginLink, e.m.loginRects)
	}
	if someLine(bodyOf(t, e.m, bodyW, bodyH), "Login WEB") {
		t.Error("the consumed button is still rendered")
	}
}

// A failed opener (headless machine) keeps the button and relies on the
// bottom toast as the manual-copy backup.
func TestLoginWEBClickFailureKeepsButtonWithToast(t *testing.T) {
	e := loginEnv(t)
	e.loginRunErr = errors.New("no browser")
	rect := e.m.loginRects[0]
	e.step(clickAt(rect.X+rect.W/2, rect.Y))
	e.finishOps()
	if e.m.toast == "" || !strings.Contains(e.m.toast, "Could not open a browser") || !strings.Contains(e.m.toast, "http://127.0.0.1:4096") {
		t.Errorf("failed open missing the copy-fallback toast with the link: %q", e.m.toast)
	}
	if e.m.loginLink == "" || !someLine(bodyOf(t, e.m, bodyW, bodyH), "Login WEB") {
		t.Error("the button was dropped on a failed open")
	}
	// the combined toast stays inside the 3-row pinned block; the pair
	// link must survive the wrap at narrow widths (toast max rows)
	for _, w := range []int{80, 90, bodyW} {
		rows := strings.Split(e.m.Body(w, bodyH), "\n")
		var visible strings.Builder
		lw := leftWidth(w)
		for _, l := range rows[len(rows)-3:] { // the toast block, pinned to the (left) pane bottom
			visible.WriteString(strings.TrimRight(l[:min(lw, len(l))], " "))
		}
		if got := visible.String(); !strings.Contains(got, "http://127.0.0.1:4096/auth/connect/abc123") {
			t.Errorf("width %d: link clipped from the fallback toast: %q", w, got)
		}
	}
}

// The stale pair token expires when its container stops: the fresh
// podman-ps cache misses it and the pinned button disappears.
func TestLoginWEBExpiresWhenContainerStops(t *testing.T) {
	e := newEnv(t, nil)
	e.hooks.pairOut = "  URL  http://127.0.0.1:4096/pair?token=AbCdEfGh\n"
	e.rebuild(t)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	lines := bodyOf(t, e.m, bodyW, bodyH)
	var rect shell.Rect
	for _, r := range e.m.CardRects() {
		if strings.Contains(paneRowRowAt(lines, r, 1), "alpha") {
			rect = r
		}
	}
	e.step(clickAt(rect.X+19+2, rect.Y+4)) // Web button — alpha's container runs
	e.finishOps()
	if e.m.loginLink == "" {
		t.Fatal("pair link did not arm on a running container's web access")
	}
	e.hooks.psOut = "" // the container is gone on the next fresh ps
	e.step(tea.WindowSizeMsg{Width: bodyW, Height: bodyH})
	e.finishOps()
	if e.m.loginLink != "" || len(e.m.loginRects) != 0 {
		t.Fatalf("stale login state survived the container stop: link %q rects %v", e.m.loginLink, e.m.loginRects)
	}
}

// The pinned button binds to its owning workspace: selecting another card
// hides it, selecting the owner again shows it (the link is still
// fresh); no button exists without a web session link at all.
func TestLoginWEBOtherProjectDoesNotShowIt(t *testing.T) {
	e := loginEnv(t)
	e.step(keyMsg("down")) // select beta
	if someLine(bodyOf(t, e.m, bodyW, bodyH), "Login WEB") {
		t.Error("another workspace's pinned button leaked into its pane")
	}
	e.step(keyMsg("up")) // back to alpha
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "Login WEB") {
		t.Error("the still-fresh link did not re-render on its owner's pane")
	}
}

// The pinned block never scrolls away: pane scrolling at tight heights
// keeps the button at the top while the settings content moves.
func TestLoginWEBPinnedAcrossScroll(t *testing.T) {
	e := loginEnv(t)
	const w, h = 100, 12
	e.m.Body(w, h)
	e.step(tea.MouseMsg{Button: tea.MouseButtonWheelDown})
	lines := bodyOf(t, e.m, w, h)
	head := strings.Join(lines[:4], "\n")
	if !strings.Contains(head, "Login WEB") {
		t.Errorf("the pinned button scrolled away:\n%s", head)
	}
	for _, l := range lines[3:] {
		if strings.Contains(l, "Login WEB") {
			t.Error("a second (scrolled) copy of the button rendered")
		}
	}
}

// A web access without a pair link (expired/refused pairing) arms
// nothing — the URL toast stays the only announcement.
func TestLoginWEBNoPairLinkNoButton(t *testing.T) {
	e := newEnv(t, nil) // pairOut unset → pair fails
	e.rebuild(t)
	e.hooks.psOut = "opencode-sandbox-" + e.ids["alpha"]
	lines := bodyOf(t, e.m, bodyW, bodyH)
	var rect shell.Rect
	for _, r := range e.m.CardRects() {
		if strings.Contains(paneRowRowAt(lines, r, 1), "alpha") {
			rect = r
		}
	}
	e.step(clickAt(rect.X+19+2, rect.Y+4)) // Web button — URL announce, no token
	e.finishOps()
	if !someLine(bodyOf(t, e.m, bodyW, bodyH), "http://") {
		t.Error("URL not announced")
	}
	if e.m.loginLink != "" || someLine(bodyOf(t, e.m, bodyW, bodyH), "Login WEB") {
		t.Fatalf("arm happened without a pair link: link %q", e.m.loginLink)
	}
}
