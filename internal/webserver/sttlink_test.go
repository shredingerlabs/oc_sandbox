package webserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// GET /workspaces/{name}/stt-link tests: the Open Workspace card's mic
// button resolves the dictation sidecar's live published port (podman
// port, never persisted — the pair-link pattern); a missing member
// answers the refusal sidecar.

// GET /workspaces/{name}/stt-link: the STT member's published loopback
// binding, resolved live — ok:true with the sidecar URL.
func TestSttLinkResolvesLivePort(t *testing.T) {
	home := t.TempDir()
	root := seedWorkspace(t, home, "alpha", "swdev", nil)
	ctr := registry.ContainerName(registry.ContainerIdentity(root))
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		if name == "podman" && args[0] == "port" {
			if args[1] != ctr || args[2] != "8090/tcp" {
				t.Fatalf("podman port invocation = %v", args)
			}
			return "127.0.0.1:8091\n", nil
		}
		return "", nil
	}}
	s := startServerWith(t, home, fake)

	v := getSttJSON(t, s.URL()+"workspaces/alpha/stt-link")
	if !v.OK || v.URL != "http://127.0.0.1:8091" {
		t.Fatalf("sidecar = %+v, want ok with the published binding", v)
	}
}

// No published 8090 port (stopped pod, offline run, next-start-pending
// toggle flip) → the refusal sidecar; the card note carries the message.
func TestSttLinkNoMemberRefuses(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	fake := &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		return "", nil
	}}
	s := startServerWith(t, home, fake)

	v := getSttJSON(t, s.URL()+"workspaces/alpha/stt-link")
	if v.OK || v.URL != "" || v.Error != "dictation sidecar not running — start the workspace" {
		t.Fatalf("sidecar = %+v, want the not-running refusal", v)
	}
}

// Unknown workspace refuses.
func TestSttLinkUnknownWorkspace(t *testing.T) {
	s := startFixtureServer(t, t.TempDir())
	if resp := get(t, s.URL()+"workspaces/nope/stt-link"); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

// getSttJSON GETs and decodes the mic button's JSON sidecar.
func getSttJSON(t *testing.T, url string) sttLinkView {
	t.Helper()
	resp := get(t, url)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status = %d, want 200", url, resp.StatusCode)
	}
	var v sttLinkView
	if err := json.NewDecoder(strings.NewReader(body(t, resp))).Decode(&v); err != nil {
		t.Fatalf("decode sidecar: %v", err)
	}
	return v
}
