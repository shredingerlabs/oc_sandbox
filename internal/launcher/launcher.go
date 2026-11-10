// Package launcher is the minimal TUI launcher face (spec §4, #151):
// exactly two buttons — Open WebUI and Quit — plus the URL/status line
// showing the full tokenized URL while the webserver runs. The
// lifecycle around the face lives in cmd/oc-sandbox (server at launch,
// single-instance probe, SSE shutdown push on quit); the face owns the
// quit interaction: guarded exit, the containers prompt ("Stop running
// containers before quitting? [y/N]"), and the graceful stop.
//
// The package stays self-contained (no shell/theme imports) so the
// parity-gate flip (#99) can delete the full TUI without touching it.
package launcher

import (
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/containerview"
	"github.com/shredingerlabs/oc-sandbox/internal/discovery"
	"github.com/shredingerlabs/oc-sandbox/internal/registry"
)

// Options carries the launcher's wiring. Open defaults to the platform
// browser opener (§3: xdg-open / open / cmd start); Busy is the
// guarded-exit check (nil = never busy); Runner + Registry feed the
// containers prompt and the graceful stop (nil Runner = nothing can run).
type Options struct {
	URL      string // full tokenized URL, "" when the server is not running
	Open     func(url string) error
	Busy     func() bool
	Runner   cmdrun.Runner
	Registry *registry.Registry
}

// rect is a clickable region in view coordinates (visible cells).
type rect struct{ x, y, w int }

func (r rect) hit(x, y int) bool {
	return y == r.y && x >= r.x && x < r.x+r.w
}

// Model is the tea.Model for the launcher face. Update has a pointer
// receiver: quit-flow state mutates across messages.
type Model struct {
	opts Options
	focus int // 0 = Open WebUI, 1 = Quit
	openRect, quitRect rect
	promptFocus   int // during the containers prompt: 0 = Yes, 1 = No (default), 2 = Cancel
	promptYesRect,
	promptNoRect,
	promptCancelRect rect
	guardCancelRect rect // guarded exit's Cancel button
	openFailed       bool
	exitPending      bool // guarded exit is up: press Quit again
	prompt           bool // containers prompt open
	stopNames        []string
	quitRequested    bool
	stopErr          error
	exitStatus       int // 0 clean, 130 ctrl+c (story 28 SIGINT parity)
}

// New builds the launcher face.
func New(opts Options) Model {
	if opts.Open == nil {
		opts.Open = platformOpen
	}
	return Model{opts: opts}
}

// OpenFailed reports whether the last Open WebUI activation failed —
// the face keeps the URL visible for manual copy either way.
func (m *Model) OpenFailed() bool { return m.openFailed }

// QuitRequested reports whether the model decided to quit (tea.Quit was
// issued): the caller then pushes the SSE shutdown and closes the server.
func (m *Model) QuitRequested() bool { return m.quitRequested }

// ExitPending reports whether the guarded-exit confirmation is up.
func (m *Model) ExitPending() bool { return m.exitPending }

// QuitPrompt reports whether the containers prompt is open.
func (m *Model) QuitPrompt() bool { return m.prompt }

// ExitStatus reports how the face quit: 0 clean or 130 (ctrl+c, story 28
// bash parity); read the final model after tea.Program.Run.
func (m *Model) ExitStatus() int { return m.exitStatus }

// FinalSummary is what the caller prints to stdout after the program
// exits: the face runs on the altscreen, so scrollback only gets the
// pre-TUI transcript — the caller re-prints the URL/status line plus the
// last outcome here to keep the launch transcript complete.
func (m *Model) FinalSummary() string {
	var b strings.Builder
	if m.opts.URL != "" {
		b.WriteString(" WebUI: " + urlStyle.Render(m.opts.URL))
	} else {
		b.WriteString(warnStyle.Render(" WebUI: (not running)"))
	}
	if m.stopErr != nil {
		b.WriteString("\n" + warnStyle.Render("Stopping containers failed: "+m.stopErr.Error()))
	}
	if m.openFailed {
		b.WriteString("\n" + warnStyle.Render("Could not open a browser — copy the URL above"))
	}
	return b.String()
}

// OpenBrowser opens the platform browser (§3 opener) — the launch-time
// auto-open rides it alongside the Open WebUI button.
func OpenBrowser(url string) error { return platformOpen(url) }

// openDoneMsg carries the browser-open outcome back through Update (the
// cmd goroutine never touches the model directly — no races).
type openDoneMsg struct{ err error }

// stopDoneMsg carries the graceful-stop outcome back through Update.
type stopDoneMsg struct {
	err error
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update implements tea.Model (bubbletea v1 signature: the model comes
// back; the launcher mutates through its pointer receiver).
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case openDoneMsg:
		m.openFailed = msg.err != nil
		return m, nil
	case stopDoneMsg:
		m.stopErr = msg.err
		if msg.err != nil {
			return m, nil // stay: the face shows the failure, Quit can retry
		}
		m.quitRequested = true
		return m, tea.Quit
	case tea.KeyMsg:
		return m.updateKey(msg)
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			// While the containers prompt or guarded exit owns the
			// bottom row, only its buttons respond (keyboard parity:
			// the same states consume all other keys).
			if m.prompt {
				if m.promptYesRect.hit(msg.X, msg.Y) {
					return m, m.answerYes()
				}
				if m.promptNoRect.hit(msg.X, msg.Y) {
					return m, m.answerNo()
				}
				if m.promptCancelRect.hit(msg.X, msg.Y) {
					m.prompt = false
					return m, nil
				}
				return m, nil
			}
			if m.exitPending && m.guardCancelRect.hit(msg.X, msg.Y) {
				m.exitPending = false
				return m, nil
			}
			if m.openRect.hit(msg.X, msg.Y) {
				return m, m.cmdOpen()
			}
			if m.quitRect.hit(msg.X, msg.Y) {
				return m, m.requestQuit()
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) updateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyCtrlC:
		// SIGINT discipline (story 28 parity): leave running, quit now —
		// the caller still pushes the best-effort SSE shutdown.
		m.exitStatus = 130
		m.quitRequested = true
		return m, tea.Quit
	}

	if m.prompt { // containers prompt consumes everything else
		switch s := strings.ToLower(msg.String()); {
		case s == "y":
			return m, m.answerYes()
		case s == "n":
			return m, m.answerNo() // shortcut: leave them running
		case s == "esc":
			m.prompt = false // esc cancels the whole quit
			return m, nil
		case s == "enter" || s == " ":
			return m, m.answerPrompt() // activates the focused button
		case s == "tab" || s == "right" || s == "left" || s == "shift+tab":
			m.promptFocus = (m.promptFocus + 1) % 3 // Yes, No, Cancel …
		}
		return m, nil
	}

	if m.exitPending { // guarded exit: Quit again proceeds, anything else cancels
		if keyName(msg) == "q" {
			m.exitPending = false
			return m, m.askContainers()
		}
		m.exitPending = false
		return m, nil
	}

	switch keyName(msg) {
	case "tab", "right", "left", "shift+tab":
		m.focus = 1 - m.focus
	case "enter", " ":
		if m.focus == 0 {
			return m, m.cmdOpen()
		}
		return m, m.requestQuit()
	case "q":
		return m, m.requestQuit()
	case "o":
		return m, m.cmdOpen()
	}
	return m, nil
}

// keyName normalizes rune keys to their string ("q", "y", …).
func keyName(msg tea.KeyMsg) string {
	if msg.Type == tea.KeyRunes {
		return string(msg.Runes)
	}
	return msg.String()
}

func (m *Model) cmdOpen() tea.Cmd {
	open, url := m.opts.Open, m.opts.URL
	return func() tea.Msg { return openDoneMsg{err: open(url)} }
}

// requestQuit runs the guarded exit: background work up → press Quit
// again; otherwise straight to the containers question.
func (m *Model) requestQuit() tea.Cmd {
	if m.opts.Busy != nil && m.opts.Busy() {
		m.exitPending = true
		return nil
	}
	m.exitPending = false
	return m.askContainers()
}

// askContainers scopes the containers prompt: no oc-sandbox containers
// running → quit directly; otherwise ask before touching anything.
func (m *Model) askContainers() tea.Cmd {
	if m.opts.Runner == nil {
		m.quitRequested = true
		return tea.Quit
	}
	names := discovery.RunningContainers(m.opts.Runner)
	if len(names) == 0 {
		m.quitRequested = true
		return tea.Quit
	}
	m.stopNames = names
	m.prompt = true
	m.promptFocus = 1 // No is the default: leave them running
	return nil
}

// answerPrompt activates the currently focused prompt button.
func (m *Model) answerPrompt() tea.Cmd {
	switch m.promptFocus {
	case 0:
		return m.answerYes()
	case 2:
		m.prompt = false
		return nil
	default:
		return m.answerNo()
	}
}

// answerYes stops the containers, then quits; a stop failure surfaces
// through stopDoneMsg and aborts the quit (Quit can retry).
func (m *Model) answerYes() tea.Cmd {
	m.prompt = false
	return m.cmdStop()
}

// answerNo quits right away, leaving the containers running (the prompt
// default, glossary: detached containers outlive the launcher).
func (m *Model) answerNo() tea.Cmd {
	m.prompt = false
	m.quitRequested = true
	return tea.Quit
}

func (m *Model) cmdStop() tea.Cmd {
	names := m.stopNames
	run, reg := m.opts.Runner, m.opts.Registry
	return func() tea.Msg {
		return stopDoneMsg{err: containerview.StopContainers(run, reg, names, func(string) {})}
	}
}

// View implements tea.Model: the minimal face.
func (m *Model) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("oc-sandbox") + "\n\n")

	// Two buttons on row 2 with the URL/status line beneath them
	// (separated by a blank, row 4) — the click rects derive from the
	// same layout the renderer paints (color codes never shift cells).
	openBtn, openFocused := "[ Open WebUI ]", m.focus == 0
	quitBtn, quitFocused := "[ Quit ]", m.focus == 1
	const row = 2
	openX, openW := 1, len(openBtn)
	quitX := openX + openW + 3
	m.openRect, m.quitRect = rect{x: openX, y: row, w: openW}, rect{x: quitX, y: row, w: len(quitBtn)}
	buttons := strings.Repeat(" ", openX)
	if openFocused {
		buttons += focusedStyle.Render(openBtn)
	} else {
		buttons += idleStyle.Render(openBtn)
	}
	buttons += strings.Repeat(" ", quitX-openX-openW)
	if quitFocused {
		buttons += focusedStyle.Render(quitBtn)
	} else {
		buttons += idleStyle.Render(quitBtn)
	}
	b.WriteString(buttons + "\n\n")
	if m.opts.URL != "" {
		b.WriteString(" WebUI: " + urlStyle.Render(m.opts.URL) + "\n\n")
	} else {
		b.WriteString(warnStyle.Render(" WebUI: (not running)") + "\n\n")
	}

	// The status row (row 6) and the prompt's button row (row 7, only
	// rendered together with its question) share the bottom area; their
	// click rects derive from the same painted layout.
	const promptBtnRow = 7
	switch {
	case m.prompt:
		b.WriteString(warnStyle.Render(fmt.Sprintf(
			"Stop %d oc-sandbox container(s) before quitting? (esc cancels)",
			len(m.stopNames))) + "\n")
		yes, no, cancel := "[ Yes ]", "[ No ]", "[ Cancel ]"
		yesX, yesW := 1, len(yes)
		noX := yesX + yesW + 3
		cancelX := noX + len(no) + 3
		m.promptYesRect = rect{x: yesX, y: promptBtnRow, w: yesW}
		m.promptNoRect = rect{x: noX, y: promptBtnRow, w: len(no)}
		m.promptCancelRect = rect{x: cancelX, y: promptBtnRow, w: len(cancel)}
		buttons := strings.Repeat(" ", yesX) +
			renderButton(yes, m.promptFocus == 0) +
			strings.Repeat(" ", noX-yesX-yesW) +
			renderButton(no, m.promptFocus == 1) +
			strings.Repeat(" ", cancelX-noX-len(no)) +
			renderButton(cancel, m.promptFocus == 2)
		b.WriteString(buttons + "\n")
	case m.exitPending:
		b.WriteString(warnStyle.Render("Background work is running — press Quit again to quit (esc cancels)") + "\n")
		cancel := "[ Cancel ]"
		m.guardCancelRect = rect{x: 1, y: promptBtnRow, w: len(cancel)}
		b.WriteString(strings.Repeat(" ", 1) + renderButton(cancel, false) + "\n")
	case m.stopErr != nil:
		b.WriteString(warnStyle.Render("Stopping containers failed: "+m.stopErr.Error()) + "\n")
	case m.openFailed:
		b.WriteString(warnStyle.Render("Could not open a browser — copy the URL above") + "\n")
	}
	return b.String()
}

// renderButton paints one bracket button with the face's local styles
// (focused = accent, idle = dimmed).
func renderButton(label string, focused bool) string {
	if focused {
		return focusedStyle.Render(label)
	}
	return idleStyle.Render(label)
}

// palette — local to the launcher so the parity-gate flip deleting the
// full TUI leaves the face untouched.
var (
	titleStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	urlStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	focusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	idleStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
)

// platformOpen is the §3 browser opener: xdg-open / macOS open /
// Windows cmd start. Failures surface through OpenFailed — the URL
// stays visible for manual copy.
func platformOpen(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // no zombies across repeated opens
	return nil
}
