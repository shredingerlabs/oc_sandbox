// Variant C: table + inline settings drawer. THROWAWAY prototype (issue #69).
// Top: full-width dense table (one row per project, columns for the fields
// people scan). Bottom: expandable settings drawer for the selected row.
// Tests: does a scannable table beat spatial layouts? Does the drawer feel
// cramped or natural?
package protoapp

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type variantC struct {
	projects  []Project
	focus     int
	drawer    bool
	drawerRow int
	dbl       dblClickTracker
	toast     string
}

func newVariantC() *variantC {
	return &variantC{drawer: true, projects: fakeProjects()}
}

func (v *variantC) Name() string { return "C table" }

func (v *variantC) Update(msg tea.Msg, width, height int) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "down", "j":
			v.focus = (v.focus + 1) % len(v.projects)
		case "up", "k":
			v.focus = (v.focus - 1 + len(v.projects)) % len(v.projects)
		case "enter":
			v.toast = fmt.Sprintf("▶ start %s (fake)", v.projects[v.focus].Name)
		case "s":
			v.drawer = !v.drawer
		}
	case tea.MouseMsg:
		n := v.dbl.press(msg)
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if idx, hit := v.hitRow(msg.Y); hit {
				if n == 2 {
					v.focus = idx
					v.toast = fmt.Sprintf("▶ start %s (fake double-click)", v.projects[idx].Name)
				} else if idx != v.focus {
					v.focus = idx
					v.toast = ""
				}
			}
		}
	}
}

func (v *variantC) hitRow(y int) (int, bool) {
	// header line at body row 0; rows start at 1
	if y < 1 {
		return 0, false
	}
	idx := y - 1
	if idx < len(v.projects) {
		return idx, true
	}
	return 0, false
}

func (v *variantC) View(width, height int, h help.Model, k keyMap) string {
	bodyH := height - 2

	// Table columns: status, name, edition, start, modes, last used.
	var lines []string
	lines = append(lines, styleHeader.Render(fmt.Sprintf("%-3s %-14s %-10s %-9s %-18s %s",
		"", "NAME", "EDITION", "START", "MODES", "LAST USED")))
	for i, p := range v.projects {
		status := styleStatusStop.Render("○")
		if p.Status == "running" {
			status = styleStatusRun.Render("●")
		}
		line := fmt.Sprintf("%-3s %-14s %-10s %-9s %-18s %s",
			status,
			p.Name,
			shortEdition(p.Edition),
			p.StartOption,
			strings.Join(p.Modes, ","),
			p.LastUsed.Format("2006-01-02"),
		)
		if !p.SetupComplete {
			line += " " + styleWarn.Render("!")
		}
		if i == v.focus {
			line = styleSelectedRow.Width(width).Render(line)
		}
		lines = append(lines, lipgloss.NewStyle().MaxWidth(width).Render(line))
	}

	table := lipgloss.JoinVertical(lipgloss.Left, lines...)

	if v.drawer {
		p := v.projects[v.focus]
		var rows []string
		rows = append(rows, styleHeader.Render(fmt.Sprintf("Settings — %s  (s toggles this drawer)", p.Name)))
		rows = append(rows, settingsBlock("Edition", shortEdition(p.Edition),
			"edition web", "edition embedded", "edition swdev",
			"edition matlab", "edition ros2", "edition writing"))
		rows = append(rows, settingsBlock("Modes", modesLabel(p.Modes), "none", "offline", "hil_mode", "cbm_ui"))
		rows = append(rows, settingsBlock("Start", p.StartOption, "console", "opencode", "web"))
		rows = append(rows, settingsBlock("AI provider", p.AiProvider, "gwdg-saia", "none"))
		rows = append(rows, settingsBlock("VCS tracking", p.VcsTracking, "none", "github.com", "gitlab.com", "own GitLab", "others"))
		rows = append(rows, settingsBlock("Use proxy", boolLabel(p.UseProxy), "yes", "no"))
		if v.toast != "" {
			rows = append(rows, styleWarn.Render(v.toast))
		}
		drawer := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), true, false, false).
			BorderForeground(lipgloss.Color("238")).
			MaxWidth(width).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
		table = lipgloss.JoinVertical(lipgloss.Left, table, drawer)
	}

	body := lipgloss.NewStyle().MaxHeight(bodyH).Render(table)
	return shell(width, height, 0, body, h, k)
}
