// Package opener builds the §3 browser-open command: xdg-open on
// Linux, macOS open, Windows cmd start. WSL-aware, because a Go binary
// running inside WSL reports GOOS=linux: there the opener probes the
// WSL-native wslview (wslu) first, then the cmd.exe interop start, and
// only falls back to the plain-Linux xdg-open (WSLg) when no interop
// binary is offered — a failed open surfaces the copy fallback.
package opener

import (
	"os/exec"
	"runtime"

	"github.com/shredingerlabs/oc-sandbox/internal/wsl"
)

// Cmd returns the platform browser command for url. On WSL the interop
// openers win over xdg-open: xdg-open is absent without xdg-utils and
// opens nothing without WSLg, while the interop binaries reach the
// Windows default browser (the URL stays clickable in the terminal
// either way — the opener just removes the manual step).
func Cmd(url string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url)
	default: // linux — plain or WSL
		if wsl.IsWSL() {
			if _, err := exec.LookPath("wslview"); err == nil {
				return exec.Command("wslview", url)
			}
			if _, err := exec.LookPath("cmd.exe"); err == nil {
				return exec.Command("cmd.exe", "/c", "start", "", url)
			}
		}
		return exec.Command("xdg-open", url)
	}
}

// Browser opens url in the platform browser — the launcher's Open
// seam: Start is the failure signal, the detached Wait reaps.
func Browser(url string) error {
	cmd := Cmd(url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // no zombies across repeated opens
	return nil
}
