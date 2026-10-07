// Root model: variant switcher + shared state. THROWAWAY prototype (issue #69).
// The floating switcher (bottom-right) is the TUI equivalent of the
// ?variant= URL param from the prototype skill: cycles with V / ←→ clicks,
// always visible, visually distinct from the design being evaluated.
package protoapp

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type variant interface {
	Name() string
	Update(msg tea.Msg, width, height int)
	View(width, height int, h help.Model, k keyMap) string
}

type Root struct {
	width, height int
	variants      []variant
	current       int
	keys          keyMap
	help          help.Model
	switchDbl     dblClickTracker
}

func NewRoot() Root {
	return Root{
		variants: []variant{newVariantA(), newVariantB(), newVariantC()},
		keys:     newKeys(),
		help:     help.New(),
	}
}

func (r *Root) Init() tea.Cmd { return nil }

func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width, r.height = msg.Width, msg.Height
		return r, nil
	case tea.KeyMsg:
		if key.Matches(msg, r.keys.SwitchVariant) {
			r.current = (r.current + 1) % len(r.variants)
			return r, nil
		}
		if key.Matches(msg, r.keys.Quit) {
			return r, tea.Quit
		}
	case tea.MouseMsg:
		// Switcher pill hit-test: bottom two rows, right-aligned.
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			pill := r.pillGeometry()
			if pill.hit(msg.X, msg.Y) {
				if r.switchDbl.press(msg) == 2 {
					// double-click on pill = quit (throwaway affordance)
					return r, tea.Quit
				}
				if msg.X < pill.midX {
					r.current = (r.current - 1 + len(r.variants)) % len(r.variants)
				} else {
					r.current = (r.current + 1) % len(r.variants)
				}
				return r, nil
			}
		}
	}
	r.variants[r.current].Update(msg, r.width, r.height)
	return r, nil
}

type pill struct {
	x, y, w, h, midX int
}

func (p pill) hit(x, y int) bool {
	return y >= p.y && y < p.y+p.h && x >= p.x && x < p.x+p.w
}

func (r *Root) pillGeometry() pill {
	label := r.pillLabel()
	w := lipgloss.Width(label)
	h := 1
	// drawn on the footer's last row, right-aligned with padding
	return pill{x: r.width - w - 1, y: r.height - 1, w: w, h: h, midX: r.width - w/2 - 1}
}

func (r *Root) pillLabel() string {
	return fmt.Sprintf(" ‹ %d/3: %s › ", r.current+1, r.variants[r.current].Name())
}

func (r *Root) View() string {
	if r.width == 0 {
		return "resize…"
	}
	body := r.variants[r.current].View(r.width, r.height, r.help, r.keys)
	// Overwrite the footer's right end with the variant pill.
	lines := strings.Split(body, "\n")
	last := len(lines) - 1
	pillLabel := styleSwitcher.Render(r.pillLabel())
	start := r.width - lipgloss.Width(pillLabel)
	if start > 0 && last >= 0 {
		line := lines[last]
		if len(line) < r.width {
			line += strings.Repeat(" ", r.width-len(line))
		}
		// naive splice: strip ANSI would be needed for exactness; instead
		// rebuild: keep left part (plain width) + pill
		lines[last] = line[:maxInt(0, start)] + pillLabel
	}
	return strings.Join(lines, "\n")
}

var styleSwitcher = lipgloss.NewStyle().
	Background(lipgloss.Color("130")).
	Foreground(lipgloss.Color("255")).
	Bold(true)
