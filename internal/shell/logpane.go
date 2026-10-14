package shell

import (
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// LineMsg is one streamed output line for a LogPane; long-running
// operations emit it via Line from a tea.Cmd goroutine, and the owning
// view forwards every message it does not handle to LogPane.Update.
type LineMsg string

// Line returns a tea.Cmd producing a LineMsg.
func Line(s string) tea.Cmd {
	return func() tea.Msg { return LineMsg(s) }
}

// LogPane is the embedded log-pane skeleton (approved layout record
// #72): a bubbles viewport over the streamed lines plus a spinner row
// while an operation runs. Component only — the background-task manager
// (#82) builds on this contract.
type LogPane struct {
	vp      viewport.Model
	spin    spinner.Model
	content string
	running bool
	follow  bool
	noHead  bool // HideHead: the owner renders the spinner itself
}

// NewLogPane returns an empty, idle log pane that follows new lines.
func NewLogPane() LogPane {
	return LogPane{
		vp:     viewport.New(0, 0),
		spin:   spinner.New(spinner.WithSpinner(spinner.Dot)),
		follow: true,
	}
}

// SetSize resizes the pane; the first row is the spinner row, the rest
// is the viewport (the full height when the head is hidden).
func (l LogPane) SetSize(width, height int) LogPane {
	l.vp.Width = width
	if l.noHead {
		l.vp.Height = max(0, height)
	} else {
		l.vp.Height = max(0, height-1)
	}
	if l.follow {
		l.vp.GotoBottom()
	}
	return l
}

// Run marks an operation as running (spinner on) or finished (spinner
// off).
func (l LogPane) Run(on bool) LogPane {
	l.running = on
	return l
}

// Spinning reports whether the spinner is running.
func (l LogPane) Spinning() bool { return l.running }

// HideHead moves spinner ownership to the caller: the pane drops its
// own spinner row (and keeps the full size for the viewport), because
// the view rides the frame next to its status text (toast row).
func (l LogPane) HideHead() LogPane {
	l.noHead = true
	return l
}

// SpinFrame returns the spinner's current frame ("" when idle) so a view
// can place it behind its own status text instead of the pane's head row.
func (l LogPane) SpinFrame() string {
	if !l.running {
		return ""
	}
	return l.spin.View()
}

// Spin returns the tea.Cmd that advances the spinner one frame (nil
// when idle); Update schedules the next tick itself.
func (l LogPane) Spin() tea.Cmd {
	if !l.running {
		return nil
	}
	return func() tea.Msg { return l.spin.Tick() }
}

// Update handles LineMsg (append + auto-follow), spinner ticks, and
// viewport scrolling (keys / mouse wheel). Scrolling up stops the
// auto-follow; reaching the bottom re-enables it.
func (l LogPane) Update(msg tea.Msg) (LogPane, tea.Cmd) {
	switch msg := msg.(type) {
	case LineMsg:
		l.content += string(msg) + "\n"
		l.vp.SetContent(l.content)
		if l.follow {
			l.vp.GotoBottom()
		}
		return l, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		l.spin, cmd = l.spin.Update(msg)
		return l, cmd
	default:
		var cmd tea.Cmd
		l.vp, cmd = l.vp.Update(msg)
		if l.vp.AtBottom() {
			l.follow = true
		} else if l.scrolledUp(msg) {
			l.follow = false
		}
		return l, cmd
	}
}

// scrolledUp reports whether msg scrolls the viewport upward (wheel or
// key), which suspends the auto-follow.
func (l LogPane) scrolledUp(msg tea.Msg) bool {
	switch m := msg.(type) {
	case tea.MouseMsg:
		return m.Button == tea.MouseButtonWheelUp
	case tea.KeyMsg:
		switch m.String() {
		case "up", "pgup", "k":
			return true
		}
	}
	return false
}

// View renders the spinner row (when running) above the viewport; the
// owner of a hidden head renders the frame itself (toast row).
func (l LogPane) View() string {
	head := ""
	if l.running && !l.noHead {
		head = " " + l.spin.View()
	}
	return head + "\n" + l.vp.View()
}

// Lines returns the streamed lines so far (for tests and completion
// handling).
func (l LogPane) Lines() []string {
	out := strings.Split(strings.TrimSuffix(l.content, "\n"), "\n")
	if len(out) == 1 && out[0] == "" {
		return nil
	}
	return out
}
