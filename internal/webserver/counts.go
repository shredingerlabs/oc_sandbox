package webserver

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/shredingerlabs/oc-sandbox/internal/catalogs"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// counts.go serves GET /counts — the one fragment behind the sidebar's
// count pills: registered workspaces, tracker/provider catalog entries,
// running sandbox containers and built edition images. One fetch per
// navigation refreshes all five pills at once. Fail-soft: the podman
// counts ride the discovery adapters' own empty-list-on-error
// semantics (the same every screen renders); an unreadable registry
// omits its attribute so the pill keeps its last value.

// handleCounts serves GET /counts: the data-counts div carrying the
// five numbers as data attributes. An omitted attribute means "no
// fresh number" — the client leaves that pill alone.
func (s *Server) handleCounts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	run := s.runnerSeam()
	cat := catalogs.Read(s.opts.Home)

	var b strings.Builder
	b.WriteString(`<div data-counts`)
	if n, ok := s.countWorkspaces(); ok {
		b.WriteString(` data-ws="` + strconv.Itoa(n) + `"`)
	}
	b.WriteString(` data-trackers="` + strconv.Itoa(len(cat.Trackers)) + `"`)
	b.WriteString(` data-providers="` + strconv.Itoa(len(cat.Providers)) + `"`)
	b.WriteString(` data-containers="` + strconv.Itoa(len(discovery.RunningContainers(run))) + `"`)
	b.WriteString(` data-images="` + strconv.Itoa(countEditionImages(run)) + `"`)
	b.WriteString(`></div>`)
	_, _ = w.Write([]byte(b.String()))
}

// countWorkspaces counts the registered workspaces; ok=false when the
// registry is unreadable (no fresh number — the pill keeps its value).
// A missing workspaces.json is the first-run state, not an error: zero.
func (s *Server) countWorkspaces() (int, bool) {
	entries, err := s.regView().AllOrdered()
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return 0, true
		}
		return 0, false
	}
	return len(entries), true
}

// countEditionImages counts the built edition images (base and every
// shipped/custom edition stage; the infra images — oc-proxy,
// oc-searxng — are not user-built editions and never count).
func countEditionImages(run cmdrun.Runner) int {
	n := 0
	for _, img := range discovery.SandboxImages(run) {
		if discovery.EditionFromImage(img.Tag) != "" {
			n++
		}
	}
	return n
}
