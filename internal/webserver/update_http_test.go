package webserver

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Settings → Update card over HTTP (spec §9): GET /update renders
// the HTMX fragment from the updater state; POST /update/check re-runs
// the check and returns the fragment; POST /update/run starts the
// exclusive run and returns the fragment at once (the phase events ride
// /events). All under the token grammar the middleware enforces.

func postForm(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/x-www-form-urlencoded", strings.NewReader(""))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// GET /update renders the card: installed/latest rows, the two actions
// and the log pane slot.
func TestUpdateFragmentRendersCard(t *testing.T) {
	s := New(Options{BasePort: 0, Version: "v1.0.0", InstallRoot: t.TempDir()})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	doc := body(t, get(t, s.URL()+"update"))
	for _, want := range []string{
		"Installed version: v1.0.0",
		"Latest version: checking…",
		"data-role=\"update-check\"", // Check now
		"data-role=\"update-run\"",   // Update
		`data-role="update-log"`,     // embedded log pane slot
		`data-confirm-title="Update oc-sandbox?"`, // shared confirm popup
		"Kept: proxy/allowlist.txt",               // popup copy carries the keep-note (§12)
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("update card missing %q:\n%s", want, doc)
		}
	}
}

// The verdict rows render from the phase; a failed run keeps the error
// visible on the card until the next attempt.
func TestUpdateFragmentRendersPhases(t *testing.T) {
	s := New(Options{BasePort: 0, Version: "v1.0.0", InstallRoot: t.TempDir()})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	u := s.updater

	// available: the verdict row and an enabled Update button
	u.check = func() (string, error) { return "v2.0.0", nil }
	u.Check()
	doc := body(t, get(t, s.URL()+"update"))
	if !strings.Contains(doc, "Latest version: v2.0.0") || !strings.Contains(doc, "Update available!") {
		t.Fatalf("available card:\n%s", doc)
	}

	// installing: the Update button disables
	release := make(chan struct{})
	u.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	u.runScript = func(script string, args []string, emit func(string)) error {
		<-release
		return errSentinel // this first run fails
	}
	u.Run()
	doc = body(t, get(t, s.URL()+"update"))
	if !strings.Contains(doc, "Updating…") || !strings.Contains(doc, `data-role="update-run" disabled`) {
		t.Fatalf("installing card:\n%s", doc)
	}

	// failed: the reason persists on the card
	close(release)
	waitForUpdates(t, subscribe(s.hub), PhaseFailed)
	doc = body(t, get(t, s.URL()+"update"))
	if !strings.Contains(doc, "Update failed: "+errSentinel.Error()) {
		t.Fatalf("failed card:\n%s", doc)
	}

	// applied: the persistent restart note, no error
	u.runScript = func(script string, args []string, emit func(string)) error { return nil }
	u.Run()
	waitForUpdates(t, subscribe(s.hub), PhaseApplied)
	doc = body(t, get(t, s.URL()+"update"))
	if !strings.Contains(doc, "Update installed — restart oc-sandbox to apply.") {
		t.Fatalf("applied card missing the restart note:\n%s", doc)
	}
	if strings.Contains(doc, "Update failed:") {
		t.Fatalf("applied card still shows the stale error:\n%s", doc)
	}
}

// POST /update/check re-runs the check on demand and returns the fresh
// card.
func TestUpdateCheckNowEndpoint(t *testing.T) {
	s := New(Options{BasePort: 0, Version: "v1.0.0", InstallRoot: t.TempDir()})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	u := s.updater
	writeVersion(t, s.opts.InstallRoot, "v1.0.0")
	u.check = func() (string, error) { return "v2.0.0", nil }

	resp := postForm(t, s.URL()+"update/check")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	if !strings.Contains(doc, "Update available!") || !strings.Contains(doc, "v2.0.0") {
		t.Fatalf("check-now response did not re-run the check:\n%s", doc)
	}
}

// POST /update/run starts the exclusive run and answers at once — the
// phases stream via /events, never blocking the request.
func TestUpdateRunEndpointStartsAndAnswers(t *testing.T) {
	s := New(Options{BasePort: 0, Version: "v1.0.0", InstallRoot: t.TempDir()})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	u := s.updater
	u.check = func() (string, error) { return "v2.0.0", nil }
	u.Check()

	release := make(chan struct{})
	defer close(release)
	u.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	u.runScript = func(script string, args []string, emit func(string)) error {
		<-release
		return nil
	}
	done := make(chan struct{})
	go func() { defer close(done); postForm(t, s.URL()+"update/run") }()

	// The request must not wait for the script.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("POST /update/run blocked on the update op")
	}
	// The run is live (served fragment shows installing).
	doc := body(t, get(t, s.URL()+"update"))
	if !strings.Contains(doc, `data-role="update-run" disabled`) {
		t.Fatalf("card after run start:\n%s", doc)
	}
}

// Check now stays usable while the update runs: it answers 200 and does
// not flip the installing phase.
func TestUpdateCheckEndpointWhileInstalling(t *testing.T) {
	s := New(Options{BasePort: 0, Version: "v1.0.0", InstallRoot: t.TempDir()})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	u := s.updater
	u.check = func() (string, error) { return "v2.0.0", nil }
	u.Check()

	release := make(chan struct{})
	defer close(release)
	u.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	u.runScript = func(script string, args []string, emit func(string)) error {
		<-release
		return nil
	}
	u.Run()

	resp := postForm(t, s.URL()+"update/check")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check during installing: status = %d, want 200", resp.StatusCode)
	}
	if ev := u.snapshot(); ev.Phase != PhaseInstalling {
		t.Fatalf("phase after check during run = %q, want installing", ev.Phase)
	}
}

// POST /update/run while installing is a no-op that still answers 200
// with the current card (server-side enforcement of the disabled button).
func TestUpdateRunEndpointWhileInstalling(t *testing.T) {
	s := New(Options{BasePort: 0, Version: "v1.0.0", InstallRoot: t.TempDir()})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	u := s.updater
	u.check = func() (string, error) { return "v2.0.0", nil }
	u.Check()

	release := make(chan struct{})
	var started int
	u.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	u.runScript = func(script string, args []string, emit func(string)) error {
		started++
		<-release
		return nil
	}
	ch := subscribe(s.hub)
	u.Run()
	waitForUpdates(t, ch, PhaseInstalling)

	resp := postForm(t, s.URL()+"update/run")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	close(release)
	waitForUpdates(t, ch, PhaseApplied)
	if started != 1 {
		t.Fatalf("run script executed %d times, want exactly 1", started)
	}
}
