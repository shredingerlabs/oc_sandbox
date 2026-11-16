package openworkspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/wsl"
)

// wslPairLinkFixture fakes a WSL host for the pair-link opener: the
// detection seam reads a fixture /proc/version and PATH is a stub dir
// holding the named stubs.
func wslPairLinkFixture(t *testing.T, stubs map[string]string) {
	t.Helper()
	wsl.VersionPath = filepath.Join(t.TempDir(), "proc-version")
	t.Cleanup(func() { wsl.VersionPath = "/proc/version" })
	if err := os.WriteFile(wsl.VersionPath, []byte("Linux version 5.15.167.4-microsoft-standard-WSL2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	for name, body := range stubs {
		p := filepath.Join(bin, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
}

// WSL + wslview: the pair link opens through wslview, not xdg-open
// (the Login WEB button's "Could not open a browser" false alarm on
// WSL hosts).
func TestOpenBrowserWSLUsesWslview(t *testing.T) {
	wslPairLinkFixture(t, map[string]string{
		"wslview": "#!/bin/sh\ntrue\n",
	})
	cmd := openBrowser("http://127.0.0.1:4123/pair?tkt=x")
	if cmd.Args[0] != "wslview" {
		t.Fatalf("opener argv[0] = %q, want wslview under WSL", cmd.Args[0])
	}
	if cmd.Args[1] != "http://127.0.0.1:4123/pair?tkt=x" {
		t.Fatalf("opener argv[1] = %q, want the pair link", cmd.Args[1])
	}
}

// WSL without wslu: the cmd.exe interop start carries the pair link.
func TestOpenBrowserWSLUsesCmdStart(t *testing.T) {
	wslPairLinkFixture(t, map[string]string{
		"cmd.exe": "#!/bin/sh\ntrue\n",
	})
	cmd := openBrowser("http://127.0.0.1:4123/pair?tkt=x")
	if cmd.Args[0] != "cmd.exe" || cmd.Args[1] != "/c" {
		t.Fatalf("opener argv = %v, want cmd.exe /c start under WSL", cmd.Args)
	}
	if cmd.Args[2] != "start" || cmd.Args[4] != "http://127.0.0.1:4123/pair?tkt=x" {
		t.Fatalf("opener argv = %v, want start with the pair link", cmd.Args)
	}
}

// WSL with interop disabled: the xdg-open attempt stays (WSLg covers
// it when present) — the Start failure surfaces the copy fallback.
func TestOpenBrowserWSLNoInteropKeepsXdgOpen(t *testing.T) {
	wslPairLinkFixture(t, nil)
	cmd := openBrowser("http://127.0.0.1:4123/pair?tkt=x")
	if cmd.Args[0] != "xdg-open" {
		t.Fatalf("opener argv[0] = %q, want the xdg-open fallback", cmd.Args[0])
	}
}
