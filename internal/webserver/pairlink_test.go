package webserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// POST /workspaces/{name}/pair-link tests: see pairlink_test.go — the
// sidecar type lives in wsactions.go.

// POST /workspaces/{name}/pair-link (§10/§9): a FRESH one-time sign-in
// link per request — WebPort (live podman port, never persisted) plus
// PairLink (opencode pair) — with both pair_link and plain_url in the
// sidecar (plain URL = fallback when pairing fails or the service is
// mid-boot).
func TestPairLinkFreshPerRequest(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	ctr := registry.ContainerName(registry.ContainerIdentity(root))
	links := []string{"http://127.0.0.1:4123/pair?tkt=one", "http://127.0.0.1:4123/pair?tkt=two"}
	n := 0
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		switch {
		case name == "podman" && args[0] == "port":
			return "127.0.0.1:4123\n", nil
		case name == "podman" && args[0] == "exec":
			// `podman exec --user dev <ctr> opencode pair --url <hostURL>`
			if len(args) < 8 || args[5] != "pair" {
				return "", nil
			}
			if args[3] != ctr || args[7] != "http://127.0.0.1:4123" {
				t.Fatalf("pair invocation = %v", args)
			}
			link := links[n%len(links)]
			n++
			return "open " + link + " in your browser\n", nil
		}
		return "", nil
	}}
	s := startServerWith(t, home, fake)

	first := postJSON(t, s.URL()+"workspaces/alpha/pair-link")
	second := postJSON(t, s.URL()+"workspaces/alpha/pair-link")
	if first.PairLink == "" || second.PairLink == "" {
		t.Fatalf("pair links empty: %+v %+v", first, second)
	}
	if first.PairLink == second.PairLink {
		t.Fatal("pair link must be fresh per request — one-time links, every access pairs afresh (§10)")
	}
	if first.PlainURL != "http://127.0.0.1:4123" {
		t.Fatalf("plain_url = %q, want the published host binding", first.PlainURL)
	}
}

// Pairing failure keeps the plain URL — the fallback when pairing
// fails or the v2 service is mid-boot.
func TestPairLinkFallbackOnPairingFailure(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	ctr := registry.ContainerName(registry.ContainerIdentity(root))
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		switch {
		case name == "podman" && args[0] == "port":
			return "127.0.0.1:4123\n", nil
		case name == "podman" && args[0] == "exec":
			_ = ctr
			return "", errPairFail
		}
		return "", nil
	}}
	s := startServerWith(t, home, fake)

	v := postJSON(t, s.URL()+"workspaces/alpha/pair-link")
	if v.PairLink != "" {
		t.Fatalf("pair_link = %q, want empty on pairing failure", v.PairLink)
	}
	if v.PlainURL != "http://127.0.0.1:4123" {
		t.Fatalf("plain_url = %q, want the published binding as fallback", v.PlainURL)
	}
}

// No published 4096 port (service not up / started without web) → both
// fields empty; the frontend shows its busy/Retry rows.
func TestPairLinkNoPublishedPort(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		return "", nil
	}}
	s := startServerWith(t, home, fake)

	v := postJSON(t, s.URL()+"workspaces/alpha/pair-link")
	if v.PairLink != "" || v.PlainURL != "" {
		t.Fatalf("sidecar = %+v, want both empty without a published port", v)
	}
}

// Unknown workspace refuses.
func TestPairLinkUnknownWorkspace(t *testing.T) {
	s := startFixtureServer(t, t.TempDir())
	if resp := postMap(t, s.URL()+"workspaces/nope/pair-link", nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

var errPairFail = &staticErr{"pair: service unavailable"}

type staticErr struct{ msg string }

func (e *staticErr) Error() string { return e.msg }

// postJSON posts and decodes the JSON sidecar.
func postJSON(t *testing.T, url string) pairLinkView {
	t.Helper()
	resp := postMap(t, url, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST %s: status = %d, want 200", url, resp.StatusCode)
	}
	var v pairLinkView
	if err := json.NewDecoder(strings.NewReader(body(t, resp))).Decode(&v); err != nil {
		t.Fatalf("decode sidecar: %v", err)
	}
	return v
}
