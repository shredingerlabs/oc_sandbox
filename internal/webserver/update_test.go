package webserver

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The §12 update state machine: the check (server start / Check now)
// publishes the phase via the one SSE `update` event; the run is the
// exclusive op whose applied phase re-reads the rewritten VERSION file.
// GitHub/transport seams are injected — hermetic, no network.

var errSentinel = errors.New("install script reported an error")

func newTestUpdater(t *testing.T) (*Server, *Updater) {
	t.Helper()
	s := New(Options{BasePort: 0, Version: "v1.0.0", InstallRoot: t.TempDir()})
	return s, s.updater
}

// writeVersion writes the install's VERSION file — install.sh's
// `printf '%s\n' "$DOWNLOADED_VERSION" > "$INSTALL_PATH/VERSION"`.
func writeVersion(t *testing.T, installRoot, tag string) {
	t.Helper()
	if err := os.MkdirAll(installRoot, 0o755); err != nil {
		t.Fatalf("mkdir install root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(installRoot, "VERSION"), []byte(tag+"\n"), 0o644); err != nil {
		t.Fatalf("write VERSION: %v", err)
	}
}

// subscribe returns a channel receiving every hub event from now on
// (in-package seam on the hub's fan-out).
func subscribe(hub *Hub) chan event {
	ch := make(chan event, 64)
	hub.mu.Lock()
	hub.subs[ch] = struct{}{}
	hub.mu.Unlock()
	return ch
}

// waitForUpdates consumes hub events until an `update` event with the
// wanted phase arrives, returning every update event seen along the way
// (timeout = the op never finished).
func waitForUpdates(t *testing.T, ch chan event, through string) []UpdateEvent {
	t.Helper()
	deadline := time.After(2 * time.Second)
	var out []UpdateEvent
	for {
		select {
		case ev := <-ch:
			if ev.class != "update" {
				continue
			}
			var got UpdateEvent
			if err := json.Unmarshal([]byte(ev.data), &got); err != nil {
				t.Fatalf("update payload %q: %v", ev.data, err)
			}
			out = append(out, got)
			if got.Phase == through {
				return out
			}
		case <-deadline:
			t.Fatalf("no %q update event; got %+v", through, out)
		}
	}
}

// A fresh server never published a phase — the banner stays off until
// the first check lands.
func TestUpdateNoPhaseBeforeFirstCheck(t *testing.T) {
	s, u := newTestUpdater(t)
	if ev := u.snapshot(); ev.Phase != "" {
		t.Fatalf("initial phase = %q, want none", ev.Phase)
	}
	if len(s.hub.buf) != 0 {
		t.Fatalf("hub history = %v, want empty", s.hub.buf)
	}
}

// The startup check compares the VERSION file against the latest tag and
// publishes `available` (badge on).
func TestUpdateCheckAvailable(t *testing.T) {
	s, u := newTestUpdater(t)
	writeVersion(t, s.opts.InstallRoot, "v1.0.0")
	u.check = func() (string, error) { return "v1.1.0", nil }
	ch := subscribe(s.hub)

	u.Check()

	if ev := u.snapshot(); ev.Phase != PhaseAvailable || ev.Latest != "v1.1.0" || ev.Installed != "v1.0.0" {
		t.Fatalf("state = %+v, want available v1.0.0→v1.1.0", ev)
	}
	got := waitForUpdates(t, ch, PhaseAvailable)
	if len(got) != 1 || got[0].Latest != "v1.1.0" || got[0].Installed != "v1.0.0" {
		t.Fatalf("published events = %+v, want one available event v1.0.0→v1.1.0", got)
	}
}

// Up to date: the badge stays off.
func TestUpdateCheckUpToDate(t *testing.T) {
	s, u := newTestUpdater(t)
	writeVersion(t, s.opts.InstallRoot, "v1.1.0")
	u.check = func() (string, error) { return "v1.1.0", nil }
	ch := subscribe(s.hub)

	u.Check()

	if ev := u.snapshot(); ev.Phase != PhaseUpToDate {
		t.Fatalf("phase = %q, want up-to-date", ev.Phase)
	}
	evs := waitForUpdates(t, ch, PhaseUpToDate)
	if len(evs) != 1 {
		t.Fatalf("update events = %+v, want exactly one", evs)
	}
}

// No VERSION file and no compiled version: the card reads "unknown",
// which compares as outdated — never silently stuck.
func TestUpdateCheckUnknownInstalled(t *testing.T) {
	s := New(Options{BasePort: 0, InstallRoot: t.TempDir()})
	u := s.updater
	u.check = func() (string, error) { return "v1.1.0", nil }

	u.Check()

	if ev := u.snapshot(); ev.Installed != "unknown" || ev.Phase != PhaseAvailable {
		t.Fatalf("state = %+v, want unknown installed → available", ev)
	}
}

// A failed check stays silent (no phase event, badge off); the card
// notes why it cannot compare.
func TestUpdateCheckFailureSilent(t *testing.T) {
	s, u := newTestUpdater(t)
	u.check = func() (string, error) { return "", errSentinel }
	ch := subscribe(s.hub)

	u.Check()

	if ev := u.snapshot(); ev.Phase != "" || !ev.Checked || !ev.CheckFailed {
		t.Fatalf("state = %+v, want checked-with-failure and no phase", ev)
	}
	deadline := time.After(100 * time.Millisecond)
	for {
		select {
		case ev := <-ch:
			if ev.class == "update" {
				t.Fatalf("failed check published %q, want silence", ev.data)
			}
		case <-deadline:
			return
		}
	}
}

// "Check now" re-runs the check on demand and re-publishes the phase.
func TestUpdateCheckNowReruns(t *testing.T) {
	s, u := newTestUpdater(t)
	writeVersion(t, s.opts.InstallRoot, "v1.0.0")
	tags := []string{"v1.1.0", "v1.0.0"}
	u.check = func() (string, error) {
		tag := tags[0]
		tags = tags[1:]
		return tag, nil
	}

	u.Check()
	u.Check()

	if ev := u.snapshot(); ev.Phase != PhaseUpToDate || ev.Latest != "v1.0.0" {
		t.Fatalf("state after second check = %+v, want up-to-date v1.0.0", ev)
	}
}

// The run: fetch script → bash --yes → on success the server re-reads
// the rewritten VERSION (§12 `applied`) and publishes the phase with the
// new installed version.
func TestUpdateRunAppliesAndRereadsVersion(t *testing.T) {
	s, u := newTestUpdater(t)
	writeVersion(t, s.opts.InstallRoot, "v1.0.0")
	u.check = func() (string, error) { return "v2.0.0", nil }
	u.Check()

	u.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	u.runScript = func(script string, args []string, emit func(string)) error {
		emit("downloading…")
		emit("extracting…")
		writeVersion(t, s.opts.InstallRoot, "v2.0.0") // install.sh rewrites VERSION
		return nil
	}
	ch := subscribe(s.hub)

	u.Run()

	evs := waitForUpdates(t, ch, PhaseApplied)
	var installing, applied *UpdateEvent
	for i := range evs {
		switch evs[i].Phase {
		case PhaseInstalling:
			installing = &evs[i]
		case PhaseApplied:
			applied = &evs[i]
		}
	}
	if installing == nil || applied == nil {
		t.Fatalf("update events = %+v, want installing then applied", evs)
	}
	if applied.Installed != "v2.0.0" || applied.Op == 0 {
		t.Fatalf("applied event = %+v, want op-keyed event with the re-read VERSION v2.0.0", applied)
	}
	if installing.Latest != "v2.0.0" || installing.Installed != "v1.0.0" || installing.Op != applied.Op {
		t.Fatalf("installing event = %+v, want same op with v1.0.0→v2.0.0", installing)
	}
	if ev := u.snapshot(); ev.Phase != PhaseApplied || !ev.Updated {
		t.Fatalf("state = %+v, want applied with the restart note latched", ev)
	}
}

// The run's script output streams as op-keyed line events — the log pane
// under the Update card filters on the update op's ID.
func TestUpdateRunStreamsLogLines(t *testing.T) {
	s, u := newTestUpdater(t)
	u.check = func() (string, error) { return "v2.0.0", nil }
	u.Check()

	u.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	u.runScript = func(script string, args []string, emit func(string)) error {
		emit("installing oc-sandbox…")
		return nil
	}
	ch := subscribe(s.hub)

	u.Run()

	evs := waitForUpdates(t, ch, PhaseApplied)
	var lines []string
	for _, ev := range s.hub.buf {
		if ev.class == "line" && strings.Contains(ev.data, `"op":`+strconv.Itoa(evs[len(evs)-1].Op)) {
			lines = append(lines, ev.data)
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "installing oc-sandbox…") {
		t.Fatalf("line events = %v, want the op-%d script output", lines, evs[len(evs)-1].Op)
	}
}

// Failure publishes `failed` with the reason; the error stays visible
// until the next attempt clears it.
func TestUpdateRunFailurePersistsUntilNextAttempt(t *testing.T) {
	s, u := newTestUpdater(t)
	u.check = func() (string, error) { return "v2.0.0", nil }
	u.Check()

	u.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	u.runScript = func(script string, args []string, emit func(string)) error {
		return errSentinel
	}
	ch := subscribe(s.hub)

	u.Run()

	evs := waitForUpdates(t, ch, PhaseFailed)
	last := evs[len(evs)-1]
	if last.Phase != PhaseFailed || last.Error != errSentinel.Error() {
		t.Fatalf("failed event = %+v, want the reason carried", last)
	}
	if ev := u.snapshot(); ev.RunErr != errSentinel.Error() {
		t.Fatal("failure must persist on the card until the next attempt")
	}

	// The next attempt starts clean: success clears the failure.
	u.runScript = func(script string, args []string, emit func(string)) error { return nil }
	u.Run()
	waitForUpdates(t, ch, PhaseApplied)
	if ev := u.snapshot(); ev.RunErr != "" || ev.Phase != PhaseApplied {
		t.Fatalf("after retry state = %+v, want applied with the error cleared", ev)
	}
}

// While the update runs it is globally exclusive (§12) — but Check now
// stays usable: it must not error the run and must not flip the
// installing phase.
func TestUpdateCheckNowStaysUsableWhileInstalling(t *testing.T) {
	s, u := newTestUpdater(t)
	u.check = func() (string, error) { return "v2.0.0", nil }
	u.Check()

	release := make(chan struct{})
	u.fetchScript = func() (string, error) { return "/tmp/install.sh", nil }
	u.runScript = func(script string, args []string, emit func(string)) error {
		<-release
		return nil
	}
	ch := subscribe(s.hub)
	u.Run()
	waitForUpdates(t, ch, PhaseInstalling)

	// The update op is exclusive: a plain op on another key must wait.
	otherDone := make(chan struct{})
	s.queue.Launch(webOp{Key: "ws-a", Run: func(emit func(string)) error { return nil }, Done: func(err error) { close(otherDone) }})
	select {
	case <-otherDone:
		t.Fatal("an op started while the update runs — exclusivity broken")
	case <-time.After(100 * time.Millisecond):
	}

	// Check now during the run: usable, and it does not disturb the phase.
	u.check = func() (string, error) { return "v9.9.9", nil }
	u.Check()
	if ev := u.snapshot(); ev.Phase != PhaseInstalling {
		t.Fatalf("phase = %q after check-during-run, want installing", ev.Phase)
	}
	close(release)
	waitForUpdates(t, ch, PhaseApplied)
}

// A second run request while installing is a no-op — the disabled Update
// button is enforced server-side too (no double update).
func TestUpdateRunWhileInstallingIsNoOp(t *testing.T) {
	s, u := newTestUpdater(t)
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

	u.Run() // must not start a second op

	close(release)
	waitForUpdates(t, ch, PhaseApplied)
	if started != 1 {
		t.Fatalf("run script executed %d times, want exactly 1", started)
	}
}
