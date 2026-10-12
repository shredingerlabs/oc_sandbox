package app_test

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/app"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

func stubShell() shell.Model {
	return shell.New(stubView{})
}

type stubView struct{}

func (stubView) Update(tea.Msg) tea.Cmd   { return nil }
func (stubView) Body(int, int) string     { return "" }
func (stubView) SubTabs() ([]string, int) { return nil, 0 }
func (stubView) TextFocused() bool        { return false }

// Exit-code discipline (spec #84 story 28, gate A5): 0 clean, 1 error,
// 2 user abort, 130 SIGINT.
func TestExitCodeMapsRunOutcome(t *testing.T) {
	m := stubShell()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"clean exit", nil, 0},
		{"general error", errors.New("podman exploded"), 1},
		{"user abort", app.ErrUserAbort, 2},
		{"wrapped user abort", errors.Join(app.ErrUserAbort), 2},
		{"SIGINT forwarded by bubbletea", tea.ErrInterrupted, 130},
		{"killed (SIGTERM/kill)", tea.ErrProgramKilled, 130},
	}
	for _, c := range cases {
		if got := app.ExitCode(m, c.err); got != c.want {
			t.Errorf("%s: ExitCode = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestExitCodeCtrlCKeysQuitAsSigint(t *testing.T) {
	m := stubShell()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if cmd == nil {
		t.Fatal("ctrl+c did not quit the shell")
	}
	final := m2.(shell.Model)
	if got := app.ExitCode(final, nil); got != 130 {
		t.Errorf("ctrl+c quit: ExitCode = %d, want 130", got)
	}
}

// q-exit is a CLEAN exit 0 (story 28 bash parity), unlike ctrl+c.
func TestExitCodeQuitKeyIsClean(t *testing.T) {
	m := stubShell()
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q did not quit the shell")
	}
	if got := app.ExitCode(m2.(shell.Model), nil); got != 0 {
		t.Errorf("q quit: ExitCode = %d, want 0", got)
	}
}
