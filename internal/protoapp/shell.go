// Shared shell: header menu, footer help, variant switcher. THROWAWAY
// prototype (issue #69). Every variant embeds this chrome.
package protoapp

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// Menu items reflect the spec state after ticket 63 (Settings Flow
// Consolidation): the header menu holds Open Project, New Project, Container,
// Settings, Exit.
var headerMenuItems = []string{"Open Project", "New Project", "Container", "Settings", "Exit"}

type keyMap struct {
	SwitchVariant key.Binding
	NextProject   key.Binding
	PrevProject   key.Binding
	Start         key.Binding
	Menu          key.Binding
	Help          key.Binding
	Quit          key.Binding
}

func newKeys() keyMap {
	return keyMap{
		SwitchVariant: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "next variant"),
		),
		NextProject: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓", "select project"),
		),
		PrevProject: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑", "select project"),
		),
		Start: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "start project"),
		),
		Menu: key.NewBinding(
			key.WithKeys("m"),
			key.WithHelp("m", "menu"),
		),
		Help: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "more"),
		),
		Quit: key.NewBinding(
			key.WithKeys("q", "esc"),
			key.WithHelp("q", "quit"),
		),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.SwitchVariant, k.NextProject, k.Start, k.Menu, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.SwitchVariant, k.Menu, k.Quit}, {k.NextProject, k.Start, k.Help}}
}

// shell renders the shared chrome around a variant's center view.
func shell(width, height int, menuFocus int, body string, h help.Model, k keyMap) string {
	// Header: title + menu items.
	menu := make([]string, len(headerMenuItems))
	for i, item := range headerMenuItems {
		if i == menuFocus {
			menu[i] = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1).Render(item)
		} else {
			menu[i] = lipgloss.NewStyle().Foreground(colMuted).Padding(0, 1).Render(item)
		}
	}
	title := styleHeaderTitle.Render("oc-sandbox")
	hint := styleHeader.Render("m: menu ↩ scroll, ○/● container status")
	left := lipgloss.Width(title + strings.Join(menu, ""))
	gap := width - left - lipgloss.Width(hint)
	if gap < 1 {
		gap = 1
	}
	header := title + strings.Join(menu, "") + strings.Repeat(" ", gap) + hint
	header = lipgloss.NewStyle().MaxWidth(width).Render(header)

	footer := h.View(k)
	if fpad := width - lipgloss.Width(footer); fpad > 0 {
		footer = strings.Repeat(" ", fpad) + footer
	}
	footer = styleFooter.Render(footer)

	// Vertical layout: clamp/fill body so header+body+footer == height.
	maxBody := height - 2
	if lipgloss.Height(body) > maxBody {
		body = lipgloss.NewStyle().MaxHeight(maxBody).Render(body)
	}
	pad := maxBody - lipgloss.Height(body)
	if pad < 0 {
		pad = 0
	}
	var b strings.Builder
	b.WriteString(header)
	for i := 0; i < pad; i++ {
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(body)
	b.WriteString("\n")
	b.WriteString(footer)
	return b.String()
}
