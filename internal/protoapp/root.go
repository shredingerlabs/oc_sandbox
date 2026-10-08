// Root model: view switcher. THROWAWAY prototype (issue #70/#71, feedback
// 3: single winning design — Open Project (variant A), New Project +
// Container (design C) — with Settings; design pill removed).
package protoapp

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

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
	npct          *designC
	settings      *settings71
	view          int
	keys          keyMap
	help          help.Model
}

func NewRoot() Root {
	return Root{
		open: newVariantA(),
		npct: newDesignC(),
		keys: newKeys(),
		help: help.New(),
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
						case viewOpen, viewNew, viewContainer, viewSettings:
							r.view = i
						default: // Exit
							return r, tea.Quit
						}
						return r, nil
					}
				}
			}
		}
	}
	// route the event to the active center view
	switch r.view {
	case viewNew:
		r.npct.UpdateNP(msg, r.width, r.height)
	case viewContainer:
		r.npct.UpdateCT(msg, r.width, r.height)
	case viewSettings:
		r.setInstance().Update(msg, r.width, r.height)
	default:
		r.open.Update(msg, r.width, r.height)
	}
	return r, nil
}

// setInstance lazily creates the settings model so NewRoot stays cheap.
func (r *Root) setInstance() *settings71 {
	if r.settings == nil {
		r.settings = newSettings71()
	}
	return r.settings
}

func (r *Root) View() string {
	if r.width == 0 {
		return "resize…"
	}
	var body string
	switch r.view {
	case viewNew:
		body = r.npct.ViewNP(r.width, r.height, r.help, r.keys)
	case viewContainer:
		body = r.npct.ViewCT(r.width, r.height, r.help, r.keys)
	case viewSettings:
		body = r.setInstance().View(r.width, r.height, r.help, r.keys)
	default:
		body = r.open.View(r.width, r.height, r.help, r.keys)
	}
	return body
}
