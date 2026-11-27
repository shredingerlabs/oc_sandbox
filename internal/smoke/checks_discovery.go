package smoke

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"syscall"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
)

// A2 — toolchain-set + mode discovery (issue #73 A2, re-keyed at the
// #215 cutover): the shipped toolchains tree runs through the Go
// discovery package (bundles + units + live lint) and start.sh --help
// still feeds the mode discovery.
func checkDiscoveryParity(home, root string) Item {
	p := newPhase("A2", "Toolchain-set (toolchains tree) and mode (start.sh --help) discovery")
	fx := tuiFixtureTree(p, home, root)
	if p.reasons != nil {
		return p.finish()
	}

	// The shipped toolchains tree seeded into the smoke home discovers
	// the manifest bundles and every shipped unit valid.
	cat := discovery.Discover(filepath.Join(home, ".oc-sandbox", "toolchains"))
	if len(cat.Bundles) == 0 {
		p.reasonf("toolchains discovery found no bundles in the shipped manifest")
	}
	if _, ok := cat.Unit("go"); !ok {
		p.reasonf("toolchains discovery lost the shipped go recipe")
	}

	wantModes := []string{"offline", "cbm_ui", "hil_mode"}
	gotModes, err := discovery.Modes(cmdrun.ExecRunner{}, filepath.Join(fx, "scripts", "start.sh"))
	if err != nil {
		p.reasonf("Go Modes: %v", err)
	} else if !reflect.DeepEqual(sortedCopy(gotModes), sortedCopy(wantModes)) {
		p.reasonf("Go modes = %v, want the set %v (live start.sh --help, order as printed)", gotModes, wantModes)
	}

	// Error path: a start.sh that prints no flags → discovery must fail.
	empty := filepath.Join(home, "A2-empty")
	if err := os.MkdirAll(filepath.Join(empty, "scripts"), 0o755); err != nil {
		p.reasonf("empty fixture: %v", err)
		return p.finish()
	}
	if err := os.WriteFile(filepath.Join(empty, "scripts", "start.sh"), []byte("#!/usr/bin/env bash\necho no flags here\n"), 0o755); err != nil {
		p.reasonf("empty fixture write: %v", err)
		return p.finish()
	}
	if _, err := discovery.Modes(cmdrun.ExecRunner{}, filepath.Join(empty, "scripts", "start.sh")); err == nil {
		p.reasonf("Go Modes accepted a start.sh without flags")
	}
	return p.finish()
}

// tuiFixtureTree mirrors dist/scripts/start.sh into a fixture tree so
// the mode discovery's SCRIPT_DIR-relative lookups resolve inside the
// tree.
func tuiFixtureTree(p *phase, home, root string) string {
	fx := filepath.Join(home, "A2")
	if err := os.MkdirAll(filepath.Join(fx, "scripts"), 0o755); err != nil {
		p.reasonf("fixture tree: %v", err)
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, "dist", "scripts", "start.sh"))
	if err != nil {
		p.reasonf("fixture copy dist/scripts/start.sh: %v", err)
		return fx
	}
	if err := os.WriteFile(filepath.Join(fx, "scripts", "start.sh"), data, 0o755); err != nil {
		p.reasonf("fixture write start.sh: %v", err)
	}
	return fx
}

// A5 — exit-code policy at the seams where the launcher delegates to
// scripts (issue #73 A5): 0 clean, 1 general error, 2 user abort, 130
// SIGINT — the seam contract the launcher's exit mapping keeps
// (story 28: tea.ErrInterrupted → 130, ExitStatus passthrough).
func checkExitCodeSeams(home, root string) Item {
	p := newPhase("A5", "Exit codes 0/1/2/130 at the script seams (the launcher's exit mapping keeps the policy)")
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
	return p.finish()
}

// sortedCopy returns a sorted copy of s (set comparison helper).
func sortedCopy(s []string) []string {
	c := append([]string(nil), s...)
	slices.Sort(c)
	return c
}

// exitCodeOf maps a delegated-command error onto the exit-code policy
// (0 clean, 1 general, 2 abort, 130 for SIGINT-terminated children,
// 128+n otherwise) — the seam contract the launcher's exit mapping keeps.
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
