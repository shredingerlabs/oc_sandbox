package shell

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Minimum terminal size (terminal support matrix): below it the shell
// renders an inline too-small screen instead of the app.
const (
	MinWidth  = 80
	MinHeight = 24
)

// SubTabMsg is sent to the active CenterView when the user clicks one of
// its sub-menu tab pills (row SubTabRowY).
type SubTabMsg struct{ Index int }

// NavRequestMsg asks the root to switch the active center view (e.g. the
// Settings import flow referring into the New Project wizard). Views emit
// it as a tea.Cmd; the root consumes it so navigation stays a shell
// concern, not a view-to-view call.
type NavRequestMsg struct{ Index int }

// RecoverMsg asks the first-run setup flow to resume setup for the
// project at Path: the resumable recovery entry other views emit (e.g.
// Open Project details on a project with setup_complete false). Like
// NavRequestMsg it is consumed at the root level so views never depend
// on the setup flow's package.
type RecoverMsg struct{ Path string }

// Hook lets a center view see every message the root receives (before
// the active view). The first-run setup flow uses it to catch the
// post-create handoff (newproject.CreatedMsg) and RecoverMsg even when
// another view is active: consumed=false passes the message through
// unchanged, so hooks stay passive for everything they don't own.
type Hook interface {
	HookMsg(msg tea.Msg) (consumed bool, cmd tea.Cmd)
}

// CenterView is a center view (#77-#80) plugged into the shell. Update
// receives every message the shell itself does not consume (all key
// events after the shell's own handling, mouse events outside the
// header menu / sub-menu row, and any other message). Body renders into
// the body region: rows starting at screen row 3, height rows tall,
// full width — pin content top-left with the view's own left padding.
// SubTabs returns the sub-menu row items and the selected index (nil
// items = empty sub-menu row). TextFocused reports whether a text input
// currently holds focus: while true the shell suppresses its own key
// handling (1–4 view switch, quit) so letters type.
type CenterView interface {
	Update(msg tea.Msg) tea.Cmd
	Body(width, height int) string
	SubTabs() (items []string, selected int)
	TextFocused() bool
}

// Model is the root tea.Model: too-small guard, header menu (clicks and
// 1–4), sub-menu row, body region, footer help. When Queue is wired, the
// shell owns the shared background-task manager (#82): lifecycle
// messages (taskmgr.DoneMsg / StatusMsg / LineEvent) are broadcast to
// EVERY center view and handed to the manager exactly once, and quitting
// is guarded: while any view reports busy work the first quit asks for
// confirmation and any other key cancels it.
type Model struct {
	width, height int
	views         []CenterView
	active        int
	keys          KeyMap
	help          help.Model
	Queue         *taskmgr.Manager
	exitPending   bool
	exitStatus    int
}

// ExitStatus reports how the shell quit: 0 clean (q/esc/menu Exit) or
// ExitSigint (ctrl+c, story 28 bash parity); read the final model after
// tea.Program.Run.
func (m Model) ExitStatus() int { return m.exitStatus }

// New returns a shell around the given center views, ordered like
// MenuItems (Open Project / New Project / Container / Settings).
func New(views ...CenterView) Model {
	return Model{views: views, keys: NewKeys(), help: help.New()}
}

// NewWithQueue returns a shell wired to the shared background-task
// manager (#82).
func NewWithQueue(q *taskmgr.Manager, views ...CenterView) Model {
	m := New(views...)
	m.Queue = q
	return m
}

// ExitPending reports whether an exit confirmation is up (guarded exit).
func (m Model) ExitPending() bool { return m.exitPending }

// Quit is the guarded quit decision: false while background work runs
// (the caller keeps the model and re-asks after the confirmation), true
// when no work is in flight — mirroring Update's quit path.
func (m Model) Quit() bool { return !m.workBusy() }

// workBusy reports whether background work runs: the wired queue's state
// or, without one, any view that implements the Busy query.
func (m Model) workBusy() bool {
	if m.Queue != nil {
		return m.Queue.Busy()
	}
	for _, v := range m.views {
		if b, ok := v.(BusyView); ok && b.Busy() {
			return true
		}
	}
	return false
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Active returns the index of the active center view.
func (m Model) Active() int { return m.active }

// TooSmall reports whether the terminal is below the 80x24 minimum.
func (m Model) TooSmall() bool {
	return m.width < MinWidth || m.height < MinHeight
}

// SubTabRects returns the current view's sub-tab pill hit rects.
func (m Model) SubTabRects() []Rect {
	if m.active >= len(m.views) {
		return nil
	}
	items, selected := m.views[m.active].SubTabs()
	_, rects := SubTabsRow(items, selected)
	return rects
}

// BusyView is a center view that may have background work in flight;
// center views with long ops implement it for the guarded exit (#82).
type BusyView interface {
	Busy() bool
}

// ConfirmView is a center view with a confirmation popup open right now
// (ADR-0021). While true the shell routes esc/q/1–4 to the view so the
// popup consumes them instead of quitting or switching.
type ConfirmView interface {
	ConfirmOpen() bool
}

// confirmOpen polls the active view's confirmation-popup state.
func (m Model) confirmOpen() bool {
	if cv, ok := m.views[m.active].(ConfirmView); ok {
		return cv.ConfirmOpen()
	}
	return false
}

// ActivableView is a center view that refreshes external state when it
// becomes active. The shell reuses view instances across switches, so a
// view whose data lives outside the model (e.g. the Open Project view's
// registry read) implements this to stay fresh without polling.
type ActivableView interface {
	Activate() tea.Cmd
}

// setActive switches the active view to i and activates the newly-active
// view, returning its activation cmd. Pointer receiver: Update's value
// copy calls it and then returns the mutated model.
func (m *Model) setActive(i int) tea.Cmd {
	if i < 0 || i >= len(m.views) {
		return nil
	}
	m.active = i
	if av, ok := m.views[i].(ActivableView); ok {
		return av.Activate()
	}
	return nil
}

// Update implements tea.Model.
// menuViews is the number of view slots the header menu addresses: the
// first four MenuItems map to them, Exit is fixed at the end. Views
// beyond that (the appended first-run setup flow) are reachable only
// through root-level handoff messages — no header item, no click.
func menuViews(numViews int) int { return min(numViews, len(MenuItems)-1) }

func (m Model) exitRectIndex() int { return len(MenuItems) - 1 }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Hooked views see every message first (post-create handoff and
	// recovery routing); the first hook that consumes one wins. A
	// consumed RecoverMsg also focuses the hooking view, so navigation
	// stays a root concern.
	for i, v := range m.views {
		if h, ok := v.(Hook); ok {
			if consumed, cmd := h.HookMsg(msg); consumed {
				// A consumed handoff (RecoverMsg, CreatedMsg, …) also
				// focuses the hooking view, so navigation stays a root
				// concern.
				m.setActive(i)
				return m, cmd
			}
		}
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case taskmgr.DoneMsg, taskmgr.StatusMsg, taskmgr.LineEvent:
		return m, m.broadcastTaskMsg(msg)
	case tea.KeyMsg:
		if !m.views[m.active].TextFocused() {
			if msg.Type == tea.KeyCtrlC { // ctrl+c = SIGINT discipline (story 28): exit 130
				m.exitStatus = ExitSigint
				return m, tea.Quit
			}
			// Confirmation popup (ADR-0021): while the active view has
			// one open it consumes q/esc/1–4 itself — routing them to
			// the shell would quit or switch on the popup's q/esc. The
			// popup's cancel counts as "any other key" for the guarded
			// exit, so a pending confirmation is dropped.
			if cv, ok := m.views[m.active].(ConfirmView); ok && cv.ConfirmOpen() {
				m.exitPending = false
				return m, m.views[m.active].Update(msg)
			}
			if key.Matches(msg, m.keys.Quit) {
				if m.workBusy() { // guarded exit (#82): ask once, cancel by doing anything else
					m.exitPending = true
				} else {
					m.exitPending = false
					return m, tea.Quit
				}
				return m, nil
			}
			switch msg.String() {
			case "1", "2", "3", "4":
				m.exitPending = false
				act := m.setActive(int(msg.String()[0] - '1'))
				return m, act
			default:
				m.exitPending = false
			}
		}
		return m, m.views[m.active].Update(msg)
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if msg.Y == 0 {
				for i, r := range MenuRects() {
					if r.Hit(msg.X, msg.Y) {
						if i >= menuViews(len(m.views)) { // Exit (or beyond)
							if m.workBusy() { // guarded exit: first click asks (#82)
								m.exitPending = true
								return m, nil
							}
							m.exitPending = false
							return m, tea.Quit
						}
						m.exitPending = false
						act := m.setActive(i)
						return m, act
					}
				}
			}
			if msg.Y == SubTabRowY {
				for i, r := range m.SubTabRects() {
					if r.Hit(msg.X, msg.Y) {
						return m, m.views[m.active].Update(SubTabMsg{Index: i})
					}
				}
			}
		}
		return m, m.views[m.active].Update(msg)
	case NavRequestMsg:
		act := m.setActive(msg.Index)
		return m, act
	default:
		return m, m.views[m.active].Update(msg)
	}
}

// broadcastTaskMsg hands a task-manager message to the wired queue
// exactly once and to every center view (the owner reacts, all others
// ignore foreign ops) — navigation stays free (#82).
func (m Model) broadcastTaskMsg(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	if m.Queue != nil {
		cmds = append(cmds, m.Queue.Update(msg))
	}
	for _, v := range m.views {
		cmds = append(cmds, v.Update(msg))
	}
	return tea.Batch(cmds...)
}

// View implements tea.Model.
func (m Model) View() string {
	if m.TooSmall() {
		return fmt.Sprintf("Terminal too small — recommended 80x24, got %dx%d.\nResize to continue.", m.width, m.height)
	}
	items, selected := m.views[m.active].SubTabs()
	tabs, _ := SubTabsRow(items, selected)
	tabsRow := strings.Repeat(" ", TitleWidth()-1) + tabs
	bodyH := m.height - BodyRows
	body := lipgloss.NewStyle().Width(m.width).Height(bodyH).
		MaxHeight(bodyH).MaxWidth(m.width).Render(m.views[m.active].Body(m.width, bodyH))
	var b strings.Builder
	b.WriteString(HeaderRow(m.active, m.width))
	b.WriteString("\n")
	b.WriteString(SeparatorRow(m.width))
	b.WriteString("\n")
	b.WriteString(tabsRow)
	b.WriteString("\n")
	b.WriteString(body)
	b.WriteString("\n")
	b.WriteString(m.Footer())
	// Full-window black background: the screen paint fills every cell the
	// app covers, even behind cards and padding the views leave plain.
	return lipgloss.NewStyle().Background(ColWindowBg).
		Width(m.width).Height(m.height).MaxHeight(m.height).Render(b.String())
}

// Footer renders the help row, or — while a guarded exit waits for
// confirmation (#82) — the "ops running" strip.
func (m Model) Footer() string {
	if m.exitPending {
		return strings.Join([]string{"", StyleWarn.Render(
			"● background work running — press q again to quit, any other key cancels")}, "\n")
	}
	return FooterRow(m.width, m.help, m.keys)
}
