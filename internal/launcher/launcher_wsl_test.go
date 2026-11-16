package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/wsl"
)

// wslFixture fakes a WSL host: the detection seam reads a fixture
// /proc/version and PATH is a stub dir; the optional stubs log their
// argv to logPath through $WSL_LOG.
func wslFixture(t *testing.T, stubs map[string]string) (logPath string) {
	t.Helper()
	wsl.VersionPath = filepath.Join(t.TempDir(), "proc-version")
	t.Cleanup(func() { wsl.VersionPath = "/proc/version" })
	if err := os.WriteFile(wsl.VersionPath, []byte("Linux version 5.15.167.4-microsoft-standard-WSL2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath = filepath.Join(t.TempDir(), "stub.log")
	bin := t.TempDir()
	for name, body := range stubs {
		p := filepath.Join(bin, name)
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	t.Setenv("WSL_LOG", logPath)
	return logPath
}

// stubLog waits for the detached stub child to append its argv —
// Start returns before the child runs, so the log lands asynchronously.
func stubLog(t *testing.T, logPath string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(logPath); err == nil && strings.TrimSpace(string(b)) != "" {
			return string(b)
		}
		time.Sleep(5 * time.Millisecond)
	}
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("stub log %s never appeared", logPath)
	}
	return string(b)
}

// WSL + wslview (wslu): the browser opener must run wslview — the
// plain-Linux xdg-open default fails on a WSL host without it (the
// "Could not open a browser" false alarm, issue: WSL browser auto-open).
func TestOpenBrowserWSLUsesWslview(t *testing.T) {
	logPath := wslFixture(t, map[string]string{
		"wslview": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$WSL_LOG\"\n",
	})
	if err := OpenBrowser(testURL); err != nil {
		t.Fatalf("OpenBrowser under WSL: %v — must open via wslview", err)
	}
	if got := stubLog(t, logPath); !strings.Contains(got, testURL) {
		t.Fatalf("wslview stub got %q, want the URL", got)
	}
}

// WSL without wslu: the cmd.exe interop start carries the URL to the
// Windows default browser.
func TestOpenBrowserWSLUsesCmdStart(t *testing.T) {
	logPath := wslFixture(t, map[string]string{
		"cmd.exe": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$WSL_LOG\"\n",
	})
	if err := OpenBrowser(testURL); err != nil {
		t.Fatalf("OpenBrowser under WSL: %v — must open via cmd.exe start", err)
	}
	got := stubLog(t, logPath)
	if !strings.Contains(got, "start") || !strings.Contains(got, testURL) {
		t.Fatalf("cmd.exe stub got %q, want start with the URL", got)
	}
}

// WSL with interop disabled (no wslview, no cmd.exe, no xdg-open): the
// open honestly fails — the face keeps the URL for manual copy.
func TestOpenBrowserWSLNoInteropFails(t *testing.T) {
	wslFixture(t, nil)
	if err := OpenBrowser(testURL); err == nil {
		t.Fatal("OpenBrowser without any opener must fail (URL stays for copy)")
	}
}

// Plain Linux (non-WSL kernel string): xdg-open stays the opener.
func TestOpenBrowserPlainLinuxKeepsXdgOpen(t *testing.T) {
	logPath := wslFixture(t, map[string]string{
		"xdg-open": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$WSL_LOG\"\n",
	})
	// The fixture pins a WSL kernel string; re-point the seam at a
	// plain-Linux one.
	if err := os.WriteFile(wsl.VersionPath, []byte("Linux version 6.8.0-tuxedo #42 SMP\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := OpenBrowser(testURL); err != nil {
		t.Fatalf("OpenBrowser on plain Linux: %v — must open via xdg-open", err)
	}
	if got := stubLog(t, logPath); !strings.Contains(got, testURL) {
		t.Fatalf("xdg-open stub got %q, want the URL", got)
	}
}
