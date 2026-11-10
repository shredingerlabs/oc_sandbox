package webserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
)

// maxHistory caps the SSE history buffer — reconnect replay serves at
// most this many events after the Last-Event-ID serial.
const maxHistory = 512

// event is one hub-distributed SSE event.
type event struct {
	serial uint64 // the SSE id: — monotonic per hub instance
	class  string // line | workspace | update | shutdown
	data   string // JSON payload body
}

// Hub fans out the one multiplexed SSE stream per browser tab
// (GET /events, spec §9): event classes line (taskmgr log lines keyed
// by Op.ID), workspace (card/config re-render on state change), update
// (§12 badge phases) and shutdown (quit flow §4). A history buffer plus
// live flush gives reconnect replay keyed by the SSE id: = event
// serial; taskmgr's emit→LineEvent transport maps onto PublishLine.
type Hub struct {
	mu   sync.Mutex
	buf  []event
	subs map[chan event]struct{}
	next uint64
}

func newHub() *Hub {
	return &Hub{subs: map[chan event]struct{}{}}
}

// publish stores the event in the history buffer and fans it out to
// every live stream without blocking them (a slow tab drops the live
// copy — reconnect replay covers it); it returns the event serial.
func (h *Hub) publish(class, data string) uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	ev := event{serial: h.next, class: class, data: data}
	h.buf = append(h.buf, ev)
	if len(h.buf) > maxHistory {
		h.buf = h.buf[len(h.buf)-maxHistory:]
	}
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
	return ev.serial
}

// PublishLine streams one taskmgr log line: the Op.ID-keyed line event
// (spec §9 `line` — taskmgr's emit→LineEvent transport).
func (h *Hub) PublishLine(opID int, text string) uint64 {
	return h.publish("line", fmt.Sprintf(`{"op":%d,"text":%s}`, opID, mustJSON(text)))
}

// PublishWorkspace asks open tabs to re-render card/config state.
func (h *Hub) PublishWorkspace() uint64 {
	return h.publish("workspace", "{}")
}

// PublishUpdate pushes one §12 update phase (up-to-date / available /
// installing / applied / failed) for the footer banner and Update card.
func (h *Hub) PublishUpdate(phase string) uint64 {
	return h.publish("update", fmt.Sprintf(`{"phase":%s}`, mustJSON(phase)))
}

// PublishShutdown is the quit flow's best-effort push (§4): open tabs
// swap to the server-stopped screen; streams end, no reconnect.
func (h *Hub) PublishShutdown() uint64 {
	return h.publish("shutdown", "{}")
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic("webserver: SSE payload: " + err.Error())
	}
	return string(b)
}

// serveEvents writes the multiplexed stream: replay of buffered events
// after the client's Last-Event-ID serial, then live flush until the
// client disconnects or the server shuts down.
func (h *Hub) serveEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	last := lastEventID(r)

	// Subscribe and snapshot the replay under one lock, so replay and
	// live delivery never duplicate or skip a serial.
	h.mu.Lock()
	live := make(chan event, 64)
	h.subs[live] = struct{}{}
	var replay []event
	for _, ev := range h.buf {
		if ev.serial > last {
			replay = append(replay, ev)
		}
	}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.subs, live)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "retry: 2000\n\n")
	for _, ev := range replay {
		if writeEvent(w, ev) {
			flusher.Flush()
		}
	}
	flusher.Flush()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, open := <-live:
			if !open {
				return
			}
			if !writeEvent(w, ev) {
				return
			}
			flusher.Flush()
			if ev.class == "shutdown" {
				return // stream ends: tabs stop reconnecting (§12)
			}
		}
	}
}

// lastEventID parses the reconnect cursor — 0 when absent or malformed
// (a malformed id replays history; harmless).
func lastEventID(r *http.Request) uint64 {
	id, err := strconv.ParseUint(r.Header.Get("Last-Event-ID"), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// writeEvent frames one event; false when the client went away.
func writeEvent(w http.ResponseWriter, ev event) bool {
	_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.serial, ev.class, ev.data)
	return err == nil
}
