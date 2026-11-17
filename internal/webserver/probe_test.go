package webserver

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// ProbeInstance is the single-instance seam (spec §4): before binding,
// the launcher probes the base port's token-less /healthz.
func TestProbeInstanceOcSandboxAnswersTrue(t *testing.T) {
	s := New(Options{BasePort: 0})
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = s.Close() }()
	port := portOf(t, s.URL())
	if !ProbeInstance(port) {
		t.Fatal("ProbeInstance = false on a running oc-sandbox WebUI, want true")
	}
}

func TestProbeInstanceForeignAppFalse(t *testing.T) {
	// A foreign HTTP app answering /healthz with anything but "ok" —
	// it reveals nothing and must be scanned past, not pointed at.
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "hello")
	}))
	defer foreign.Close()
	if ProbeInstance(portOf(t, foreign.URL)) {
		t.Fatal("ProbeInstance = true on a foreign app, want false")
	}
}

func TestProbeInstanceNothingListeningFalse(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close() // closed: nothing answers
	if ProbeInstance(port) {
		t.Fatal("ProbeInstance = true with nothing listening, want false")
	}
}

// A displaced instance (§4/story 10): a foreign app holds the base
// port and the running oc-sandbox sits a few ports up — FindInstance
// probes the whole scan window, so a second launch still points at the
// running instance and exits 0 instead of scanning past it and starting
// a duplicate WebUI. The assertion keys on the port the instance
// actually bound, not base+1: another process may legitimately take
// base+1 first (random ephemeral allocation), the instance then binds
// one port up, and FindInstance must still find it.
func TestFindInstanceFindsDisplacedInstance(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "hello")
	}))
	defer foreign.Close()
	base := portOf(t, foreign.URL)
	inner := New(Options{BasePort: base + 1})
	if err := inner.Start(); err != nil {
		t.Fatalf("start displaced instance: %v", err)
	}
	defer func() { _ = inner.Close() }()
	bound := portOf(t, inner.URL())
	if got := FindInstance(base); got != bound {
		t.Fatalf("FindInstance = %d, want %d (the displaced instance)", got, bound)
	}
}

// Every port in the scan window answering like a foreign (non-instance)
// app → 0, the launch proceeds to bind (§4). The window is held for the
// test's lifetime (reserveWindow) where bindable, so nothing can start
// inside it mid-run and answer "ok" — a property a random base cannot
// guarantee (a real instance starting inside a random window once
// failed this test).
func TestFindInstanceNoneAnswers(t *testing.T) {
	base := reserveWindow(t)
	if got := FindInstance(base); got != 0 {
		t.Fatalf("FindInstance = %d, want 0 — every window port is a foreign app", got)
	}
}

// reserveWindow holds a scan window's ports with foreign-app listeners
// ("hello" — anything but the token-less "ok"): the deterministic
// nothing-answers-like-an-instance scenario. Ports occupied by ambient
// churn (source sockets, foreign listeners) are left as holes — they
// never answer "ok", which is what the assertion is about; only a live
// instance taints the window, and a tainted attempt is torn down and
// retried at a fresh random base. If the host stays too contaminated
// to ever assemble a clean window, the test skips.
func reserveWindow(t *testing.T) int {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "hello")
	})
	const attempts = 200 // tainted windows are rare; plenty of slack
	for attempt := 0; attempt < attempts; attempt++ {
		anchor, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		base := anchor.Addr().(*net.TCPAddr).Port
		lns := []net.Listener{anchor}
		tainted := false
		for offset := 1; offset < scanWindow; offset++ {
			ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base+offset)))
			if err != nil {
				if ProbeInstance(base + offset) { // an instance — not a hole
					tainted = true
					break
				}
				continue // ambient occupant: scans past like any foreign app
			}
			lns = append(lns, ln)
		}
		if tainted {
			for _, l := range lns {
				_ = l.Close()
			}
			continue
		}
		for _, ln := range lns {
			ln := ln
			go func() { _ = http.Serve(ln, handler) }()
			t.Cleanup(func() { _ = ln.Close() })
		}
		return base
	}
	t.Skip("no clean scan window — host too occupied for a deterministic scan")
	return 0
}

// portOf extracts the loopback port from a scheme://host:port/ URL.
func portOf(t *testing.T, url string) int {
	t.Helper()
	var hostPort string
	if _, err := fmt.Sscanf(url, "http://%s", &hostPort); err != nil {
		t.Fatalf("parse %q: %v", url, err)
	}
	for i := len(hostPort) - 1; i >= 0; i-- {
		if hostPort[i] == ':' {
			var p int
			if _, err := fmt.Sscanf(hostPort[i+1:], "%d", &p); err != nil {
				t.Fatalf("parse port from %q: %v", hostPort, err)
			}
			return p
		}
		if hostPort[i] == '/' {
			hostPort = hostPort[:i]
		}
	}
	t.Fatalf("no port in %q", url)
	return 0
}

// A foreign app on the base port scans upward past it (spec §4) — the
// WebUI binds the next free port and the chosen port is never persisted.
// The assertion checks the scan-up contract (strictly above the foreign
// port, within the window), not base+1 exactly: another process may
// legitimately take base+1 first, and binding base+2 then is correct
// scan-up behavior, not a failure.
func TestStartScansPastForeignApp(t *testing.T) {
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "hello")
	}))
	defer foreign.Close()
	base := portOf(t, foreign.URL)
	s := New(Options{BasePort: base})
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() { _ = s.Close() }()
	if got := portOf(t, s.URL()); got <= base || got >= base+scanWindow {
		t.Fatalf("bound port %d, want a scan-up past the foreign app within %d–%d", got, base+1, base+scanWindow-1)
	}
}
