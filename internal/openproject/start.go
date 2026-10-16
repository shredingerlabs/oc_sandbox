package openproject

import (
	"strings"

	"github.com/mattn/go-runewidth"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// startOpts are the hardcoded TUI choices (bash start-tui.sh:777, 1344).
var (
	startOptions = []string{"console", "opencode", "web"}
)

// StartArgs rebuilds the start.sh argument vector from stored fields
// (data-map §3, bash start_container): the project root first, then
// --edition, --container-id, one --<mode> flag per stored mode,
// --use_proxy, --start_web for the web start option (the TUI always
// starts detached, so --start_opencode and the foreground --start_web
// env path never fire), and --detach last.
func StartArgs(projectPath, edition, containerID string, modes []string, startOption string, useProxy bool) []string {
	args := []string{projectPath, "--edition", edition, "--container-id", containerID}
	for _, mode := range modes {
		args = append(args, "--"+mode)
	}
	if useProxy {
		args = append(args, "--use_proxy")
	}
	if startOption == "web" {
		args = append(args, "--start_web")
	}
	return append(args, "--detach")
}

// ValidModes rejects the mutually exclusive offline + cbm_ui combination
// (bash validate_container_modes; start.sh exits on it).
func ValidModes(modes []string) bool {
	hasOffline, hasCBM := false, false
	for _, m := range modes {
		switch m {
		case "offline":
			hasOffline = true
		case "cbm_ui":
			hasCBM = true
		}
	}
	return !(hasOffline && hasCBM)
}

// modeLabel renders a mode option row label with its selection symbol.
func modeLabel(mode string, selected bool) string {
	if selected {
		return shell.SymOn + " " + mode
	}
	return shell.SymOff + " " + mode
}

// hasMode reports whether modes contains mode.
func hasMode(modes []string, mode string) bool {
	for _, m := range modes {
		if m == mode {
			return true
		}
	}
	return false
}

// withoutMode returns modes minus the given mode (newproject parity).
func withoutMode(modes []string, mode string) []string {
	var out []string
	for _, m := range modes {
		if m != mode {
			out = append(out, m)
		}
	}
	return out
}

// truncate cuts s to at most n cells (display width, ANSI- and
// wide-rune-aware), appending an ellipsis when cut.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if shell.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	// Cut rune by rune, tracking cell width, so wide runes never
	// overflow the budget. Reserve one cell for the ellipsis.
	rs := []rune(s)
	w := 0
	for i, r := range rs {
		rw := runewidth.RuneWidth(r)
		if w+rw > n-1 {
			return string(rs[:i]) + "…"
		}
		w += rw
	}
	return s
}

// divider renders a ────── rule n cells wide.
func divider(n int) string {
	if n < 1 {
		return ""
	}
	return strings.Repeat("─", n)
}
