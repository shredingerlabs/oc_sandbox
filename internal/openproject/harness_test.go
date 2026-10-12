package openproject

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Test harness: fake cmdrun runner (no podman), two registered projects
// with written sandbox_config.json files, a Dockerfile fixture, and a
// recording Launcher that completes every op immediately.

const startHelpOut = "Usage: start.sh [project]\n  --skills  enable skills mode\n  --offline offline mode\n--use_proxy --detach --container-id"

type runnerHooks struct {
	helpOut  string // `bash start.sh --help` output ("" = startHelpOut)
	psOut    string // `podman ps` output
	imageErr error  // `podman image exists` error (nil = image exists)
	portErr  error  // `podman port` error
	startErr error  // start.sh error
	buildErr error
	onStart  func(args []string) // fires right before start.sh "runs"
	onBuild  func(args []string)
}

func (h *runnerHooks) output(name string, args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	switch {
	case name == "bash" && filepath.Base(args[0]) == "start.sh":
		if h.onStart != nil {
			h.onStart(args[1:])
		}
		return "starting...", h.startErr
	case name == "bash" && filepath.Base(args[0]) == "build-container.sh":
		if h.onBuild != nil {
			h.onBuild(args[1:])
		}
		return "built", h.buildErr
	case name == "podman" && args[0] == "ps":
		return h.psOut, nil
	case name == "podman" && args[0] == "image":
		return "", h.imageErr
	case name == "podman" && args[0] == "port":
		return "127.0.0.1:4096", h.portErr
	}
	return "", nil
}

func (h *runnerHooks) help(args []string) (string, error) {
	out := h.helpOut
	if out == "" {
		out = startHelpOut
	}
	return out, nil
}

type launchRecord struct {
	ops []Op
}

func (l *launchRecord) Launch(op Op) tea.Cmd {
	l.ops = append(l.ops, op)
	// Execute the op like DefaultLauncher, recording the completion.
	return func() tea.Msg {
		var lines []string
		err := op.Run(func(line string) { lines = append(lines, line) })
		return OpDoneMsg{ID: op.ID, Lines: lines, Err: err}
	}
}

type env struct {
	home    string
	run     *cmdrun.Fake
	hooks   *runnerHooks
	launch  *launchRecord
	m       *Model
	roots   map[string]string
	ids     map[string]string
	lastCmd tea.Cmd
	mgr     *taskmgr.Manager
	base    time.Time
	seq     int
}

func newEnv(t *testing.T, hooks *runnerHooks) *env {
	t.Helper()
	if hooks == nil {
		hooks = &runnerHooks{}
	}
	e := &env{home: t.TempDir(), hooks: hooks, launch: &launchRecord{}, roots: map[string]string{}, ids: map[string]string{}}
	e.run = &cmdrun.Fake{Script: func(name string, args []string) (string, error) {
		switch {
		case name == "bash" && filepath.Base(args[0]) == "start.sh" && len(args) == 2 && args[1] == "--help":
			return hooks.help(args)
		}
		return hooks.output(name, args)
	}}
	e.dockerfile(t, []string{"base", "swdev"})
	if err := e.reg(t, time.Now()).CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	e.addProject(t, "alpha", "base", "console")
	e.addProject(t, "beta", "base", "console")
	e.rebuild(t)
	return e
}

// rebuild recreates the view model against the current fixtures/hooks.
func (e *env) rebuild(t *testing.T) {
	t.Helper()
	e.m = e.build(t)
}

func (e *env) build(t *testing.T) *Model {
	t.Helper()
	return New(Deps{
		Home:             e.home,
		Runner:           e.run,
		StartScript:      filepath.Join(e.home, "start.sh"),
		BuildScript:      filepath.Join(e.home, "build-container.sh"),
		Dockerfile:       filepath.Join(e.home, "Dockerfile"),
		DockerfileCustom: "",
		Ops:              e.launch,
	})
}

// buildWithExec injects a recording terminal-handoff seam.
func (e *env) buildWithExec(t *testing.T, execed *[]string) *Model {
	t.Helper()
	return New(Deps{
		Home:             e.home,
		Runner:           e.run,
		StartScript:      filepath.Join(e.home, "start.sh"),
		BuildScript:      filepath.Join(e.home, "build-container.sh"),
		Dockerfile:       filepath.Join(e.home, "Dockerfile"),
		DockerfileCustom: "",
		Exec: func(c *exec.Cmd, cb tea.ExecCallback) tea.Cmd {
			*execed = append(*execed, strings.Join(c.Args, " "))
			return func() tea.Msg { return accessDoneMsg{} }
		},
	})
}

func (e *env) dockerfile(t *testing.T, editions []string) {
	t.Helper()
	var b strings.Builder
	for _, ed := range editions {
		b.WriteString("FROM scratch AS opencode-sandbox-" + ed + "\n")
	}
	if err := os.WriteFile(filepath.Join(e.home, "Dockerfile"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *env) reg(t *testing.T, now time.Time) *registry.Registry {
	t.Helper()
	if e.base.IsZero() {
		e.base = now
	}
	// Stamps recede per insert: registry AllOrdered sorts by last_used
	// desc (ascending sort + reverse), so alpha (stamped latest) is first.
	seq := e.seq
	e.seq++
	return &registry.Registry{Home: e.home, Now: func() time.Time { return e.base.Add(-time.Duration(seq) * time.Minute) }}
}

func (e *env) addProject(t *testing.T, name, edition, startOption string, extraModes ...string) {
	t.Helper()
	root := filepath.Join(e.home, "projects", name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	r := e.reg(t, time.Now())
	if err := r.AddProject(registry.AddRequest{Name: name, Path: root, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	modes := []string{"skills"}
	modes = append(modes, extraModes...)
	if err := sandboxcfg.Create(e.home, sandboxcfg.Path(root), edition, modes, startOption, "gwdg-saia"); err != nil {
		t.Fatal(err)
	}
	p, err := r.ByPath(root)
	if err != nil {
		t.Fatal(err)
	}
	e.roots[name] = root
	e.ids[name] = p.ContainerID
}

// --- step / render helpers (Bullet-dee test seam: the BubbleTea model) ---

func keyMsg(s string) tea.Msg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func clickAt(x, y int) tea.Msg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func bodyOf(t *testing.T, m *Model, w, h int) []string {
	t.Helper()
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })
	return strings.Split(shell.StripANSI(m.Body(w, h)), "\n")
}

func findLine(lines []string, needle string) (int, bool) {
	for i, l := range lines {
		if strings.Contains(l, needle) {
			return i, true
		}
	}
	return -1, false
}

func countLines(lines []string, needle string) int {
	n := 0
	for _, l := range lines {
		if strings.Contains(l, needle) {
			n++
		}
	}
	return n
}

func someLine(lines []string, needle string) bool { _, ok := findLine(lines, needle); return ok }

// --- external state readers ---

func (e *env) projects(t *testing.T) []registry.Project {
	t.Helper()
	projs, err := (&registry.Registry{Home: e.home}).AllOrdered()
	if err != nil {
		t.Fatal(err)
	}
	return projs
}

func (e *env) startStatus(t *testing.T, name string) string {
	t.Helper()
	for _, p := range e.projects(t) {
		if p.Name == name {
			return p.ContainerStatus
		}
	}
	t.Fatalf("project %s missing", name)
	return ""
}

func (e *env) registryOnDisk(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.home, ".config", "oc-sandbox", "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// startCalls extracts real start.sh invocation arg vectors (skip the
// --help discovery call: that one is exactly [script, --help]).
func (e *env) startCalls(t *testing.T) [][]string {
	t.Helper()
	var out [][]string
	for _, c := range e.run.Calls {
		if c.Name == "bash" && len(c.Args) > 2 && c.Args[0] == e.startScript() {
			out = append(out, c.Args[1:])
		}
	}
	return out
}

// buildCalls extracts build-container.sh invocation arg vectors.
func (e *env) buildCalls(t *testing.T) [][]string {
	t.Helper()
	var out [][]string
	for _, c := range e.run.Calls {
		if c.Name == "bash" && len(c.Args) > 1 && c.Args[0] == filepath.Join(e.home, "build-container.sh") {
			out = append(out, c.Args[1:])
		}
	}
	return out
}

// paneRowText renders the text under a one-row hit rect; paneRowRowAt
// renders the row dy rows below the rect's top line (cards put content
// one row below their top border).
func paneRowText(lines []string, r shell.Rect) string {
	return paneRowRowAt(lines, r, 0)
}

func paneRowRowAt(lines []string, r shell.Rect, dy int) string {
	idx := r.Y - shell.SubTabRowY - 1 + dy
	if idx < 0 || idx >= len(lines) {
		return ""
	}
	l := lines[idx]
	rs := []rune(l)
	if r.X >= len(rs) {
		return ""
	}
	l = string(rs[r.X:])
	if len(rs)-r.X > r.W {
		l = string(rs[r.X : r.X+r.W])
	}
	return l
}

// --- generic test helpers ---

func rectsEqual(a, b []shell.Rect) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- external-state helpers for tests ---

func (e *env) projectsFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(e.home, ".config", "oc-sandbox", "projects.json")
}

func (e *env) startScript() string { return filepath.Join(e.home, "start.sh") }

func (e *env) cfgEdition(t *testing.T, name string) string {
	t.Helper()
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(e.roots[name]))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.ContainerEdition
}

// reconcileStart drains the beginStart plumbing (batched key op + poll +
// reconcile cmd) until a start op is actually queued — without running
// the op itself (tests can then inspect the in-flight / queued state) —
// or until the cmd tree is empty (non-launch flows like the build
// prompt). Running the op past the queue is finishOps' job.
// finishKeyCmds drains the usual telemetry-free cmd remains of a key
// step (poll refresh); done only for the guard tests.
func (e *env) finishKeyCmds(cmd tea.Cmd) {
	for cmd != nil {
		msg := cmd()
		if msg == nil {
			return
		}
		switch msg := msg.(type) {
		case tea.BatchMsg:
			for _, c := range []tea.Cmd(msg) {
				e.finishKeyCmds(c)
			}
			return
		default:
			if e.mgr != nil {
				e.mgr.Update(msg)
			}
			_ = e.m.Update(msg)
			return
		}
	}
}

func (e *env) reconcileStart() {
	// drains the start plumbing far enough for the flow to stand still:
	// stops while an op is queued/running (queue tests assert that state),
	// otherwise runs to the end (non-launch flows land on prompts).
	var out []tea.Cmd
	out = append(out, e.lastCmd)
	wasBusy := e.mgr != nil && e.mgr.Busy()
	for len(out) > 0 {
		if len(e.launch.ops) > 0 || (e.mgr != nil && e.mgr.Busy() && !wasBusy) {
			break // op left the seam — leave its cmd for the caller
		}
		cmd := out[0]
		out = out[1:]
		if cmd == nil {
			continue
		}
		msg := cmd()
		switch msg := msg.(type) {
		case nil:
		case tea.BatchMsg:
			out = append(out, []tea.Cmd(msg)...)
		case spinner.TickMsg:
		default:
			if e.mgr != nil {
				if next := e.mgr.Update(msg); next != nil {
					out = append(out, next)
				}
			}
			if c := e.m.Update(msg); c != nil {
				out = append(out, c)
			}
		}
	}
	if len(out) > 0 {
		e.lastCmd = tea.Batch(out...)
	} else {
		e.lastCmd = nil
	}
}
