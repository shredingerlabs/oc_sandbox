package app_test

// Composed-TUI regression tests for the reported bugs (single click on
// Create Project did nothing, init failure toast was opaque, and
// podman-ps stalls froze the whole TUI). They drive the real composed
// shell headlessly through teatest with a scriptable podman and the
// real dist/ scripts of an installed-layout home.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	teatest "github.com/charmbracelet/x/exp/teatest"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// installHome builds a $HOME with the ~/.oc-sandbox install layout
// (scripts + templates + Dockerfiles copied from the repo's dist).
func installHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	root := filepath.Join(home, ".oc-sandbox")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"scripts", "templates"} {
		out, err := exec.Command("cp", "-r",
			filepath.Join(repoRoot(t), "dist", name),
			filepath.Join(root, name)).CombinedOutput()
		if err != nil {
			t.Fatalf("copy dist/%s: %v: %s", name, err, out)
		}
	}
	for _, name := range []string{"Dockerfile", "Dockerfile.custom"} {
		b, err := os.ReadFile(filepath.Join(repoRoot(t), "dist", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// podmanRunner answers podman queries in-process; everything else runs
// for real through cmdrun.ExecRunner (real bash, real dist scripts).
type podmanRunner struct {
	mu         sync.Mutex
	images     map[string]bool
	running    []string
	port       string
	calls      []string
	lines      []string
	delay      map[string]time.Duration // per-verb artificial delay (ps/run/...)
	block      string                   // verb that never returns; "" = none
	blockAfter int                      // block verb only past this many podman calls
	ncalls     int                      // podman call counter
}

func (r *podmanRunner) Output(name string, args ...string) (string, error) {
	r.mu.Lock()
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	r.mu.Unlock()
	if name == "podman" {
		r.mu.Lock()
		verb := args[0]
		d := r.delay[verb]
		r.ncalls++
		blk := r.block == verb && r.ncalls > r.blockAfter
		r.mu.Unlock()
		if blk {
			select {} // never returns: a stalled rootless podman (lock wait)
		}
		if d > 0 {
			time.Sleep(d)
		}
		switch {
		case args[0] == "build": // fake build: mark the -t target as built
			r.mu.Lock()
			for i, a := range args {
				if a == "-t" && i+1 < len(args) {
					r.images[args[i+1]] = true
				}
			}
			r.mu.Unlock()
			return "", nil
		case args[0] == "image" && args[1] == "exists":
			if r.images[args[2]] {
				return "", nil
			}
			return "", fmt.Errorf("no image")
		case args[0] == "ps":
			return strings.Join(r.running, "\n"), nil
		case args[0] == "port":
			if r.port != "" {
				return r.port + "\n", nil
			}
			return "", fmt.Errorf("no port published")
		}
		return "", nil
	}
	out, err := cmdrun.ExecRunner{}.Output(name, args...)
	r.mu.Lock()
	r.lines = append(r.lines, name+" exit="+fmt.Sprint(err != nil)+": "+lastLines(out, 3))
	r.mu.Unlock()
	return out, err
}

func lastLines(out string, n int) string {
	ls := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.Join(ls, " | ")
}

// Log returns the recorded podman/script calls.
func (r *podmanRunner) Log() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// LogEcho returns the recorded non-podman call outcomes.
func (r *podmanRunner) LogEcho() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}

// recorder tees all program output into one buffer for screen checks.
type recorder struct {
	mu  sync.Mutex
	buf []byte
}

func (rec *recorder) pipeFrom(r io.Reader) {
	go func() {
		for {
			p := make([]byte, 1<<16)
			n, err := r.Read(p)
			if n > 0 {
				rec.mu.Lock()
				rec.buf = append(rec.buf, p[:n]...)
				rec.mu.Unlock()
			}
			if err != nil {
				// an empty buffer reads EOF; keep polling until quit
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()
}

func (rec *recorder) contents() []byte {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]byte(nil), rec.buf...)
}

// newTUI composes the production app with the fake podman and starts it
// headlessly at 120x40.
func newTUI(t *testing.T, home string, run cmdrun.Runner) (*teatest.TestModel, *recorder) {
	t.Helper()
	a := app.Compose(home, "dev", run, time.Now)
	tm := teatest.NewTestModel(t, a.Model, teatest.WithInitialTermSize(120, 41))
	rec := &recorder{}
	rec.pipeFrom(tm.Output())
	return tm, rec
}

// defaultImages marks every dist edition as built.
func defaultImages() map[string]bool {
	m := map[string]bool{}
	for _, e := range strings.Fields("base web embedded swdev matlab ros2 writing") {
		m["opencode-sandbox-"+e] = true
	}
	return m
}

func waitScreen(t *testing.T, rec *recorder, ok bool, subs ...string) bool {
	t.Helper()
	_ = ok
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out := string(rec.contents())
		all := true
		for _, s := range subs {
			if !strings.Contains(out, s) {
				all = false
				break
			}
		}
		if all {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// waitScreenAny returns once any of the subs shows up in the stream.
func waitScreenAny(t *testing.T, rec *recorder, subs ...string) bool {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		out := string(rec.contents())
		for _, s := range subs {
			if strings.Contains(out, s) {
				return true
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func screenOf(t *testing.T, rec *recorder) string {
	t.Helper()
	return renderScreen(120, 41, string(rec.contents())).text()
}

// mousePress builds a left-press mouse message at 0-based screen coords.
func mousePress(x, y int) tea.MouseMsg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

// seedProject registers a project with its sandbox config without the
// create flow (a "project exists after restart" starting point).
func seedProject(t *testing.T, home, name string) string {
	t.Helper()
	root := filepath.Join(home, "oc-sandbox", name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	reg := &registry.Registry{Home: home, Now: time.Now}
	if err := reg.CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	if err := reg.AddProject(registry.AddRequest{Name: name, Path: root, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.Create(home, sandboxcfg.Path(root), "base", nil, "console", "none"); err != nil {
		t.Fatal(err)
	}
	return root
}

// openNewProjectView types the navigation key and waits for the wizard.
func openNewProjectView(t *testing.T, tm *teatest.TestModel, rec *recorder) {
	t.Helper()
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if !waitScreen(t, rec, false, "Project name") {
		t.Fatal("view 2 did not open the New Project wizard")
	}
}

func press(t *testing.T, tm *teatest.TestModel, r rune) tea.KeyMsg {
	t.Helper()
	_ = t
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// E2E: a single left click on the Create Project button activates the
// create flow (typed earlier only the focus moved and the flow required
// a double click).
func TestE2ECreateSingleClickActivates(t *testing.T) {
	home := installHome(t)
	run := &podmanRunner{images: defaultImages()}
	tm, rec := newTUI(t, home, run)
	defer tm.Quit()

	if !waitScreen(t, rec, false, "oc-sandbox") {
		t.Fatal("TUI never rendered the header")
	}
	openNewProjectView(t, tm, rec)
	for _, r := range "singleproj" {
		press(t, tm, r)
	}
	if !waitScreen(t, rec, false, "singleproj") {
		t.Fatal("name did not reach the form")
	}
	g := renderScreen(120, 41, string(rec.contents()))
	_, _, ok := g.find("│  Create Project  │")
	if !ok {
		t.Fatal("Create Project button not rendered:\n" + g.String())
	}
	btnR, btnC, _ := g.find("Create Project")
	tm.Send(mousePress(btnC+2, btnR)) // single click: focus + popup (ADR-0021)
	// Create confirmation popup: tab to Create, enter runs.
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	// The create chain runs real scripts. Either the registry entry
	// appears (success), or the chain reaches a settled failure screen —
	// both prove the click ran the flow.
	if !waitScreenAny(t, rec, "Creating project", "Retry", "Go back", "Project initialization failed") {
		t.Fatalf("single click did not run the create flow; screen:\n%s", screenOf(t, rec))
	}
	reg := &registry.Registry{Home: home, Now: time.Now}
	// The create-flow log pane announces "Creating project" the moment
	// the chain is queued — poll until the registry write lands.
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := reg.ByName("singleproj"); err == nil {
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("registry write missing; screen:\n%s", screenOf(t, rec))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// E2E: a stalled podman ps (rootless lock contention) must not freeze
// the TUI — historically the render path and beginStart queried podman
// synchronously and the whole UI stopped.
func TestE2EStartFlowKeepsUILiveWhenPodmanStalls(t *testing.T) {
	home := installHome(t)
	seedProject(t, home, "frozenproj")
	run := &podmanRunner{
		images:     map[string]bool{"opencode-sandbox-base": true},
		block:      "ps", // stall podman ps from the start
		blockAfter: 1,    // first poll still allowed; then frozen forever
	}
	tm, rec := newTUI(t, home, run)
	defer tm.Quit()

	if !waitScreen(t, rec, false, "frozenproj") {
		t.Fatalf("project card never rendered:\n%s", screenOf(t, rec))
	}
	g := renderScreen(120, 41, string(rec.contents()))
	_, c, ok := g.find("│ ○ frozenproj")
	if !ok {
		t.Fatal("card not rendered:\n" + g.String())
	}
	tm.Send(mousePress(c+6, 4))
	tm.Send(mousePress(c+6, 4)) // double click → start flow (reconcile + launch)
	time.Sleep(500 * time.Millisecond)

	// The stalled podman keeps the running-conversation in the air; the
	// UI must still be alive: switching the view repaints.
	before := len(rec.contents())
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if !waitScreen(t, rec, false, "Project name") {
		t.Fatalf("TUI froze while podman ps stalled (bytes %d → %d):\n%s",
			before, len(rec.contents()), renderScreen(120, 41, string(rec.contents())).String())
	}
}
