// Variant A: card grid. THROWAWAY prototype (issue #69).
// Cards flow left-to-right, wrapping on width. Single click selects
// (focus ring + details strip below the grid); double-click / Enter starts.
package protoapp

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type variantA struct {
	projects []Project
	focus    int
	width    int
	cardW    int
	cols     int
	dbl      dblClickTracker
	toast    string
}

func newVariantA() *variantA {
	return &variantA{projects: fakeProjects(), cardW: 30}
}

func (v *variantA) Name() string { return "A (Card grid + detail strip)" }

func (v *variantA) Update(msg tea.Msg, width, height int) {
	v.width = width
	// Body height: shell takes header+footer.
	bodyH := height - 2
	cardH := 7
	rows := maxInt(1, (bodyH-4)/cardH) // 4 lines for detail strip
	if rows < 1 {
		rows = 1
	}
	if cols := maxInt(1, (width-2)/(v.cardW+2)); cols != v.cols {
		v.cols = cols
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "down", "j", "tab", "right", "l":
			v.focus = (v.focus + 1) % len(v.projects)
		case "up", "k", "shift+tab", "left", "h":
			v.focus = (v.focus - 1 + len(v.projects)) % len(v.projects)
		case "enter":
			v.toast = fmt.Sprintf("▶ start %s (fake)", v.projects[v.focus].Name)
		}
	case tea.MouseMsg:
		n := v.dbl.press(msg)
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if idx, hit := v.hitCard(msg.X, msg.Y, bodyH); hit {
				if n == 2 {
					v.focus = idx
					v.toast = fmt.Sprintf("▶ start %s (fake double-click)", v.projects[idx].Name)
				} else if idx != v.focus {
					v.focus = idx
					v.toast = ""
				}
			}
		}
		if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
			// grid fits; scroll is a no-op (reaction target: does that feel wasted?)
		}
	}
}

func (v *variantA) hitCard(x, y, bodyH int) (int, bool) {
	if v.cols == 0 {
		return 0, false
	}
	cardH := 7
	row := y / cardH // header is row 0; cards start at line 1
	if row == 0 {
		return 0, false
	}
	// rows scroll region: cards start at body row 0 (after header in shell)
	_ = bodyH
	col := x / (v.cardW + 2)
	idx := (row-1)*v.cols + col
	if idx >= 0 && idx < len(v.projects) {
		return idx, true
	}
	return 0, false
}

func (v *variantA) View(width, height int, h help.Model, k keyMap) string {
	bodyH := height - 2
	cardH := 7
	rowsWanted := maxInt(1, (bodyH-4)/cardH)

	cards := make([]string, len(v.projects))
	for i, p := range v.projects {
		cards[i] = v.card(p, i == v.focus, width)
	}

	var gridRows []string
	for start := 0; start < len(cards); start += v.cols {
		end := min(start+v.cols, len(cards))
		row := lipgloss.JoinHorizontal(lipgloss.Top, cards[start:end]...)
		gridRows = append(gridRows, row)
		if len(gridRows) >= rowsWanted {
			break
		}
	}
	grid := lipgloss.JoinVertical(lipgloss.Left, gridRows...)

	detail := v.detailStrip(width)
	if v.toast != "" {
		detail = styleWarn.Render(v.toast)
	}

	body := lipgloss.JoinVertical(lipgloss.Left, grid, "", detail)
	return shell(width, height, 0, body, h, k)
}

func (v *variantA) card(p Project, focused bool, termW int) string {
	status := styleStatusStop.Render("○ stopped")
	if p.Status == "running" {
		status = styleStatusRun.Render("● running")
	}
	name := p.Name
	if !p.SetupComplete {
		name += " (setup pending)"
	}
	inner := []string{
		name,
		status,
		styleMutedAlt.Render(shortEdition(p.Edition)),
		styleMutedAlt.Render("start " + p.StartOption),
		styleMutedAlt.Render("last " + p.LastUsed.Format("2006-01-02 15:04")),
	}
	cardStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(v.cardW - 2 - lipgloss.Width(lipgloss.RoundedBorder().TopLeft)) // border chars
	cardStyle = cardStyle.Width(v.cardW)
	if focused {
		cardStyle = cardStyle.BorderForeground(colAccent).Foreground(lipgloss.Color("255"))
	} else {
		cardStyle = cardStyle.BorderForeground(lipgloss.Color("238"))
	}
	return cardStyle.Render(strings.Join(inner, "\n"))
}

func (v *variantA) detailStrip(termW int) string {
	p := v.projects[v.focus]
	rows := []string{
		description("Name", p.Name),
		description("Edition", p.Edition, "opencode-sandbox-web", "opencode-sandbox-embedded", "opencode-sandbox-swdev", "opencode-sandbox-matlab", "opencode-sandbox-ros2", "opencode-sandbox-writing"),
		description("Modes", strings.Join(p.Modes, ", "), "offline", "hil_mode", "cbm_ui"),
		description("Start", p.StartOption, "console", "opencode", "web"),
		description("AI provider", p.AiProvider, "gwdg-saia", "none"),
		description("VCS tracking", p.VcsTracking, "none", "github.com", "gitlab.com", "own GitLab", "others"),
		description("Use proxy", boolLabel(p.UseProxy), "yes", "no"),
	}
	out := styleHeader.Render("Settings — " + p.Name + "  (current values highlighted; muted = clickable alternatives)")
	out += "\n" + strings.Join(rows, "\n")
	return lipgloss.NewStyle().MaxWidth(termW).Render(out)
}

func description(label string, current string, alts ...string) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("%-13s", label+" ")+" "+styleCurrentVal.Render("● "+current))
	for _, a := range alts {
		if a == current {
			continue
		}
		parts = append(parts, styleMutedAlt.Render("○ "+a))
	}
	return strings.Join(parts, "   ")
}

func boolLabel(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func shortEdition(e string) string {
	return "edition " + strings.TrimPrefix(e, "opencode-sandbox-")
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ help.Model
var _ key.Binding
