package webserver

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// Queue semantics (#153, §9 — mirror taskmgr exactly): one-at-a-time per
// workspace (queued-not-refused), parallel across workspaces, no
// 409-refusal path — exercised through the start route on the shared
// opRunner (the update run's exclusivity is #160's).
func TestQueueOneAtATimePerWorkspaceParallelAcross(t *testing.T) {
	home := t.TempDir()
	rootA := seedWorkspace(t, home, "alpha", "swdev", nil)
	rootB := seedWorkspace(t, home, "beta", "swdev", nil)

	// The fake blocks every start.sh on release: ops that overlap would
	// deadlock the assertions, so each start is hand-released.
	inFlight := make(chan string, 8)
	release := make(chan struct{})
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		switch name {
		case "podman":
			return "", nil // nothing running: both starts run start.sh
		case "bash":
			inFlight <- args[1]
			<-release
			return "", nil
		}
		return "", nil
	}}
	s := startServerWith(t, home, fake)

	r1 := postMap(t, s.URL()+"workspaces/alpha/start", nil)
	if r1.StatusCode != 200 {
		t.Fatalf("first start: %d", r1.StatusCode)
	}
	waitInFlight(t, inFlight, rootA, "first alpha start")

	// Second alpha start is queued-not-refused — 200 while the first op
	// is still running.
	r2 := postMap(t, s.URL()+"workspaces/alpha/start", nil)
	if r2.StatusCode != 200 {
		t.Fatalf("queued start must not be refused: %d", r2.StatusCode)
	}

	// A beta start runs IN PARALLEL with the still-running alpha op.
	rb := postMap(t, s.URL()+"workspaces/beta/start", nil)
	if rb.StatusCode != 200 {
		t.Fatalf("beta start: %d", rb.StatusCode)
	}
	waitInFlight(t, inFlight, rootB, "beta start while alpha still runs")

	// Release alpha: the queued alpha op starts only after the first
	// finished — one-at-a-time per workspace.
	release <- struct{}{}
	waitInFlight(t, inFlight, rootA, "queued alpha start after the first finished")

	// Drain: three ops total, two for alpha one for beta.
	release <- struct{}{}
	release <- struct{}{}
	awaitWorkspaceCount(t, s, 3)
}

func waitInFlight(t *testing.T, ch chan string, want, what string) {
	t.Helper()
	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("%s: started %s, want %s", what, got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("%s never started", what)
	}
}

// awaitWorkspaceCount waits (bounded) until n workspace completion
// events have been observed on the SSE stream.
func awaitWorkspaceCount(t *testing.T, s *Server, n int) {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(s.URL() + "events")
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	defer resp.Body.Close()
	var acc []byte
	for {
		if strings.Count(string(acc), "event: workspace") >= n {
			return
		}
		tmp := make([]byte, 1024)
		m, rerr := resp.Body.Read(tmp)
		if m > 0 {
			acc = append(acc, tmp[:m]...)
			continue
		}
		if rerr != nil {
			t.Fatalf("stream ended after %d workspace events, want %d", strings.Count(string(acc), "event: workspace"), n)
		}
	}
}
