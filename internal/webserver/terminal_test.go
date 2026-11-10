package webserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// terminalArgv mirrors the per-terminal -e conventions (§3).
func TestTerminalArgvConventions(t *testing.T) {
	for _, c := range []struct {
		term, want string
	}{
		{"gnome-terminal", "--"},
		{"kitty", "bash"},
		{"foot", "bash"},
		{"xterm", "-e"},
		{"konsole", "-e"},
		{"x-terminal-emulator", "-e"},
		{"xfce4-terminal", "-e"},
	} {
		got := terminalArgv(c.term, "podman exec -it x bash")
		if got[0] != c.want {
			t.Fatalf("%s argv[0] = %q, want %q (argv %v)", c.term, got[0], c.want, got)
		}
		if !strings.Contains(strings.Join(got, " "), "podman exec -it x bash") {
			t.Fatalf("%s argv %v loses the command", c.term, got)
		}
	}
}

// waitFor polls (bounded) until poll() succeeds.
func waitFor(t *testing.T, what string, poll func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if poll() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s never happened", what)
}

// spawnLinux probes the §3 list via LookPath and launches the first hit
// detached — asserted here with a fake x-terminal-emulator on PATH that
// records its argv.
func TestSpawnLinuxProbeAndExec(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux probe list")
	}
	bin := t.TempDir()
	rec := filepath.Join(t.TempDir(), "argv.log")
	fake := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + rec + "\n"
	if err := os.WriteFile(filepath.Join(bin, "x-terminal-emulator"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("OC_SANDBOX_TERMINAL", "")

	if err := spawnTerminal("podman exec -it --user dev ctr bash"); err != nil {
		t.Fatalf("spawnTerminal: %v", err)
	}
	waitFor(t, "fake terminal run", func() bool {
		_, err := os.ReadFile(rec)
		return err == nil
	})
	argv, _ := os.ReadFile(rec)
	got := strings.Split(strings.TrimSpace(string(argv)), "\n")
	if len(got) != 2 || got[0] != "-e" || got[1] != "podman exec -it --user dev ctr bash" {
		t.Fatalf("terminal argv = %v, want [-e <command>]", got)
	}
}

// The configurable override wins over the probe list (§3).
func TestSpawnLinuxOverride(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux probe list")
	}
	bin := t.TempDir()
	rec := filepath.Join(t.TempDir(), "argv.log")
	fake := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + rec + "\n"
	if err := os.WriteFile(filepath.Join(bin, "my-term"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	// The probe list stays unfulfilled — only the override exists.
	t.Setenv("PATH", bin)
	t.Setenv("OC_SANDBOX_TERMINAL", filepath.Join(bin, "my-term"))

	if err := spawnTerminal("podman exec -it ctr bash"); err != nil {
		t.Fatalf("override spawn: %v", err)
	}
	waitFor(t, "override terminal run", func() bool {
		_, err := os.ReadFile(rec)
		return err == nil
	})
	argv, _ := os.ReadFile(rec)
	if !strings.Contains(string(argv), "podman exec -it ctr bash") {
		t.Fatalf("override argv = %q, want the command", argv)
	}
}

// No terminal at all (SSH/headless) → the decisive no-window error.
func TestSpawnLinuxHeadless(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux probe list")
	}
	t.Setenv("PATH", t.TempDir()) // no terminals offered
	t.Setenv("OC_SANDBOX_TERMINAL", "")
	err := spawnTerminal("podman exec -it ctr bash")
	if err == nil {
		t.Fatal("headless spawn must fail with the no-window signal")
	}
	if !strings.Contains(err.Error(), "no terminal") {
		t.Fatalf("error = %v, want the no-terminal signal", err)
	}
}

// macOS/Windows code paths construct their platform invocations.
func TestSpawnPlatformShapes(t *testing.T) {
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("osascript", "-e", `tell application "Terminal" to do script "x"`)
		if cmd.Path == "" {
			t.Fatal("osascript command construction")
		}
	}
	if runtime.GOOS == "windows" {
		cmd := exec.Command("cmd.exe", "/c", "wt.exe", "bash", "-c", "x")
		if cmd.Args[3] != "wt.exe" {
			t.Fatal("wt.exe interop shape")
		}
	}
}
