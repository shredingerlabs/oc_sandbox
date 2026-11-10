package webserver

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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
// a duplicate WebUI.
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
	if got := FindInstance(base); got != base+1 {
		t.Fatalf("FindInstance = %d, want %d (the displaced instance)", got, base+1)
	}
}

// Nothing answers in the scan window → 0, the launch proceeds to bind.
func TestFindInstanceNoneAnswers(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	base := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if got := FindInstance(base); got != 0 {
		t.Fatalf("FindInstance = %d, want 0 with nothing listening", got)
	}
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
	if got := portOf(t, s.URL()); got != base+1 {
		t.Fatalf("bound port %d, want %d (scan-up past the foreign app)", got, base+1)
	}
}
