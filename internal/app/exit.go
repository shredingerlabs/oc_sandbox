package app

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// ErrUserAbort marks a user-initiated abort surfaced by a delegated
// flow (the standard exit-2 path); wrapped anywhere in the error chain.
var ErrUserAbort = errors.New("user abort")

// ExitCode maps the tea.Program run outcome plus the final shell model
// onto the standard exit codes (spec #84 story 28, gate A5): 0 clean
// (q/esc exit is a CLEAN exit, bash parity), 1 general error, 2 user
// abort, 130 SIGINT — both the external signal (bubbletea forwards it
// as ErrInterrupted/ErrProgramKilled) and a ctrl+c keystroke quit.
func ExitCode(m shell.Model, err error) int {
	switch {
	case err == nil:
		if m.ExitStatus() == shell.ExitSigint {
			return 130
		}
		return 0
	case errors.Is(err, tea.ErrInterrupted), errors.Is(err, tea.ErrProgramKilled):
		return 130
	case errors.Is(err, ErrUserAbort):
		return 2
	default:
		return 1
	}
}
