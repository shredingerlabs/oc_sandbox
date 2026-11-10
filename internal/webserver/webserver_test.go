package webserver

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// startTestServer starts a real Server on 127.0.0.1 with a random port
// (BasePort 0) and returns it plus an HTTP client with no redirect
// following (auth grammar must be assertable status-by-status).
func startTestServer(t *testing.T) *Server {
	t.Helper()
	s := New(Options{BasePort: 0})
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func body(t *testing.T, resp *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// GET /healthz is token-less and reveals nothing (#150): plain-text "ok",
// no JSON envelope, no version, no paths, no token echo.
func TestHealthzTokenLessAndSilent(t *testing.T) {
	s := startTestServer(t)
	resp := get(t, s.baseURL()+"healthz")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("content-type = %q, want text/plain", got)
	}
	if got := body(t, resp); got != "ok\n" {
		t.Fatalf("body = %q, want %q", got, "ok\n")
	}
}

// Index shell (§5/#150): GET / under the token prefix serves the Signal
// index shell — sidebar tree (Workspace → Open/New; Settings sub-menus),
// Standard/Advanced control, dark/bright theme toggle, footer update
// banner slot. Screens are stubs (later tickets).
func TestIndexShell(t *testing.T) {
	s := startTestServer(t)
	resp := get(t, s.URL())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	doc := body(t, resp)
	for _, want := range []string{
		`data-nav="ws-open"`, // sidebar: Workspace → Open
		`data-nav="ws-new"`,  // sidebar: Workspace → New
		`data-nav="set-vcs"`, // sidebar: Settings → VCS Tracker
		`data-nav="set-ai"`,  // sidebar: Settings → AI-Provider
		`data-nav="set-update"`,
		`data-role="display-mode"`,  // Standard/Advanced control slot
		`data-role="theme-toggle"`,  // dark/bright toggle slot
		`data-role="update-banner"`, // footer banner slot
		`data-role="screen"`,        // screen content slot (stub)
		`data-display="standard"`,   // display-mode default on <html> (#152)
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("index shell missing %q", want)
		}
	}
}

// The shell serves its assets with the right content types, and the
// stylesheet carries both color modes (dark AND bright, §5).
func TestShellAssets(t *testing.T) {
	s := startTestServer(t)
	for _, c := range []struct{ path, ct string }{
		{"css/main.css", "text/css"},
		{"js/main.js", "text/javascript"},
		{"img/editions/swdev.svg", "image/svg+xml"},
		{"img/editions/base.svg", "image/svg+xml"},
	} {
		resp := get(t, s.URL()+c.path)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", c.path, resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, c.ct) {
			t.Fatalf("%s: content-type = %q, want %s", c.path, ct, c.ct)
		}
		if resp.Header.Get("Referrer-Policy") != "no-referrer" {
			t.Fatalf("%s: missing Referrer-Policy", c.path)
		}
	}
	css := body(t, get(t, s.URL()+"css/main.css"))
	for _, theme := range []string{"dark", "bright"} {
		if !strings.Contains(css, `data-theme="`+theme+`"`) {
			t.Fatalf("stylesheet missing the %q color mode", theme)
		}
	}
	// Display-mode machinery (#152): Standard hides advanced-only rows
	// client-side (immediate re-render, no reload).
	if !strings.Contains(css, `[data-display="standard"]`) ||
		!strings.Contains(css, "data-advanced-only") {
		t.Fatal("stylesheet missing the Standard display-mode hiding rule")
	}
	js := body(t, get(t, s.URL()+"js/main.js"))
	for _, want := range []string{
		`"settings"`,            // GET on load — stored truth, no localStorage
		`method: "POST"`,        // toggle persists via POST /settings
		`data-advanced-only`,    // stub rows carry the matrix marker
		"presets, see Advanced", // helper wording (spec §6, PresetsHint)
		"displaychange",         // screens re-render reactively on this event
	} {
		if !strings.Contains(js, want) {
			t.Fatalf("main.js missing %q", want)
		}
	}
}

// Auth grammar (#150): a tokenized URL is the capability. Missing and
// wrong tokens refuse EVERY route — pages, assets, SSE, unsafe verbs —
// with 404 (capability URLs don't advertise what exists behind them).
func TestAuthGrammar(t *testing.T) {
	s := startTestServer(t)
	base := s.baseURL() // http://127.0.0.1:<port>/
	withTok := s.URL()  // .../t/<token>/
	routes := []string{
		"",
		"css/main.css",
		"img/editions/swdev.svg",
		"events",
		"settings",
		"workspaces",
		"catalogs",
	}
	for _, c := range []struct{ name, prefix string }{
		{"missing token", base},
		{"wrong token", base + "t/00000000000000000000000000000000/"},
		{"wrong token, no trailing slash", base + "t/00000000000000000000000000000000"},
	} {
		for _, r := range routes {
			for _, method := range []string{"GET", "POST"} {
				req, _ := http.NewRequest(method, c.prefix+r, nil)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("%s %s %s: %v", c.name, method, r, err)
				}
				resp.Body.Close()
				if resp.StatusCode != http.StatusNotFound {
					t.Fatalf("%s %s /%s: status = %d, want 404", c.name, method, r, resp.StatusCode)
				}
				if resp.Header.Get("Referrer-Policy") != "no-referrer" {
					t.Fatalf("%s %s /%s: missing Referrer-Policy: no-referrer", c.name, method, r)
				}
			}
		}
	}
	_ = withTok
}

// A rebound Host header (DNS rebinding) is rejected with 403 even with
// the correct token.
func TestHostAllowlist(t *testing.T) {
	s := startTestServer(t)
	for _, host := range []string{"evil.example.com", "127.0.0.1.evil.example.com", "0.0.0.0", ""} {
		req, _ := http.NewRequest("GET", s.URL(), nil)
		if host == "" {
			req.Host = "evil.example.com:9999" // port form of a non-allowlisted host
		} else {
			req.Host = host
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("host %q: %v", host, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("host %q: status = %d, want 403", host, resp.StatusCode)
		}
	}
	// Allowlisted loopback hosts pass.
	for _, host := range []string{"127.0.0.1", "localhost", "127.0.0.1:8090", "localhost:9999"} {
		req, _ := http.NewRequest("GET", s.URL(), nil)
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("host %q: %v", host, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("host %q: status = %d, want 200", host, resp.StatusCode)
		}
	}
}

// The tokenized URL itself never reaches log output: the http.Server
// ErrorLog sink redacts the /t/<token>/ prefix on every line.
func TestTokenNeverLogged(t *testing.T) {
	s := startTestServer(t)
	line := fmt.Sprintf("http: panic serving 127.0.0.1:1234: bad request at %sworkspaces", s.URL())
	if _, err := io.WriteString(s.errSink, line); err != nil {
		t.Fatalf("write log: %v", err)
	}
	got := s.errLog.String()
	if strings.Contains(got, s.token) {
		t.Fatalf("log leaked the token: %q", got)
	}
	if !strings.Contains(got, "/t/[REDACTED]/workspaces") {
		t.Fatalf("log did not redact to the [REDACTED] form: %q", got)
	}
}
