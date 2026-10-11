package smoke

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
)

// A2 — edition + mode discovery parity vs live bash output (issue #73 A2):
// the same Dockerfile/start.sh --help fixtures are fed through the bash
// TUI's detect_available_editions and the Go discovery package.
func checkDiscoveryParity(home, root string) Item {
	p := newPhase("A2", "Edition (Dockerfile stages) and mode (start.sh --help) discovery match the bash TUI")
	fx := tuiFixtureTree(p, home, root)
	if p.reasons != nil {
		return p.finish()
	}
	dockerfileCustom := filepath.Join(fx, "Dockerfile.custom")

	wantEditions := []string{"base", "swdev", "ui_extra"}
	gotEditions, err := discovery.Editions(filepath.Join(fx, "Dockerfile"), dockerfileCustom)
	if err != nil {
		p.reasonf("Go Editions: %v", err)
	} else if !reflect.DeepEqual(gotEditions, wantEditions) {
		p.reasonf("Go editions = %v, want %v", gotEditions, wantEditions)
	}

	out, err := runBash(fx, filepath.Join(fx, "scripts", "start-tui.sh"), `
detect_available_editions || { echo DETECTFAIL; exit 0; }
echo "EDITIONS ${AVAILABLE_EDITIONS[*]}"
echo "MODES ${AVAILABLE_MODES[*]}"
`)
	if err != nil {
		p.reasonf("bash detect run: %v out=%s", err, out)
		return p.finish()
	}
	var bashEditions, bashModes []string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "EDITIONS "):
			bashEditions = strings.Fields(strings.TrimPrefix(line, "EDITIONS "))
		case strings.HasPrefix(line, "MODES "):
			bashModes = strings.Fields(strings.TrimPrefix(line, "MODES "))
		}
	}
	if !reflect.DeepEqual(bashEditions, wantEditions) {
		p.reasonf("bash editions = %v, want %v", bashEditions, wantEditions)
	}
	if !reflect.DeepEqual(gotEditions, bashEditions) {
		p.reasonf("edition parity broken: Go=%v bash=%v", gotEditions, bashEditions)
	}

	gotModes, err := discovery.Modes(cmdrun.ExecRunner{}, filepath.Join(fx, "scripts", "start.sh"))
	if err != nil {
		p.reasonf("Go Modes: %v", err)
	} else if !reflect.DeepEqual(gotModes, bashModes) {
		p.reasonf("mode parity broken: Go=%v bash=%v (live start.sh --help)", gotModes, bashModes)
	}

	// Error path: no stages → both must fail.
	empty := filepath.Join(home, "A2-empty")
	if err := os.MkdirAll(filepath.Join(empty, "scripts"), 0o755); err != nil {
		p.reasonf("empty fixture: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(empty, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		p.reasonf("empty fixture write: %v", err)
		return p.finish()
	}
	if b, err := os.ReadFile(filepath.Join(root, "dist", "scripts", "start-tui.sh")); err != nil {
		p.reasonf("empty fixture script copy: %v", err)
	} else if err := os.WriteFile(filepath.Join(empty, "scripts", "start-tui.sh"), b, 0o755); err != nil {
		p.reasonf("empty fixture script write: %v", err)
	}
	if _, err := discovery.Editions(filepath.Join(empty, "Dockerfile"), ""); err == nil {
		p.reasonf("Go Editions accepted a Dockerfile without sandbox stages")
	}
	out, err = runBash(empty, filepath.Join(empty, "scripts", "start-tui.sh"), `
detect_available_editions && echo DETECTOK || echo DETECTFAIL
`)
	if err != nil || !strings.Contains(out, "DETECTFAIL") {
		p.reasonf("bash detect should fail without stages: err=%v out=%s", err, out)
	}
	return p.finish()
}

// tuiFixtureTree mirrors dist/scripts/{start-tui.sh,start.sh} next to
// fixture Dockerfiles so the bash function's SCRIPT_DIR-relative lookups
// resolve inside the tree.
func tuiFixtureTree(p *phase, home, root string) string {
	fx := filepath.Join(home, "A2")
	if err := os.MkdirAll(filepath.Join(fx, "scripts"), 0o755); err != nil {
		p.reasonf("fixture tree: %v", err)
		return ""
	}
	dockerfile := `# fixture: base stage, valid stage, invalid name (skipped by both implementations)
FROM scratch AS opencode-sandbox-base
FROM scratch AS opencode-sandbox-swdev
FROM scratch AS opencode-sandbox-Bad.Upper
`
	custom := `# fixture: duplicate stage (skipped by both), custom stage appended last
FROM scratch AS opencode-sandbox-swdev
FROM scratch AS opencode-sandbox-ui_extra
`
	pairs := [][2]string{
		{"dist/Dockerfile", "Dockerfile"},
		{"dist/Dockerfile.custom", "Dockerfile.custom"},
		{"dist/scripts/start-tui.sh", "scripts/start-tui.sh"},
		{"dist/scripts/start.sh", "scripts/start.sh"},
	}
	contents := map[string]string{"Dockerfile": dockerfile, "Dockerfile.custom": custom}
	for _, pr := range pairs {
		data, ok := contents[pr[1]]
		if !ok {
			b, err := os.ReadFile(filepath.Join(root, pr[0]))
			if err != nil {
				p.reasonf("fixture copy %s: %v", pr[0], err)
				continue
			}
			data = string(b)
		}
		if err := os.WriteFile(filepath.Join(fx, pr[1]), []byte(data), 0o755); err != nil {
			p.reasonf("fixture write %s: %v", pr[1], err)
		}
	}
	return fx
}

// A5 — exit-code policy at the seams where the TUI delegates to scripts
// (issue #73 A5): 0 clean, 1 general error, 2 user abort, 130 SIGINT; and
// SIGINT discipline: tracked temp files cleaned up, configs intact.
func checkExitCodeSeams(home, root string) Item {
	p := newPhase("A5", "Exit codes 0/1/2/130 at the script delegation seams; SIGINT cleans temps, configs intact")
	r := cmdrun.ExecRunner{}
	cases := []struct {
		name string
		cmd  string
		want int
	}{
		{"clean exit", `exit 0`, 0},
		{"general error", `exit 1`, 1},
		{"user abort", `exit 2`, 2},
		{"SIGINT", `kill -INT $$`, 130},
	}
	for _, c := range cases {
		_, err := r.Output("sh", "-c", c.cmd)
		if got := exitCodeOf(err); got != c.want {
			p.reasonf("%s: got exit code %d, want %d (err=%v)", c.name, got, c.want, err)
		}
	}

	// Signal discipline of the bash TUI itself, observed from the Go seam:
	// the child sources start-tui.sh so the INT trap removes tracked temp
	// files and exits 130.
	sub := filepath.Join(home, "A5")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		p.reasonf("prepare: %v", err)
		return p.finish()
	}
	persisted := filepath.Join(sub, ".config", "oc-sandbox", "persisted.json")
	if err := os.MkdirAll(filepath.Dir(persisted), 0o755); err != nil {
		p.reasonf("prepare config dir: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(persisted, []byte("intact\n"), 0o644); err != nil {
		p.reasonf("seed config: %v", err)
		return p.finish()
	}
	out, err := runBash(sub, filepath.Join(root, "dist", "scripts", "start-tui.sh"), `
t=$(mktemp "$HOME/.config/oc-sandbox/tmpcheck.XXXXXX")
TUI_TEMP_FILES+=("$t")
kill -INT $$
echo UNREACHABLE
`)
	if got := exitCodeOf(err); got != 130 {
		p.reasonf("SIGINT via bash TUI: got exit %d, want 130 (out=%s err=%v)", got, out, err)
	} else if strings.Contains(out, "UNREACHABLE") {
		p.reasonf("SIGINT trap did not terminate the flow")
	}
	leftovers, err := tempLeftovers(sub)
	if err != nil {
		p.reasonf("temp scan: %v", err)
	} else if len(leftovers) > 0 {
		p.reasonf("temp leftovers after SIGINT: %v", leftovers)
	}
	if b, err := os.ReadFile(persisted); err != nil || string(b) != "intact\n" {
		p.reasonf("config not intact after SIGINT: %v", err)
	}
	return p.finish()
}

// exitCodeOf maps a delegated-command error onto the bash TUI exit-code
// policy (0 clean, 1 general, 2 abort, 130 for SIGINT-terminated children,
// 128+n otherwise) — the seam contract the Go TUI must keep.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code >= 0 {
			return code
		}
		if ws, ok := ee.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			if ws.Signal() == syscall.SIGINT {
				return 130
			}
			return 128 + int(ws.Signal())
		}
	}
	return 1
}
