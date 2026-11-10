package webserver

import (
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/config"
)

// startSettingsServer starts a test server against an isolated home so
// /settings reads and writes that home's global_config.json.
func startSettingsServer(t *testing.T) *Server {
	t.Helper()
	s := New(Options{BasePort: 0, Home: t.TempDir()})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func globalConfigFile(t *testing.T, home string) string {
	t.Helper()
	b, err := os.ReadFile(config.Paths{Home: home}.GlobalConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func postForm(t *testing.T, url string, form url.Values) *http.Response {
	t.Helper()
	resp, err := http.PostForm(url, form)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// GET /settings (spec §9) carries the global settings — display mode +
// default workspace path — as a JSON sidecar (plain fetch is simpler
// for the toggle; HTMX screens swap fragments). A missing
// global_config.json reads as the defaults: standard, empty path.
func TestSettingsGetDefaults(t *testing.T) {
	s := startSettingsServer(t)
	resp := get(t, s.URL()+"settings")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	got := body(t, resp)
	for _, want := range []string{`"display_mode": "standard"`, `"default_workspace_path": ""`} {
		if !strings.Contains(got, want) {
			t.Fatalf("GET /settings missing %q:\n%s", want, got)
		}
	}
}

// A stored display_mode (and workspace path) is what GET reports — the
// stored file is the single truth, read per request (no cache).
func TestSettingsGetCarriesStoredValues(t *testing.T) {
	home := t.TempDir()
	if err := config.CreateGlobalConfig(home, "/home/user/oc-sandbox"); err != nil {
		t.Fatal(err)
	}
	if err := config.SetDisplayMode(home, config.DisplayAdvanced); err != nil {
		t.Fatal(err)
	}
	s := New(Options{BasePort: 0, Home: home})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	got := body(t, get(t, s.URL()+"settings"))
	for _, want := range []string{`"display_mode": "advanced"`, `"default_workspace_path": "/home/user/oc-sandbox"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("GET /settings missing %q:\n%s", want, got)
		}
	}
}

// POST /settings persists display_mode to global_config.json (#152):
// the response echoes the stored value, a follow-up GET reflects it,
// and the other keys survive the write.
func TestSettingsPostPersistsDisplayMode(t *testing.T) {
	s := startSettingsServer(t)
	resp := postForm(t, s.URL()+"settings", url.Values{"display_mode": {"advanced"}})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := body(t, resp); !strings.Contains(got, `"display_mode": "advanced"`) {
		t.Fatalf("POST response missing the stored mode:\n%s", got)
	}
	got := body(t, get(t, s.URL()+"settings"))
	if !strings.Contains(got, `"display_mode": "advanced"`) {
		t.Fatalf("POST did not persist — GET shows:\n%s", got)
	}
	gc, err := config.ReadGlobalConfig(s.opts.Home)
	if err != nil {
		t.Fatal(err)
	}
	if gc.DisplayMode != config.DisplayAdvanced || gc.Version != "2.0" {
		t.Fatalf("persisted config: %+v", gc)
	}
}

// An unknown display_mode is refused without writing — the stored file
// stays untouched.
func TestSettingsPostRefusesUnknownMode(t *testing.T) {
	s := startSettingsServer(t)
	if err := config.CreateGlobalConfig(s.opts.Home, "/w"); err != nil {
		t.Fatal(err)
	}
	before := globalConfigFile(t, s.opts.Home)
	resp := postForm(t, s.URL()+"settings", url.Values{"display_mode": {"compact"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if after := globalConfigFile(t, s.opts.Home); after != before {
		t.Fatalf("refused POST rewrote global_config.json:\n%s", after)
	}
}
