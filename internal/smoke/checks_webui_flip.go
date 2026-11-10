package smoke

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// checkWebUIFlip asserts the parity gate flip (spec §14, #151/#162)
// against the freshly built binary: `--full-tui` stays hidden from
// help; the default launch is the minimal launcher (webserver at
// launch + the browser auto-open carrying the tokenized URL);
// `--full-tui` reaches the full TUI without the auto-open; the
// full-TUI packages still compile (no deletion yet — the sweep rides
// #163).
func checkWebUIFlip(home, root string) Item {
	p := newPhase("A18", "Parity flip: --full-tui hidden from help, default launch = launcher face (webserver at launch + browser auto-open), --full-tui reaches the full TUI with no auto-open, full-TUI packages still compile (#151)")

	if abs, err := filepath.Abs(root); err == nil {
		root = abs // the PTY child and go build need an absolute cwd
	}
	bin := filepath.Join(home, "A18", "oc-sandbox")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		p.reasonf("work dir: %v", err)
		return p.finish()
	}

	// Full-TUI packages still compile (AC: no deletion yet — protoapp
	// and the shell/openproject/settings tree build with the WebUI).
	if out, err := runIn(root, "go", "build", "./..."); err != nil {
		p.reasonf("go build ./... (full-TUI compile): %v — %s", err, out)
	}
	if out, err := runIn(root, "go", "build", "-o", bin, "./cmd/oc-sandbox"); err != nil {
		p.reasonf("go build binary: %v — %s", err, out)
		return p.finish()
	}

	// Help hides the flag: the transitional release README documents it
	// (README sweep rides #163), help output must not.
	help, err := runIn(root, bin, "--help")
	if err != nil {
		p.reasonf("--help: %v — %s", err, help)
	} else if strings.Contains(help, "full-tui") {
		p.reasonf("--help leaks the hidden --full-tui flag: %s", help)
	} else if !strings.Contains(help, "minimal launcher") {
		p.reasonf("--help does not name the launcher as the default: %s", help)
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

	// --full-tui: the full BubbleTea TUI renders (nav bar + hints) and
	// quits on q — with NO browser auto-open (no launcher face).
	{
		h2 := filepath.Join(home, "A18", "home-tui")
		blog := filepath.Join(home, "A18", "tui-browser.log")
		if err := os.MkdirAll(h2, 0o755); err != nil {
			p.reasonf("tui home: %v", err)
			return p.finish()
		}
		out, err := runPTY(root, "q", h2, binDir, blog, bin, "--full-tui")
		if err != nil || strings.Contains(out, "timed out") {
			p.reasonf("--full-tui: err=%v out=%s", err, outTail(out))
		} else {
			for _, want := range []string{"1 open", "new workspace", "select down", "quit"} {
				if !strings.Contains(out, want) {
					p.reasonf("--full-tui missing the TUI marker %q: %s", want, outTail(out))
				}
			}
			if bdata, rerr := os.ReadFile(blog); rerr == nil && strings.TrimSpace(string(bdata)) != "" {
				p.reasonf("--full-tui must not auto-open the browser, log: %q", strings.TrimSpace(string(bdata)))
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
