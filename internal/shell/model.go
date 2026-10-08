package shell

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
// 1–4), sub-menu row, body region, footer help.
type Model struct {
	width, height int
	views         []CenterView
	active        int
	keys          KeyMap
	help          help.Model
}

// New returns a shell around the given center views, ordered like
// MenuItems (Open Project / New Project / Container / Settings).
func New(views ...CenterView) Model {
	return Model{views: views, keys: NewKeys(), help: help.New()}
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

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyMsg:
		if !m.views[m.active].TextFocused() {
			switch msg.String() {
			case "1", "2", "3", "4":
				if i := int(msg.String()[0] - '1'); i < len(m.views) {
					m.active = i
				}
				return m, nil
			}
			if key.Matches(msg, m.keys.Quit) {
				return m, tea.Quit
			}
		}
		return m, m.views[m.active].Update(msg)
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if msg.Y == 0 {
				for i, r := range MenuRects() {
					if r.Hit(msg.X, msg.Y) {
						if i >= len(m.views) { // Exit (or beyond)
							return m, tea.Quit
						}
						m.active = i
						return m, nil
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
	default:
		return m, m.views[m.active].Update(msg)
	}
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
	b.WriteString(FooterRow(m.width, m.help, m.keys))
	return b.String()
}
