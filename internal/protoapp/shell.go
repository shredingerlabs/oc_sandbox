// Shared shell: header menu, footer help, variant switcher. THROWAWAY
// prototype (issue #69). Every variant embeds this chrome.
package protoapp

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// headerMenuItems holds the spec state after ticket 63; footerReserve is the
// width Root reserves for the floating variant pill on the footer's last row.
var headerMenuItems = []string{"Open Project", "New Project", "Container", "Settings", "Exit"}

var footerReserve int

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
			key.WithHelp("v", "cycle"),
		),
		NextProject: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓", "select"),
		),
		PrevProject: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑", "select"),
		),
		Start: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("↵", "start"),
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
	return []key.Binding{k.SwitchVariant, k.NextProject, k.Start, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.SwitchVariant, k.Menu, k.Quit}, {k.NextProject, k.Start, k.Help}}
}

// menuRects returns the screen-space hit boxes of the header menu items
// (row 0), used by Root to switch center views on click.
func menuRects(width int) []rect {
	x := lipgloss.Width(styleHeaderTitle.Render("oc-sandbox"))
	out := make([]rect, len(headerMenuItems))
	for i, item := range headerMenuItems {
		w := lipgloss.Width(item) + 2 // padding 0,1
		out[i] = rect{x: x, y: 0, w: w, h: 1}
		x += w
	}
	_ = width
	return out
}

// shell renders the shared chrome around a variant's center view.
func shell(width, height int, menuFocus int, body string, h help.Model, k keyMap) string {
	// Header: title + menu items. Unselected items are white-on-blue so the
	// accent entry stands out by bold/pip only (user feedback on A).
	menu := make([]string, len(headerMenuItems))
	for i, item := range headerMenuItems {
		if i == menuFocus {
			menu[i] = lipgloss.NewStyle().Background(colAccent).Foreground(lipgloss.Color("0")).Bold(true).Padding(0, 1).Render(item)
		} else {
			menu[i] = lipgloss.NewStyle().Foreground(colCardFg).Padding(0, 1).Render(item)
		}
	}
	title := styleHeaderTitle.Render("oc-sandbox")
	left := lipgloss.Width(title + strings.Join(menu, ""))
	gap := width - left
	if gap < 1 {
		gap = 1
	}
	header := title + strings.Join(menu, "") + strings.Repeat(" ", gap)
	header = lipgloss.NewStyle().MaxWidth(width).Render(header)

	// Footer: mouse/legend hint on the left (moved here from the header per
	// user feedback), keyboard help right-aligned just before the variant
	// pill (root.View reserves its width in footerReserve before rendering).
	hint := styleFooter.Render("1 open · 2 new · 3 container · v design · m menu, ○/● status")
	helpView := h.View(k)
	avail := width - footerReserve
	lead := avail - lipgloss.Width(hint) - lipgloss.Width(helpView)
	footer := hint
	if lead >= 1 {
		footer = hint + strings.Repeat(" ", lead) + helpView
	}
	footer = lipgloss.NewStyle().MaxWidth(width).Render(footer)

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
