package main

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The WebUI server starts at launch (#150): a tokenized loopback URL,
// healthz token-less — wired into runTUI alongside the TUI.
func TestStartWebserver(t *testing.T) {
	s := startWebserver("test", t.TempDir())
	if s == nil {
		t.Fatal("startWebserver returned nil")
	}
	t.Cleanup(func() { _ = s.Close() })

	urlRe := regexp.MustCompile(`^http://127\.0\.0\.1:\d+/t/[0-9a-f]{32}/$`)
	if !urlRe.MatchString(s.URL()) {
		t.Fatalf("URL = %q, want tokenized loopback form", s.URL())
	}
	healthz := strings.Replace(s.URL(), "/t/"+s.Token()+"/", "/healthz", 1)
	resp, err := http.Get(healthz)
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", resp.StatusCode)
	}
}
