// Package wsl detects a WSL host: /proc/version mentioning microsoft
// (case-insensitive — WSL1's kernel string carries "Microsoft", WSL2's
// "microsoft-standard-WSL2"), the same probe scripts/install.sh's
// is_wsl uses. Under WSL the Linux-GOOS binary should reach the
// Windows interop binaries on PATH (cmd.exe / wt.exe / wslview)
// instead of the plain-Linux defaults (xdg-open, GUI terminals).
package wsl

import (
	"os"
	"strings"
)

// VersionPath is the kernel-version file the detection reads — the seam
// tests point at a fixture (a container's /proc/version is not
// writable).
var VersionPath = "/proc/version"

// IsWSL reports whether this Linux userspace runs on a WSL kernel.
// A missing or unreadable /proc/version reads as not-WSL.
func IsWSL() bool {
	b, err := os.ReadFile(VersionPath)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(b)), "microsoft")
}
