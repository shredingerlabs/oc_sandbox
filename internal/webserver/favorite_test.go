package webserver

// Favorite toggle (#175): POST /workspaces/{name}/favorite flips the
// display-only registry flag through the atomic write and publishes the
// workspace SSE event so open tabs re-render the sections — no
// confirmation popup and no toast, the card's section move is the
// feedback. The flag survives the round-trip in workspaces.json.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

func TestWorkspacesFavoriteToggle(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	s := startFixtureServer(t, home)

	// On: the flip lands in the registry, and the re-render moves the
	// card under the Favorite workspaces heading (the single card is
	// all-favorited, so the Registered section disappears, #173).
	v := sidecarPOST(t, s.URL()+"workspaces/alpha/favorite", "")
	if !scOK(v) {
		t.Fatalf("favorite toggle sidecar = %v, want ok", v)
	}
	w, err := (&registry.Registry{Home: home}).ByName("alpha")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if !w.Favorite {
		t.Fatal("the POST must persist favorite=true in workspaces.json")
	}
	doc := body(t, get(t, s.URL()+"workspaces"))
	if !strings.Contains(doc, "<h2>Favorite workspaces</h2>") || !strings.Contains(doc, `data-favorite="true"`) {
		t.Fatalf("favorited card must render under the favorites section:\n%s", doc)
	}
	if strings.Contains(doc, "<h2>Registered workspaces</h2>") {
		t.Fatalf("all-favorited home must hide the Registered section:\n%s", doc)
	}

	// Off: the second POST flips back — the card returns under
	// Registered workspaces and the flag reads false again.
	v = sidecarPOST(t, s.URL()+"workspaces/alpha/favorite", "")
	if !scOK(v) {
		t.Fatalf("un-favorite sidecar = %v, want ok", v)
	}
	w, err = (&registry.Registry{Home: home}).ByName("alpha")
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	if w.Favorite {
		t.Fatal("the second POST must flip favorite back to false")
	}
	doc = body(t, get(t, s.URL()+"workspaces"))
	if strings.Contains(doc, "<h2>Favorite workspaces</h2>") ||
		strings.Contains(doc, `data-favorite="true"`) {
		t.Fatalf("un-favorited card must return under Registered workspaces:\n%s", doc)
	}
	if !strings.Contains(doc, "<h2>Registered workspaces</h2>") {
		t.Fatalf("registered card must render under the Registered section:\n%s", doc)
	}
}

// The toggle publishes the workspace SSE event (a plain registry write
// has no op): open tabs re-render the sections without a reload.
func TestWorkspacesFavoritePublishesWorkspaceEvent(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	s := startFixtureServer(t, home)
	if v := sidecarPOST(t, s.URL()+"workspaces/alpha/favorite", ""); !scOK(v) {
		t.Fatalf("favorite toggle sidecar = %v, want ok", v)
	}
	awaitWorkspace(t, s)
}

// The workspace name is the URL identity: an unknown name 404s like the
// other card actions.
func TestWorkspacesFavoriteUnknown(t *testing.T) {
	home := t.TempDir()
	seedWorkspace(t, home, "alpha", "swdev", nil)
	s := startFixtureServer(t, home)
	resp := postMap(t, s.URL()+"workspaces/nope/favorite", nil)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown workspace favorite: status = %d, want 404", resp.StatusCode)
	}
}
