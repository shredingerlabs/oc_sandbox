package shell

import (
	"github.com/charmbracelet/bubbles/key"
)

// KeyMap is the approved binding table (layout record #72): `1`–`4`
// switch the center view (suppressed while a text input holds focus),
// ↑↓/j/k select, Tab/Shift-Tab cycle fields, Enter activates/cycles,
// Space toggles, letters type, Esc/q quits. Center views reuse these
// bindings so the footer help (bubbles/help) renders the real table.
type KeyMap struct {
	View1    key.Binding
	View2    key.Binding
	View3    key.Binding
	View4    key.Binding
	Up       key.Binding
	Down     key.Binding
	Cycle    key.Binding
	Activate key.Binding
	Toggle   key.Binding
	Quit     key.Binding
}

// NewKeys returns the default binding table.
func NewKeys() KeyMap {
	return KeyMap{
		View1:    key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "open project")),
		View2:    key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "new project")),
		View3:    key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "container")),
		View4:    key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "settings")),
		Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "select up")),
		Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "select down")),
		Cycle:    key.NewBinding(key.WithKeys("tab", "shift+tab"), key.WithHelp("tab/⇧tab", "next field")),
		Activate: key.NewBinding(key.WithKeys("enter"), key.WithHelp("↵", "activate")),
		Toggle:   key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
		Quit:     key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "quit")),
	}
}

// ShortHelp implements help.KeyMap.
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Down, k.Activate, k.Quit}
}

// FullHelp implements help.KeyMap.
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.View1, k.View2, k.View3, k.View4},
		{k.Up, k.Down},
		{k.Cycle, k.Activate, k.Toggle},
		{k.Quit},
	}
}
