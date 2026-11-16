package wsl

import (
	"os"
	"path/filepath"
	"testing"
)

// wslVersionFile pins the detection to a fixture file and restores the
// real path after the test.
func wslVersionFile(t *testing.T, content string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "version")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := VersionPath
	VersionPath = p
	t.Cleanup(func() { VersionPath = old })
}

// WSL2's kernel string carries "microsoft-standard-WSL2".
func TestIsWSLWSL2(t *testing.T) {
	wslVersionFile(t, "Linux version 5.15.167.4-microsoft-standard-WSL2 (root@build) #1 SMP\n")
	if !IsWSL() {
		t.Error("IsWSL = false on a WSL2 kernel string")
	}
}

// WSL1's kernel string carries "Microsoft" with a capital M.
func TestIsWSLWSL1(t *testing.T) {
	wslVersionFile(t, "Linux version 4.4.0-19041-Microsoft (Microsoft@Microsoft.com) #1 SMP\n")
	if !IsWSL() {
		t.Error("IsWSL = false on a WSL1 kernel string")
	}
}

// A plain Linux kernel string (this CI's tuxedo kernel, a distro
// kernel, any non-WSL build) reads as not-WSL.
func TestIsWSLPlainLinux(t *testing.T) {
	wslVersionFile(t, "Linux version 6.8.0-tuxedo (gcc 13.3.0) #42 SMP\n")
	if IsWSL() {
		t.Error("IsWSL = true on a plain Linux kernel string")
	}
}

// A missing kernel-version file reads as not-WSL.
func TestIsWSLMissingFile(t *testing.T) {
	VersionPath = filepath.Join(t.TempDir(), "absent")
	t.Cleanup(func() { VersionPath = "/proc/version" })
	if IsWSL() {
		t.Error("IsWSL = true with a missing /proc/version")
	}
}
