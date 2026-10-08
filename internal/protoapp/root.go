// Root model: view switcher + design switcher. THROWAWAY prototype
// (issue #70, second iteration on the #69 shell). Header menu items
// switch center-views (Open Project / New Project / Container); the
// floating pill cycles designs (structural takes on the two NEW
// center-views; Open Project is the #69 winner and stays fixed).
package protoapp

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// design is a structural take on the New Project + Container views.
type design interface {
	Name() string
	UpdateNP(msg tea.Msg, width, height int)
	ViewNP(width, height int, h help.Model, k keyMap) string
	UpdateCT(msg tea.Msg, width, height int)
	ViewCT(width, height int, h help.Model, k keyMap) string
}

// center views, matching headerMenuItems order
const (
	viewOpen = iota
	viewNew
	viewContainer
	viewSettings
)

type Root struct {
	width, height int
	open          *variantA
	set           *settings71
	designs       []design
	current       int // design index
	view          int // center view
	keys          keyMap
	help          help.Model
	switchDbl     dblClickTracker
}

func NewRoot() Root {
	return Root{
		open:    newVariantA(),
		set:     newSettings71(),
		designs: []design{newDesignA(), newDesignB(), newDesignC()},
		keys:    newKeys(),
		help:    help.New(),
	}
}

func (r *Root) Init() tea.Cmd { return nil }

func (r *Root) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		r.width, r.height = msg.Width, msg.Height
		return r, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "1":
			r.view = viewOpen
			return r, nil
		case "2":
			r.view = viewNew
			return r, nil
		case "3":
			r.view = viewContainer
			return r, nil
		case "4":
			r.view = viewSettings
			return r, nil
		}
		if key.Matches(msg, r.keys.SwitchVariant) {
			r.current = (r.current + 1) % len(r.designs)
			return r, nil
		}
		if key.Matches(msg, r.keys.Quit) {
			return r, tea.Quit
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			// header menu items on row 0 (click Open/New/Container to switch)
			if msg.Y == 0 {
				for i, m := range menuRects(r.width) {
					if m.hit(msg.X, msg.Y) {
						switch i {
						case viewOpen, viewNew, viewContainer:
							r.view = i
						case 3: // Settings
							r.view = viewSettings
						default: // Exit
							return r, tea.Quit
						}
						return r, nil
					}
				}
			}
			// Switcher pill hit-test: bottom row, right-aligned.
			pill := r.pillGeometry()
			if pill.hit(msg.X, msg.Y) {
				if r.switchDbl.press(msg) == 2 {
					// double-click on pill = quit (throwaway affordance)
					return r, tea.Quit
				}
				if msg.X < pill.midX {
					r.current = (r.current - 1 + len(r.designs)) % len(r.designs)
				} else {
					r.current = (r.current + 1) % len(r.designs)
				}
				return r, nil
			}
		}
	}
	// route the event to the active center view
	switch r.view {
	case viewNew:
		r.designs[r.current].UpdateNP(msg, r.width, r.height)
	case viewContainer:
		r.designs[r.current].UpdateCT(msg, r.width, r.height)
	case viewSettings:
		r.set.Update(msg, r.width, r.height)
	default:
		r.open.Update(msg, r.width, r.height)
	}
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
	label := r.open.Name()
	switch r.view {
	case viewSettings:
		label = r.set.Name()
	case viewNew, viewContainer:
		label = r.designs[r.current].Name()
	}
	return fmt.Sprintf(" ‹ design %d/%d: %s › ", r.current+1, len(r.designs), label)
}

func (r *Root) View() string {
	if r.width == 0 {
		return "resize…"
	}
	// Reserve the pill's width BEFORE rendering, so the shell can right-align
	// its footer help just short of the pill.
	pillLabel := r.pillLabel()
	footerReserve = lipgloss.Width(pillLabel)
	var body string
	switch r.view {
	case viewNew:
		body = r.designs[r.current].ViewNP(r.width, r.height, r.help, r.keys)
	case viewContainer:
		body = r.designs[r.current].ViewCT(r.width, r.height, r.help, r.keys)
	case viewSettings:
		body = r.set.View(r.width, r.height, r.help, r.keys)
	default:
		body = r.open.View(r.width, r.height, r.help, r.keys)
	}
	// Overwrite the footer's right end with the variant pill.
	lines := strings.Split(body, "\n")
	last := len(lines) - 1
	pillStyled := styleSwitcher.Render(pillLabel)
	start := r.width - lipgloss.Width(pillStyled)
	if start > 0 && last >= 0 {
		line := lines[last]
		if len(line) < r.width {
			line += strings.Repeat(" ", r.width-len(line))
		}
		// naive splice: strip ANSI would be needed for exactness; instead
		// rebuild: keep left part (plain width) + pill
		lines[last] = ansiCut(line, start) + pillStyled
	}
	return strings.Join(lines, "\n")
}

// ansiCut returns s truncated to n printable columns, keeping ANSI escapes
// balanced (the old byte-slice splice cut mid-escape and corrupted the help).
func ansiCut(s string, n int) string {
	var b strings.Builder
	inEsc := false
	cols := 0
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		if cols >= n {
			break
		}
		b.WriteRune(r)
		cols++
	}
	return b.String()
}

var styleSwitcher = lipgloss.NewStyle().
	Background(lipgloss.Color("130")).
	Foreground(lipgloss.Color("255")).
	Bold(true)
