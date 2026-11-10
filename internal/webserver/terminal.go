// Host terminal window spawn (spec §3): a real terminal window on the
// host opens the console/OpenCode access variants (and first-run skills
// setup). Linux: configurable override wins, else the probe list runs
// via exec.LookPath, launch detached. macOS: osascript `do script` on
// Terminal.app. Windows/WSL: wt.exe via cmd.exe interop with path
// translation. The single decisive fallback when no window can open
// (SSH, headless, none offered) renders the exact copyable command in
// the WebUI — the session stays pending; no in-browser terminal.
package webserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// linuxTerminals is the §3 probe list, in order.
var linuxTerminals = []string{
	"x-terminal-emulator", "kitty", "gnome-terminal", "konsole",
	"xfce4-terminal", "foot", "xterm",
}

// terminalSpawn is the seam the access endpoint calls; production uses
// spawnTerminal.
func (s *Server) terminalSpawn() func(string) error {
	if s.opts.TerminalSpawn != nil {
		return s.opts.TerminalSpawn
	}
	return spawnTerminal
}

// spawnTerminal launches a detached host terminal window running
// command; the error is the single decisive no-window signal.
func spawnTerminal(command string) error {
	switch runtime.GOOS {
	case "darwin":
		return spawnDarwin(command)
	case "windows":
		return spawnWindows(command)
	default:
		return spawnLinux(command)
	}
}

// terminalArgv builds the per-emulator argv for command — the -e
// conventions differ per terminal family.
func terminalArgv(term, command string) []string {
	switch term {
	case "gnome-terminal":
		// -e is removed in newer gnome-terminal; `--` takes the command.
		return []string{"--", "bash", "-c", command}
	case "kitty", "foot":
		// kitty/foot run their arguments as the command directly.
		return []string{"bash", "-c", command}
	default: // x-terminal-emulator, konsole, xfce4-terminal, xterm
		return []string{"-e", command}
	}
}

// spawnLinux probes the §3 list with LookPath and launches the first
// hit detached (the caller does not wait on the window).
func spawnLinux(command string) error {
	var cmd *exec.Cmd
	if override := os.Getenv("OC_SANDBOX_TERMINAL"); override != "" {
		cmd = exec.Command(override, terminalArgv("generic", command)...)
	} else {
		var term string
		for _, candidate := range linuxTerminals {
			if p, err := exec.LookPath(candidate); err == nil {
				term = p
				break
			}
		}
		if term == "" {
			return errNoTerminal
		}
		cmd = exec.Command(term, terminalArgv(filepath.Base(term), command)...)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // detached: reap the window process
	return nil
}

// spawnDarwin opens Terminal.app with osascript `do script`.
func spawnDarwin(command string) error {
	script := `tell application "Terminal" to do script "` + strings.ReplaceAll(command, `"`, `\"`) + `"`
	cmd := exec.Command("osascript", "-e", script)
	return cmd.Start()
}

// spawnWindows prefers wt.exe via cmd.exe interop (WSL path
// translation rides wslpath on the command side).
func spawnWindows(command string) error {
	cmd := exec.Command("cmd.exe", "/c", "wt.exe", "bash", "-c", command)
	return cmd.Start()
}

// errNoTerminal is the decisive no-window signal — the headless
// fallback renders the copyable command (§3).
var errNoTerminal = &terminalError{"no terminal emulator available"}

type terminalError struct{ msg string }

func (e *terminalError) Error() string { return e.msg }
