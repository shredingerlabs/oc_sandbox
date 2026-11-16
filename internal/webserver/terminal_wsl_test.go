package webserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shredingerlabs/oc-sandbox/internal/wsl"
)

// wslTerminalFixture fakes a WSL host for the terminal spawner: the
// detection seam reads a fixture /proc/version and PATH is a stub dir;
// the stubs log their argv to logPath through $WSL_LOG.
func wslTerminalFixture(t *testing.T, stubs map[string]string) (logPath string) {
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
	t.Setenv("OC_SANDBOX_TERMINAL", "")
	return logPath
}

// WSL + Windows Terminal: the Console/OpenCode spawn goes through the
// cmd.exe → wt.exe interop and runs the command inside the WSL distro
// (wsl.exe --exec bash <script>) — the plain-Linux probe list finds no
// GUI terminal on a WSL host, leaving the Console/OpenCode buttons dead.
func TestSpawnTerminalWSLUsesWtInterop(t *testing.T) {
	logPath := wslTerminalFixture(t, map[string]string{
		"cmd.exe": "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$WSL_LOG\"\n",
		"wt.exe":  "#!/bin/sh\ntrue\n",
	})
	if err := spawnTerminal("podman exec -it --user dev ctr bash"); err != nil {
		t.Fatalf("spawnTerminal under WSL: %v — must open via wt.exe interop", err)
	}
	got := stubLog(t, logPath)
	for _, want := range []string{"wt.exe", "wsl.exe", "--exec", "bash"} {
		if !strings.Contains(got, want) {
			t.Fatalf("wt interop log %q missing %q", got, want)
		}
	}
	m := strings.Fields(got)
	script := m[len(m)-1]
	b, err := os.ReadFile(script)
	if err != nil {
		t.Fatalf("read spawned script %q: %v", script, err)
	}
	if !strings.Contains(string(b), "podman exec -it --user dev ctr bash") {
		t.Fatalf("script %q lost the command: %q", script, b)
	}
}

// WSL without wt.exe (Windows 10 without Windows Terminal): no interop
// window opens — the decisive no-window signal falls back to the
// copyable command.
func TestSpawnTerminalWSLNoWtFallsBack(t *testing.T) {
	wslTerminalFixture(t, map[string]string{
		"cmd.exe": "#!/bin/sh\ntrue\n",
	})
	err := spawnTerminal("podman exec -it ctr bash")
	if err == nil || !strings.Contains(err.Error(), "no terminal") {
		t.Fatalf("spawnTerminal without wt.exe = %v, want the no-terminal signal", err)
	}
}

// WSL with interop disabled (no cmd.exe at all): same decisive signal.
func TestSpawnTerminalWSLNoInteropFallsBack(t *testing.T) {
	wslTerminalFixture(t, nil)
	err := spawnTerminal("podman exec -it ctr bash")
	if err == nil || !strings.Contains(err.Error(), "no terminal") {
		t.Fatalf("spawnTerminal without interop = %v, want the no-terminal signal", err)
	}
}

// stubLog waits for the detached stub child to append its argv.
func stubLog(t *testing.T, logPath string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(logPath); err == nil && strings.TrimSpace(string(b)) != "" {
			return string(b)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("stub log %s never appeared", logPath)
	return ""
}
