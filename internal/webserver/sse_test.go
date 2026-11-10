package webserver

import (
	"bufio"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// readEvent reads one SSE event block (id/event/data lines) from a
// live /events stream, failing the test on timeout.
func readEvent(t *testing.T, sse *bufio.Reader) map[string]string {
	t.Helper()
	ev := map[string]string{}
	for {
		lineCh := make(chan string, 1)
		go func() {
			line, err := sse.ReadString('\n')
			if err != nil {
				lineCh <- fmt.Sprintf("ERR:%v", err)
				return
			}
			lineCh <- strings.TrimRight(line, "\n")
		}()
		select {
		case line := <-lineCh:
			if strings.HasPrefix(line, "ERR:") {
				t.Fatalf("read event: %s", line[4:])
			}
			if line == "" {
				if len(ev) > 0 {
					return ev // blank line ends the block
				}
				continue
			}
			if k, v, ok := strings.Cut(line, ": "); ok && k != "retry" {
				ev[k] = v
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timeout waiting for SSE event")
		}
	}
}

// connect opens the SSE stream and returns its reader after asserting
// the stream headers.
func connect(t *testing.T, url string) *bufio.Reader {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("connect SSE: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("SSE status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("SSE content-type = %q, want text/event-stream", ct)
	}
	if resp.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("SSE missing Referrer-Policy")
	}
	return bufio.NewReader(resp.Body)
}

// The hub distributes events to every connected tab and frames them
// with the SSE id: = event serial.
func TestSSEDistribution(t *testing.T) {
	s := startTestServer(t)
	a := connect(t, s.URL()+"events")
	b := connect(t, s.URL()+"events")

	serial := s.Hub().PublishLine(7, "build step 1")
	if serial == 0 {
		t.Fatal("PublishLine must return a non-zero serial")
	}
	wantData := fmt.Sprintf(`{"op":%d,"text":"build step 1"}`, 7)

	for name, r := range map[string]*bufio.Reader{"a": a, "b": b} {
		ev := readEvent(t, r)
		if ev["event"] != "line" || ev["id"] != strconv.FormatUint(serial, 10) || ev["data"] != wantData {
			t.Fatalf("client %s: got %v, want line id=%d data=%s", name, ev, serial, wantData)
		}
	}
}

// Reconnect replay (spec §3/#150): a fresh connection carrying
// Last-Event-ID receives the buffered events after that serial, in
// order, then continues live; a connection without it replays the full
// history.
func TestSSEReplayFromLineSerial(t *testing.T) {
	s := startTestServer(t)
	// Warm the hub with a workspace event and two line events.
	s.Hub().PublishWorkspace()
	serial2 := s.Hub().PublishLine(3, "first line")
	serial3 := s.Hub().PublishLine(3, "second line")

	// No Last-Event-ID → full history replay, oldest first.
	full := connect(t, s.URL()+"events")
	ev1 := readEvent(t, full)
	ev2 := readEvent(t, full)
	ev3 := readEvent(t, full)
	if ev1["event"] != "workspace" || ev2["id"] != strconv.FormatUint(serial2, 10) ||
		ev3["data"] != fmt.Sprintf(`{"op":3,"text":"second line"}`) {
		t.Fatalf("full replay wrong: %v / %v / %v", ev1, ev2, ev3)
	}

	// Last-Event-ID = serial2 → only serial3 replays, then live events flow.
	req, _ := http.NewRequest("GET", s.URL()+"events", nil)
	req.Header.Set("Last-Event-ID", strconv.FormatUint(serial2, 10))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("replay connect: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	replay := bufio.NewReader(resp.Body)
	ev := readEvent(t, replay)
	if ev["id"] != strconv.FormatUint(serial3, 10) || ev["event"] != "line" {
		t.Fatalf("replay = %v, want only serial %d", ev, serial3)
	}
	liveSerial := s.Hub().PublishWorkspace()
	ev = readEvent(t, replay)
	if ev["event"] != "workspace" || ev["id"] != strconv.FormatUint(liveSerial, 10) {
		t.Fatalf("live-after-replay = %v, want workspace id=%d", ev, liveSerial)
	}
}

// Quit flow (§4/#150): PublishShutdown pushes the shutdown event class
// to every live stream and ends the response — tabs swap to the
// server-stopped screen and never reconnect.
func TestSSEShutdown(t *testing.T) {
	s := startTestServer(t)
	r := connect(t, s.URL()+"events")
	s.Hub().PublishShutdown()
	ev := readEvent(t, r)
	if ev["event"] != "shutdown" {
		t.Fatalf("got %v, want shutdown", ev)
	}
	// The stream must end after the shutdown push.
	if _, err := r.Peek(1); err == nil {
		// Give the server a moment to close, then re-check.
		time.Sleep(100 * time.Millisecond)
		if _, err := r.Peek(1); err == nil {
			t.Fatal("stream did not end after shutdown")
		}
	}
}

// update events carry the §12 phase in their payload.
func TestSSEUpdatePhase(t *testing.T) {
	s := startTestServer(t)
	r := connect(t, s.URL()+"events")
	s.Hub().PublishUpdate("available")
	ev := readEvent(t, r)
	if ev["event"] != "update" || ev["data"] != `{"phase":"available"}` {
		t.Fatalf("got %v, want update phase=available", ev)
	}
}
