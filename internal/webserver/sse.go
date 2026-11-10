package webserver

import "net/http"

// Hub fans out the one multiplexed SSE stream per browser tab
// (GET /events, spec §9): event classes line (taskmgr log lines),
// workspace (card/config re-render), update (§12 phases) and shutdown
// (quit flow), a history buffer, and reconnect replay keyed by the SSE
// id: = event serial. taskmgr's emit→LineEvent transport maps onto
// PublishLine.
type Hub struct{}

func newHub() *Hub { return &Hub{} }

func (h *Hub) serveEvents(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
