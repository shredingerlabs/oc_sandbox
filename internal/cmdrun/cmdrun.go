// Package cmdrun provides the injected command-runner seam used by the
// data/config layer so tests need no podman or other host tools.
package cmdrun

import (
	"bytes"
	"os/exec"
	"strings"
	"sync"
)

// Runner runs an external command and returns its combined output.
type Runner interface {
	Output(name string, args ...string) (string, error)
}

// ExecRunner runs real commands via os/exec.
type ExecRunner struct{}

// Output returns combined stdout+stderr of the command.
func (ExecRunner) Output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}

// Fake is a scriptable Runner for tests: every invocation is recorded in
// Calls and answered by the Script function. It is goroutine-safe: views
// query the runner both on the event loop and from background cmds.
type Fake struct {
	Calls  []Call
	Script func(name string, args []string) (string, error)

	mu sync.Mutex
}

// Call records one invocation.
type Call struct {
	Name string
	Args []string
}

// Output implements Runner.
func (f *Fake) Output(name string, args ...string) (string, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, Call{Name: name, Args: append([]string(nil), args...)})
	script := f.Script // read under the lock, call outside
	f.mu.Unlock()
	if script == nil {
		return "", nil
	}
	return script(name, append([]string(nil), args...))
}

// Lines splits runner output into non-empty trimmed lines.
func Lines(out string) []string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}
