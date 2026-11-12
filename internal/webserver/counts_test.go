package webserver

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// The sidebar count pills ride one GET /counts fragment: registered
// workspaces, catalog entries, running sandbox containers and built
// edition images — the numbers all five pills show after one fetch.

// countsServer starts a Server on a scratch home with two registered
// workspaces ("alpha", "beta") and the given podman fake.
func countsServer(t *testing.T, run *cmdrun.Fake) *Server {
	t.Helper()
	reg, _ := fixtureRegistry(t, "alpha", "beta")
	s := New(Options{BasePort: 0, Home: reg.Home, Deps: &Deps{Run: run, Registry: reg}})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCountsAllFive(t *testing.T) {
	s := countsServer(t, fakePodman(
		[]string{"opencode-sandbox-alpha"}, // one running container
		nil, nil,
		[]string{"opencode-sandbox-swdev\t1.2GB", "oc-searxng:latest\t300MB"},
		nil,
	))
	postForm(t, s.URL()+"catalogs/trackers", map[string]string{
		"action": "add", "name": "GitHub", "kind": "github",
	})
	postForm(t, s.URL()+"catalogs/providers", map[string]string{
		"action": "add", "name": "One", "endpoint": "https://one.example.com/v1",
	})
	postForm(t, s.URL()+"catalogs/providers", map[string]string{
		"action": "add", "name": "Two", "endpoint": "https://two.example.com/v1",
	})

	resp := get(t, s.URL()+"counts")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("counts status = %d, want 200", resp.StatusCode)
	}
	doc := body(t, resp)
	for _, want := range []string{
		`data-counts`,
		`data-ws="2"`,         // registered workspaces
		`data-trackers="1"`,   // tracker catalog entries
		`data-providers="2"`,  // provider catalog entries
		`data-containers="1"`, // running sandbox containers
		`data-images="1"`,     // built edition images (infra excluded)
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("GET /counts missing %q:\n%s", want, doc)
		}
	}
}

// Fail-soft: a corrupted registry leaves the ws count out of the
// fragment (the pill keeps its last value) while the rest still render.
func TestCountsFailSoftRegistry(t *testing.T) {
	s := countsServer(t, fakePodman(nil, nil, nil, nil, nil))
	home := s.opts.Home
	bad := filepath.Join(home, ".config", "oc-sandbox", "workspaces.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write workspaces.json: %v", err)
	}

	doc := body(t, get(t, s.URL()+"counts"))
	if strings.Contains(doc, "data-ws=") {
		t.Fatalf("unreadable registry must omit data-ws:\n%s", doc)
	}
	for _, want := range []string{`data-trackers="0"`, `data-providers="0"`, `data-containers="0"`, `data-images="0"`} {
		if !strings.Contains(doc, want) {
			t.Fatalf("GET /counts missing %q:\n%s", want, doc)
		}
	}
}
