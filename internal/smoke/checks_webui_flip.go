package smoke

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// checkWebUIFlip asserts the parity gate's completion (spec §14,
// #151/#162/#163) against the freshly built binary: `--full-tui` is
// deleted along with the full-TUI code — the flag is rejected like any
// unknown flag, help names only the minimal launcher, and the default
// launch is still the launcher face (webserver at launch + the browser
// auto-open carrying the tokenized URL).
func checkWebUIFlip(home, root string) Item {
	p := newPhase("A18", "Parity gate closed: --full-tui deleted (rejected as unknown), help names only the minimal launcher, default launch = launcher face (webserver at launch + browser auto-open) (#163)")

	if abs, err := filepath.Abs(root); err == nil {
		root = abs // the PTY child and go build need an absolute cwd
	}
	bin := filepath.Join(home, "A18", "oc-sandbox")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		p.reasonf("work dir: %v", err)
		return p.finish()
	}

	// The whole tree still compiles after the full-TUI deletion (the
	// launcher face is the only bubbletea program left).
	if out, err := runIn(root, "go", "build", "./..."); err != nil {
		p.reasonf("go build ./...: %v — %s", err, out)
	}
	if out, err := runIn(root, "go", "build", "-o", bin, "./cmd/oc-sandbox"); err != nil {
		p.reasonf("go build binary: %v — %s", err, out)
		return p.finish()
	}

	// Help names the launcher as the default and never mentions the
	// deleted flag.
	help, err := runIn(root, bin, "--help")
	if err != nil {
		p.reasonf("--help: %v — %s", err, help)
	} else if strings.Contains(help, "full-tui") {
		p.reasonf("--help leaks the deleted --full-tui flag: %s", help)
	} else if !strings.Contains(help, "minimal launcher") {
		p.reasonf("--help does not name the launcher as the default: %s", help)
	}

	// The deleted flag is rejected as unknown (exit 2, abort code) —
	// the flag dies in arg parsing, before the launcher face or the
	// webserver can come up (asserted via the absence of the launcher's
	// "WebUI:" banner in the output).
	out, err := runIn(root, bin, "--full-tui")
	if err == nil {
		p.reasonf("--full-tui still accepted: out=%s", outTail(out))
	} else if !strings.Contains(out, "unknown flag: --full-tui") {
		p.reasonf("--full-tui rejection lacks the unknown-flag line: err=%v out=%s", err, outTail(out))
	} else if strings.Contains(out, "WebUI: http") {
		p.reasonf("--full-tui rejection ran past arg parsing: out=%s", outTail(out))
	}

	// Fake browser opener: the §3 xdg-open path, logging its argument.
	binDir := filepath.Join(home, "A18", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		p.reasonf("bin dir: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(binDir, "xdg-open"), []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" >> \"$BROWSER_LOG\"\n"), 0o755); err != nil {
		p.reasonf("fake xdg-open: %v", err)
		return p.finish()
	}

	// Default launch (no flags): the launcher face with the webserver
	// at launch and the auto-opened browser tab carrying the same
	// tokenized URL. Quit through the guarded exit (q, q, n).
	urlRe := regexp.MustCompile(`http://127\.0\.0\.1:\d+/t/[0-9a-f]{32}/`)
	{
		h1 := filepath.Join(home, "A18", "home-launcher")
		blog := filepath.Join(home, "A18", "browser.log")
		if err := os.MkdirAll(h1, 0o755); err != nil {
			p.reasonf("launcher home: %v", err)
			return p.finish()
		}
		out, err := runPTY(root, "q|q|n", h1, binDir, blog, bin)
		if err != nil || strings.Contains(out, "timed out") {
			p.reasonf("default launch: err=%v out=%s", err, outTail(out))
		} else {
			m := urlRe.FindString(out)
			if m == "" {
				p.reasonf("default launch printed no tokenized WebUI URL: %s", outTail(out))
			} else if bdata, rerr := os.ReadFile(blog); rerr != nil || strings.TrimSpace(string(bdata)) != m {
				p.reasonf("browser auto-open: log %q, want the launched URL %q (err %v)", bdata, m, rerr)
			}
		}
	}
	return p.finish()
}

// runPTY drives one launch under tests/run-pty.py (deterministic PTY:
// paced key chunks, fixed window) and returns the captured output.
func runPTY(root, input, homeDir, binDir, browserLog, bin string, args ...string) (string, error) {
	argv := append([]string{
		filepath.Join(root, "tests", "run-pty.py"),
		"--input", input, "--timeout", "20", "--",
		"env",
		"HOME=" + homeDir,
		"PATH=" + binDir + ":" + os.Getenv("PATH"),
		"BROWSER_LOG=" + browserLog,
		bin,
	}, args...)
	return runIn(root, "python3", argv...)
}

// outTail renders the captured output's last chunk (ANSI-laden TUI
// captures stay report-readable).
func outTail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 400 {
		s = s[len(s)-400:]
	}
	return s
}

// runIn runs a command inside the repo tree (its own module/toolchain
// context) and returns the combined output.
func runIn(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
