package smoke

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// A2 — edition + mode discovery (issue #73 A2): the same Dockerfile/
// start.sh --help fixtures run through the Go discovery package; the
// bash-era fixture trees pin the shapes the bash TUI discovered.
func checkDiscoveryParity(home, root string) Item {
	p := newPhase("A2", "Edition (Dockerfile stages) and mode (start.sh --help) discovery on fixtures")
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

	wantModes := []string{"offline", "cbm_ui", "hil_mode"}
	gotModes, err := discovery.Modes(cmdrun.ExecRunner{}, filepath.Join(fx, "scripts", "start.sh"))
	if err != nil {
		p.reasonf("Go Modes: %v", err)
	} else if !reflect.DeepEqual(sortedCopy(gotModes), sortedCopy(wantModes)) {
		p.reasonf("Go modes = %v, want the set %v (live start.sh --help, order as printed)", gotModes, wantModes)
	}

	// Error path: no stages → discovery must fail.
	empty := filepath.Join(home, "A2-empty")
	if err := os.MkdirAll(filepath.Join(empty, "scripts"), 0o755); err != nil {
		p.reasonf("empty fixture: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(empty, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		p.reasonf("empty fixture write: %v", err)
		return p.finish()
	}
	if _, err := discovery.Editions(filepath.Join(empty, "Dockerfile"), ""); err == nil {
		p.reasonf("Go Editions accepted a Dockerfile without sandbox stages")
	}
	return p.finish()
}

// tuiFixtureTree mirrors dist/scripts/start.sh next to fixture Dockerfiles
// so the mode discovery's SCRIPT_DIR-relative lookups resolve inside the
// tree.
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
// (issue #73 A5): 0 clean, 1 general error, 2 user abort, 130 SIGINT —
// verified on plain children and in the Go TUI exit mapping.
func checkExitCodeSeams(home, root string) Item {
	p := newPhase("A5", "Exit codes 0/1/2/130 at the script seams AND in the Go TUI exit mapping")
	r := cmdrun.ExecRunner{}
	// Go TUI story-28 mapping (app.ExitCode over the shell model).
	if got := app.ExitCode(shell.New(passiveView{}), nil); got != 0 {
		p.reasonf("Go mapping clean exit: got %d, want 0", got)
	}
	if got := app.ExitCode(shell.New(passiveView{}), app.ErrUserAbort); got != 2 {
		p.reasonf("Go mapping user abort: got %d, want 2", got)
	}
	if got := app.ExitCode(shell.New(passiveView{}), errors.New("boom")); got != 1 {
		p.reasonf("Go mapping general error: got %d, want 1", got)
	}
	if m, cmd := shell.New(passiveView{}).Update(tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil {
		p.reasonf("Go shell: ctrl+c did not quit")
	} else if got := app.ExitCode(m.(shell.Model), nil); got != 130 {
		p.reasonf("Go mapping ctrl+c quit: got %d, want 130", got)
	}
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
	return p.finish()
}

// sortedCopy returns a sorted copy of s (set comparison helper).
func sortedCopy(s []string) []string {
	c := append([]string(nil), s...)
	slices.Sort(c)
	return c
}

// passiveView is the minimal CenterView shell.New needs in this check.
type passiveView struct{}

func (passiveView) Update(tea.Msg) tea.Cmd   { return nil }
func (passiveView) Body(int, int) string     { return "" }
func (passiveView) SubTabs() ([]string, int) { return nil, 0 }
func (passiveView) TextFocused() bool        { return false }

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
