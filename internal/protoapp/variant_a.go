// Variant A (hybrid): card grid LEFT, settings pane RIGHT. THROWAWAY
// prototype (issue #69). Grid geometry adapted from exampleCards.go
// (fixed card size, cols = gridW/cardW, keeps selected row visible);
// inner card text/styling from variant A; left/right positioning from
// variant B. Single click selects; double-click / Enter starts.
package protoapp

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	cardW         = 30 // total card width including border
	cardH         = 7  // total card height including border (5 content lines)
	settingsPaneW = 42 // width of the right-hand settings pane
)

type variantA struct {
	projects []Project
	focus    int
	cols     int
	offRow   int
	dbl      dblClickTracker
	toast    string
}

func newVariantA() *variantA {
	return &variantA{projects: fakeProjects()}
}

func (v *variantA) Name() string { return "A cards" }

// geom mirrors exampleCards.go: fixed card size -> cols = gridW/cardW,
// visible rows = bodyH/cardH.
func (v *variantA) geom(width, height int) (gridW, cols, visRows int) {
	bodyH := height - 2
	gridW = maxInt(1, width-settingsPaneW)
	cols = maxInt(1, gridW/cardW)
	visRows = maxInt(1, bodyH/cardH)
	return gridW, cols, visRows
}

func (v *variantA) Update(msg tea.Msg, width, height int) {
	_, cols, visRows := v.geom(width, height)
	v.cols = cols
	totalRows := (len(v.projects) + cols - 1) / cols
	v.offRow = min(v.offRow, maxInt(0, totalRows-visRows))
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
			if idx, hit := v.hitCard(msg.X, msg.Y, width, height); hit {
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

// hitCard maps screen coordinates to a card. Screen row 0 is the shell
// header; the grid starts at screen row y=1 (body row 0), one card per
// cardH lines, cards flow left-to-right, wrapping on cols.
func (v *variantA) hitCard(x, y, width, height int) (int, bool) {
	_, cols, _ := v.geom(width, height)
	if y < 1 {
		return 0, false
	}
	col := x / cardW
	if col >= cols {
		return 0, false
	}
	localRow := (y - 1) / cardH
	idx := (v.offRow+localRow)*cols + col
	if idx >= 0 && idx < len(v.projects) {
		return idx, true
	}
	return 0, false
}

func (v *variantA) View(width, height int, h help.Model, k keyMap) string {
	gridW, cols, visRows := v.geom(width, height)
	v.cols = cols
	bodyH := height - 2
	totalRows := (len(v.projects) + cols - 1) / cols
	if v.offRow > totalRows-visRows {
		v.offRow = maxInt(0, totalRows-visRows)
	}
	// Keep the focused card's row visible (like exampleCards.go).
	row := v.focus / cols
	if row < v.offRow {
		v.offRow = row
	} else if row >= v.offRow+visRows {
		v.offRow = row - visRows + 1
	}

	var gridRows []string
	endRow := min(totalRows, v.offRow+visRows)
	for r := v.offRow; r < endRow; r++ {
		var cards []string
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= len(v.projects) {
				break
			}
			cards = append(cards, v.card(v.projects[i], i == v.focus))
		}
		gridRows = append(gridRows, lipgloss.JoinHorizontal(lipgloss.Top, cards...))
	}
	grid := lipgloss.JoinVertical(lipgloss.Left, gridRows...)

	settings := v.settingsPane()
	if v.toast != "" {
		// Insert under the pane header so MaxHeight can't clip it off.
		parts := strings.SplitN(settings, "\n", 2)
		settings = parts[0] + "\n" + styleWarn.Render(v.toast)
		if len(parts) > 1 {
			settings += "\n" + parts[1]
		}
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(gridW).Height(bodyH).MaxHeight(bodyH).Render(grid),
		lipgloss.NewStyle().Width(settingsPaneW-1).Height(bodyH).MaxHeight(bodyH).PaddingLeft(1).Render(settings),
	)
	return shell(width, height, 0, body, h, k)
}

func (v *variantA) card(p Project, focused bool) string {
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
	st := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(cardW - 2).  // lipgloss Width excludes the border
		Height(cardH - 2). // same for Height
		MaxHeight(cardH)
	if focused {
		st = st.BorderForeground(colAccent)
	} else {
		st = st.BorderForeground(lipgloss.Color("240"))
	}
	return st.Render(strings.Join(inner, "\n"))
}

func (v *variantA) settingsPane() string {
	p := v.projects[v.focus]
	blocks := []string{
		styleHeader.Render("Settings — " + p.Name + "  (muted = alternatives)"),
		settingsBlock("Edition", shortEdition(p.Edition),
			"edition web", "edition embedded", "edition swdev",
			"edition matlab", "edition ros2", "edition writing"),
		settingsBlock("Modes", modesLabel(p.Modes), "none", "offline", "hil_mode", "cbm_ui"),
		settingsBlock("Start", p.StartOption, "console", "opencode", "web"),
		settingsBlock("AI provider", p.AiProvider, "gwdg-saia", "none"),
		settingsBlock("VCS tracking", p.VcsTracking, "none", "github.com", "gitlab.com", "own GitLab", "others"),
		settingsBlock("Use proxy", boolLabel(p.UseProxy), "yes", "no"),
	}
	return strings.Join(blocks, "\n")
}

func modesLabel(m []string) string {
	if len(m) == 0 {
		return "none"
	}
	return strings.Join(m, ", ")
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
