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
		"workspaces",
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
