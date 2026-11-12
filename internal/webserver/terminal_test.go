package webserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// terminalArgv mirrors the per-terminal argv shapes (§3): every family
// runs `bash <script>` — the shapes differ in where program+args go.
func TestTerminalArgvShapes(t *testing.T) {
	for _, c := range []struct {
		term string
		want []string
	}{
		{"gnome-terminal", []string{"--", "bash", "/tmp/x.sh"}},
		{"kitty", []string{"bash", "/tmp/x.sh"}},
		{"foot", []string{"bash", "/tmp/x.sh"}},
		{"xfce4-terminal", []string{"-x", "bash", "/tmp/x.sh"}},
		{"mate-terminal", []string{"-e", "bash '/tmp/x.sh'"}},
		{"xterm", []string{"-e", "bash", "/tmp/x.sh"}},
		{"konsole", []string{"-e", "bash", "/tmp/x.sh"}},
		{"x-terminal-emulator", []string{"-e", "bash", "/tmp/x.sh"}},
		{"weird-term", []string{"-e", "bash", "/tmp/x.sh"}}, // unknown → xterm -e fallback
	} {
		got := terminalArgv(c.term, "/tmp/x.sh")
		if !equalArgs(got, c.want) {
			t.Fatalf("%s argv = %v, want %v", c.term, got, c.want)
		}
	}
}

// spawnLinux probes the §3 list via LookPath and launches the first hit
// detached — asserted here with a fake x-terminal-emulator on PATH that
// records its argv and then REALLY executes it (the xterm -e
// convention). The spawned command carries shell syntax (quotes, `;`,
// redirect, `$?` — the #161 setup wrapper shape), which the -e family
// never interprets: only the bash-script relay makes it run.
func TestSpawnLinuxProbeRunsCompoundCommand(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux probe list")
	}
	bin := t.TempDir()
	rec := filepath.Join(t.TempDir(), "argv.log")
	marker := filepath.Join(t.TempDir(), "exit")
	// Record the argv, strip the -e flag and exec the rest like xterm.
	fake := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + rec + "\n" +
		"[ \"$1\" = -e ] && shift\nexec \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "x-terminal-emulator"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	// The fake dir joins the PATH (not replaces it): the fake execs
	// `bash`, which must stay resolvable like on a real host.
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OC_SANDBOX_TERMINAL", "")

	command := `true; printf '%s' $? > ` + marker
	if err := spawnTerminal(command); err != nil {
		t.Fatalf("spawnTerminal: %v", err)
	}
	waitFor(t, "fake terminal run", func() bool {
		_, err := os.ReadFile(rec)
		return err == nil
	})
	argv, _ := os.ReadFile(rec)
	got := strings.Split(strings.TrimSpace(string(argv)), "\n")
	if len(got) != 3 || got[0] != "-e" || got[1] != "bash" || !strings.HasSuffix(got[2], ".sh") {
		t.Fatalf("terminal argv = %v, want [-e bash <script>]", got)
	}
	script, err := os.ReadFile(got[2])
	if err != nil {
		t.Fatalf("script read: %v", err)
	}
	if !strings.Contains(string(script), command) {
		t.Fatalf("script %q loses the command %q", script, command)
	}
	// The compound command ran through the shell: the relay fired.
	waitFor(t, "status-file relay", func() bool {
		b, err := os.ReadFile(marker)
		return err == nil && strings.TrimSpace(string(b)) == "0"
	})
}

// The configurable override wins over the probe list (§3); an unknown
// terminal name falls back to the xterm -e convention.
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
	// The fake dir joins the PATH (not replaces it): the fake execs
	// `bash`, which must stay resolvable like on a real host.
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OC_SANDBOX_TERMINAL", filepath.Join(bin, "my-term"))

	if err := spawnTerminal("podman exec -it ctr bash"); err != nil {
		t.Fatalf("override spawn: %v", err)
	}
	waitFor(t, "override terminal run", func() bool {
		_, err := os.ReadFile(rec)
		return err == nil
	})
	argv, _ := os.ReadFile(rec)
	got := strings.Split(strings.TrimSpace(string(argv)), "\n")
	if len(got) != 3 || got[0] != "-e" || got[1] != "bash" {
		t.Fatalf("override argv = %v, want [-e bash <script>]", got)
	}
	script, err := os.ReadFile(got[2])
	if err != nil {
		t.Fatalf("script read: %v", err)
	}
	if !strings.Contains(string(script), "podman exec -it ctr bash") {
		t.Fatalf("script %q loses the command", script)
	}
}

// Symlink resolution dispatches the argv family: an x-terminal-emulator
// alternative pointing at xfce4-terminal gets the -x shape (its -e
// would parse a single string), not the xterm -e convention.
func TestSpawnLinuxResolvesAlternative(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux probe list")
	}
	bin := t.TempDir()
	rec := filepath.Join(t.TempDir(), "argv.log")
	fake := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + rec + "\n"
	if err := os.WriteFile(filepath.Join(bin, "xfce4-terminal"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(bin, "xfce4-terminal"), filepath.Join(bin, "x-terminal-emulator")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("OC_SANDBOX_TERMINAL", "")

	if err := spawnTerminal("true"); err != nil {
		t.Fatalf("spawnTerminal: %v", err)
	}
	waitFor(t, "alternative terminal run", func() bool {
		_, err := os.ReadFile(rec)
		return err == nil
	})
	argv, _ := os.ReadFile(rec)
	got := strings.Split(strings.TrimSpace(string(argv)), "\n")
	if len(got) != 3 || got[0] != "-x" || got[1] != "bash" {
		t.Fatalf("alternative argv = %v, want [-x bash <script>]", got)
	}
}

// No terminal at all (SSH/headless) → the decisive no-window error, and
// no script file is written for a window that will never open.
func TestSpawnLinuxHeadless(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux probe list")
	}
	tmpEntries := func() map[string]bool {
		entries, err := os.ReadDir(os.TempDir())
		if err != nil {
			t.Fatalf("read temp: %v", err)
		}
		set := map[string]bool{}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "oc-sandbox-term-") {
				set[e.Name()] = true
			}
		}
		return set
	}
	before := tmpEntries()
	t.Setenv("PATH", t.TempDir()) // no terminals offered
	t.Setenv("OC_SANDBOX_TERMINAL", "")
	err := spawnTerminal("podman exec -it ctr bash")
	if err == nil {
		t.Fatal("headless spawn must fail with the no-window signal")
	}
	if !strings.Contains(err.Error(), "no terminal") {
		t.Fatalf("error = %v, want the no-terminal signal", err)
	}
	for name := range tmpEntries() {
		if !before[name] {
			t.Fatalf("headless spawn leaked %s", name)
		}
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
