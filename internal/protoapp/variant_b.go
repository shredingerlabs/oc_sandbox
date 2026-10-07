// Variant B: two-column master/detail (no card grid). THROWAWAY prototype
// (issue #69). Left: flat navigable project list, dense, one line each.
// Right: full-height details/settings pane for the selected project.
// Tests: does the master/detail split beat cards for density?
package protoapp

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type variantB struct {
	projects []Project
	focus    int
	dbl      dblClickTracker
	toast    string
}

func newVariantB() *variantB {
	return &variantB{projects: fakeProjects()}
}

func (v *variantB) Name() string { return "B split" }

func (v *variantB) Update(msg tea.Msg, width, height int) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "down", "j":
			v.focus = (v.focus + 1) % len(v.projects)
		case "up", "k":
			v.focus = (v.focus - 1 + len(v.projects)) % len(v.projects)
		case "enter":
			v.toast = fmt.Sprintf("▶ start %s (fake)", v.projects[v.focus].Name)
		}
	case tea.MouseMsg:
		n := v.dbl.press(msg)
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if idx, hit := v.hitList(msg.X, msg.Y); hit {
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

func (v *variantB) hitList(x, y int) (int, bool) {
	// list starts at body row 0, item i at row i
	if x < 0 || x > 36 || y < 0 {
		return 0, false
	}
	if y < len(v.projects) {
		return y, true
	}
	return 0, false
}

func (v *variantB) View(width, height int, h help.Model, k keyMap) string {
	bodyH := height - 2
	listW := 36
	detailW := width - listW - 1
	if detailW < 20 {
		detailW = 20
	}

	var list []string
	for i, p := range v.projects {
		status := styleStatusStop.Render("○")
		if p.Status == "running" {
			status = styleStatusRun.Render("●")
		}
		line := fmt.Sprintf("%s %-14s %s", status, p.Name, styleMutedAlt.Render(shortEdition(p.Edition)))
		if !p.SetupComplete {
			line += " " + styleWarn.Render("!")
		}
		if i == v.focus {
			line = styleSelectedRow.Width(listW).Render(line)
		}
		list = append(list, lipgloss.NewStyle().MaxWidth(listW).Render(line))
	}
	listView := lipgloss.JoinVertical(lipgloss.Left, list...)

	p := v.projects[v.focus]
	var rows []string
	rows = append(rows, styleCurrentVal.Render(p.Name))
	rows = append(rows, settingsBlock("Status", p.Status, "running", "stopped"))
	rows = append(rows, settingsBlock("Edition", shortEdition(p.Edition),
		"edition web", "edition embedded", "edition swdev",
		"edition matlab", "edition ros2", "edition writing"))
	rows = append(rows, settingsBlock("Modes", modesLabel(p.Modes), "none", "offline", "hil_mode", "cbm_ui"))
	rows = append(rows, settingsBlock("Start", p.StartOption, "console", "opencode", "web"))
	rows = append(rows, settingsBlock("AI provider", p.AiProvider, "gwdg-saia", "none"))
	rows = append(rows, settingsBlock("VCS tracking", p.VcsTracking, "none", "github.com", "gitlab.com", "own GitLab", "others"))
	rows = append(rows, settingsBlock("Use proxy", boolLabel(p.UseProxy), "yes", "no"))
	rows = append(rows, settingsBlock("Setup", setupLabel(p.SetupComplete)))
	rows = append(rows, "", styleMutedAlt.Render("path: "+p.Path))
	rows = append(rows, styleMutedAlt.Render("last used: "+p.LastUsed.Format("2006-01-02 15:04")))
	if v.toast != "" {
		rows = append(rows, "", styleWarn.Render(v.toast))
	}
	detail := lipgloss.JoinVertical(lipgloss.Left, rows...)
	detail = lipgloss.NewStyle().MaxWidth(detailW).MaxHeight(bodyH).Render(detail)

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(listW).MaxHeight(bodyH).Render(listView),
		lipgloss.NewStyle().MaxHeight(bodyH).Render(detail),
	)
	return shell(width, height, 0, body, h, k)
}

func setupLabel(done bool) string {
	if done {
		return "complete"
	}
	return "pending"
}
