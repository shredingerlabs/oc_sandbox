// Host terminal window spawn (spec §3): a real terminal window on the
// host opens the console/OpenCode access variants (and first-run skills
// setup). Linux: configurable override wins, else the probe list runs
// via exec.LookPath, launch detached. The command is handed to the
// window as a temp bash script — no -e-family emulator runs its command
// through a shell (verified in-source: xfce4-terminal parses -e with
// g_shell_parse_argv, konsole/xterm exec the post--e argv directly, real
// gnome-terminal -e splits shell-style without shell semantics), so a
// compound command (quotes, `;`, `>`, `$?` — the #161 setup wrapper)
// loses its shell meaning: the operators ride as literal args and the
// in-container command errors out instantly. macOS: osascript
// `do script` on Terminal.app. Windows/WSL: wt.exe via cmd.exe interop
// with path translation. The single decisive fallback when no window
// can open (SSH, headless, none offered) renders the exact copyable
// command in the WebUI — the session stays pending; no in-browser
// terminal.
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

// terminalArgv builds the per-emulator argv for script (a temp .sh file
// holding the command — every family runs `bash script`, so no emulator
// ever has to interpret shell syntax): gnome-terminal takes the command
// after `--`; kitty/foot run their arguments directly; xfce4-terminal's
// -x takes the rest of the command line as the command argv (its -e
// would g_shell_parse_argv a single string); mate-terminal's -e parses
// a single shell-style string, which a bare `bash <script>` survives;
// the xterm/konsole family and unknowns exec everything after -e.
func terminalArgv(term, script string) []string {
	switch term {
	case "gnome-terminal":
		return []string{"--", "bash", script}
	case "kitty", "foot":
		return []string{"bash", script}
	case "xfce4-terminal":
		return []string{"-x", "bash", script}
	case "mate-terminal":
		return []string{"-e", "bash '" + script + "'"}
	default: // x-terminal-emulator, konsole, xterm, unknowns
		return []string{"-e", "bash", script}
	}
}

// resolvedEmulator maps a terminal binary to its argv family key:
// symlinks resolved (the Debian x-terminal-emulator alternative points
// at any of the known emulators — e.g. xfce4-terminal needs its own -x
// shape), basename used, unknown names fall back to the xterm -e
// convention.
func resolvedEmulator(bin string) string {
	if p, err := exec.LookPath(bin); err == nil {
		if resolved, rerr := filepath.EvalSymlinks(p); rerr == nil {
			p = resolved
		}
		bin = filepath.Base(p)
	}
	return bin
}

// writeTerminalScript writes command into a new temp .sh file and
// returns its path. The file is left behind — the window process may
// start later than the spawn returns, and temp is reclaimed by the OS
// (the same lifecycle as the #161 setup status-file relay).
func writeTerminalScript(command string) (string, error) {
	f, err := os.CreateTemp("", "oc-sandbox-term-*.sh")
	if err != nil {
		return "", err
	}
	if _, werr := f.WriteString("#!/bin/bash\n" + command + "\n"); werr != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", werr
	}
	if cerr := f.Close(); cerr != nil {
		_ = os.Remove(f.Name())
		return "", cerr
	}
	return f.Name(), nil
}

// spawnLinux probes the §3 list with LookPath and launches the first
// hit detached (the caller does not wait on the window), running the
// command through a temp bash script in the emulator's argv shape.
func spawnLinux(command string) error {
	var cmd *exec.Cmd
	if override := os.Getenv("OC_SANDBOX_TERMINAL"); override != "" {
		script, err := writeTerminalScript(command)
		if err != nil {
			return err
		}
		cmd = exec.Command(override, terminalArgv(resolvedEmulator(override), script)...)
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
		script, err := writeTerminalScript(command)
		if err != nil {
			return err
		}
		cmd = exec.Command(term, terminalArgv(resolvedEmulator(term), script)...)
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
