package projectsettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Test harness: real registry fixtures (no podman — this screen never
// starts anything), the BubbleTea model seam, rendered-lines assertions.

// env holds the fixture home; timestamps recede per insert so AllOrdered
// (last_used desc) lists alpha first and the open project is alpha.
type env struct {
	home   string
	base   time.Time
	seq    int
	roots  map[string]string
	run    *cmdrun.Fake
	launch *launchRecord
}

// launchRecord records every op handed to the seam and executes it like
// DefaultLauncher so save flows complete inline.
type launchRecord struct {
	ops []taskmgr.Op
}

func (l *launchRecord) Launch(op taskmgr.Op) tea.Cmd {
	l.ops = append(l.ops, op)
	return func() tea.Msg {
		var lines []string
		err := op.Run(func(line string) { lines = append(lines, line) })
		return taskmgr.DoneMsg{ID: op.ID, Key: op.Key, Lines: lines, Err: err}
	}
}

func newEnv(t *testing.T, names ...string) *env {
	t.Helper()
	// Fixed-length home so card geometry is stable across runs.
	home := filepath.Join(os.TempDir(), "ocps-"+t.Name())
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	e := &env{home: home, roots: map[string]string{}, run: &cmdrun.Fake{}, launch: &launchRecord{}}
	if err := e.reg(t, time.Now()).CreateProjectsFile(); err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		e.addProject(t, n)
	}
	return e
}

func (e *env) reg(t *testing.T, now time.Time) *registry.Registry {
	t.Helper()
	if e.base.IsZero() {
		e.base = now
	}
	seq := e.seq
	e.seq++
	return &registry.Registry{Home: e.home, Now: func() time.Time { return e.base.Add(-time.Duration(seq) * time.Minute) }}
}

func (e *env) addProject(t *testing.T, name string) {
	t.Helper()
	root := filepath.Join(e.home, "projects", name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	r := e.reg(t, time.Now())
	if err := r.AddProject(registry.AddRequest{Name: name, Path: root, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	if err := sandboxcfg.Create(e.home, sandboxcfg.Path(root), "base", []string{"skills"}, "console", "gwdg-saia"); err != nil {
		t.Fatal(err)
	}
	e.roots[name] = root
}

func (e *env) view(t *testing.T) *View {
	t.Helper()
	return New(Deps{
		Home:            e.home,
		Runner:          e.run,
		ConfigureScript: filepath.Join(e.home, "scripts", "configure-project.sh"),
		Ops:             e.launch,
	})
}

// --- VCS fixture helpers -------------------------------------------------

// setTracking patches the project's sandbox_config vcs_tracking.
func (e *env) setTracking(t *testing.T, name, tracking string) {
	t.Helper()
	if err := sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots[name]), "vcs_tracking", tracking); err != nil {
		t.Fatal(err)
	}
}

// setRepoURL patches the project's sandbox_config repo_url.
func (e *env) setRepoURL(t *testing.T, name, url string) {
	t.Helper()
	if err := sandboxcfg.PatchString(e.home, sandboxcfg.Path(e.roots[name]), "repo_url", url); err != nil {
		t.Fatal(err)
	}
}

// writeGitconfig seeds the project-local git config (git-identity shape).
func (e *env) writeGitconfig(t *testing.T, name, gitName, gitEmail string) {
	t.Helper()
	p := filepath.Join(e.roots[name], ".git_local", "gitconfig")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	content := "[user]\n\tname = " + gitName + "\n\temail = " + gitEmail + "\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// writeHostFile seeds a CLI host credential file with the given hosts
// (the JSON shape configure-project.sh writes).
func (e *env) writeHostFile(t *testing.T, name, sub string, hosts ...string) {
	t.Helper()
	doc := map[string]any{}
	for _, h := range hosts {
		doc[h] = map[string]any{"token": "x"}
	}
	p := filepath.Join(e.roots[name], ".git_local", sub)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// configureCalls extracts configure-project.sh invocation arg vectors
// (the view always invokes it through `bash <script> <args...>`).
func (e *env) configureCalls(t *testing.T) [][]string {
	t.Helper()
	var out [][]string
	for _, c := range e.run.Calls {
		if c.Name == "bash" && filepath.Base(c.Args[0]) == "configure-project.sh" {
			out = append(out, c.Args[1:])
		}
	}
	return out
}

// sandboxValue reads one string field of the project's sandbox_config.
func (e *env) sandboxValue(t *testing.T, name, field string) string {
	t.Helper()
	data, err := os.ReadFile(sandboxcfg.Path(e.roots[name]))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	v, _ := doc[field].(string)
	return v
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
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// typeKeys sends a string one rune key at a time (spaces included).
func typeKeys(v *View, s string) {
	for _, r := range s {
		if r == ' ' {
			v.Update(keyMsg(" "))
			continue
		}
		v.Update(keyMsg(string(r)))
	}
}

func clickAt(x, y int) tea.Msg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func wheelAt(dir int) tea.Msg {
	b := tea.MouseButtonWheelDown
	if dir < 0 {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{Button: b}
}

// bodyLines renders the body stripped of ANSI, right-trimmed.
func bodyLines(t *testing.T, v *View) []string {
	t.Helper()
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })
	var out []string
	for _, l := range strings.Split(shell.StripANSI(v.Body(76, 26)), "\n") {
		out = append(out, strings.TrimRight(l, " "))
	}
	return out
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

// paneRowText renders the text under a one-row hit rect.
func paneRowText(lines []string, r shell.Rect) string {
	idx := r.Y - shell.SubTabRowY - 1
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
