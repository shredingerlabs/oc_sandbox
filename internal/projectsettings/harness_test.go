package projectsettings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Test harness: real registry fixtures (no podman — this screen never
// starts anything), the BubbleTea model seam, rendered-lines assertions.

// env holds the fixture home; timestamps recede per insert so AllOrdered
// (last_used desc) lists alpha first and the open project is alpha.
type env struct {
	home  string
	base  time.Time
	seq   int
	roots map[string]string
}

func newEnv(t *testing.T, names ...string) *env {
	t.Helper()
	// Fixed-length home so card geometry is stable across runs.
	home := filepath.Join(os.TempDir(), "ocps-"+t.Name())
	if err := os.RemoveAll(home); err != nil {
		t.Fatal(err)
	}
	e := &env{home: home, roots: map[string]string{}}
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
	return New(e.home)
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
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
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
