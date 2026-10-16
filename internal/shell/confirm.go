package shell

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Confirm is the shared confirmation popup (ADR-0021): a centered modal
// over the body region confirming triggered actions before they run —
// Create Project (content summary), Restore Config, Build (idle only),
// and Uninstall (after the typed confirm; red confirm, toggles echoed).
//
// Focus model: focus starts on Cancel; left/right/tab switch buttons,
// Enter activates the focused button, esc/q cancels anytime. Pointer: a
// press on a button rect activates it, outside the panel cancels,
// inside the panel but on no button is absorbed. The owning view
// composites Overlay over its body and keeps its usual busy/no-op
// priorities.
type Confirm struct {
	Title string   // bold headline
	Lines []string // context lines under the title (already styled)

	run         func() tea.Cmd // continuation on confirm
	open        bool           // armed and shown
	selOk       bool           // false = Cancel focused (default)
	panel       Rect           // last Overlay geometry, screen space
	cancelRect  Rect
	okRect      Rect
	cancelLabel string
	okLabel     string
	danger      bool
}

// IsOpen reports whether a popup is armed (the shell key bypass queries this).
func (c *Confirm) IsOpen() bool { return c.open }

// Rects returns the popup's button hit rects of the last Overlay render
// (cancel first, confirm second) for harness tests; empty while closed.
func (c *Confirm) Rects() []Rect {
	if !c.open {
		return nil
	}
	return []Rect{c.cancelRect, c.okRect}
}

// Open arms the popup: title, confirm button label, cancel button label
// ("Back" for the Create summary), context lines, danger render (red
// confirm), and the continuation the confirm button produces.
func (c *Confirm) Open(title, okLabel, cancelLabel string, lines []string, danger bool, run func() tea.Cmd) {
	*c = Confirm{Title: title, Lines: lines, run: run, open: true,
		cancelLabel: cancelLabel, okLabel: okLabel, danger: danger}
}

// Close disarms the popup (cancelled).
func (c *Confirm) Close() { c.selOk, c.open = false, false }

// Key consumes a key while open: left/right/tab/shift+tab switch the
// button focus, Enter activates the focused button, esc/q cancels.
// Returns the run continuation on confirm, nil otherwise.
func (c *Confirm) Key(msg tea.KeyMsg) tea.Cmd {
	if !c.open {
		return nil
	}
	switch msg.String() {
	case "left", "right", "tab", "shift+tab":
		c.selOk = !c.selOk
	case "enter":
		if c.selOk {
			c.Close()
			if c.run != nil {
				return c.run()
			}
		}
		c.Close()
	case "esc", "q", "Q":
		c.Close()
	}
	return nil
}

// Click consumes a left-button press (screen space, rects of the last
// Overlay): on the confirm rect starts the guarded action, on the
// cancel rect closes, inside the panel but on no button absorbed,
// outside the panel cancels. Returns the produced cmd and whether the
// press was consumed.
func (c *Confirm) Click(x, y int) (tea.Cmd, bool) {
	if !c.open {
		return nil, false
	}
	switch {
	case c.okRect.Hit(x, y):
		run := c.run
		c.Close()
		if run == nil {
			return nil, true
		}
		return run(), true
	case c.cancelRect.Hit(x, y):
		c.Close()
		return nil, true
	case c.panel.Hit(x, y):
		return nil, true
	default:
		c.Close()
		return nil, true // click outside cancels
	}
}

// singleLineButton renders one button as exactly one line (the middle
// row of the bordered button) so the popup geometry is plain
// arithmetic; danger tints it red (unfocused border, focused fill) per
// the bash TUI's red UNINSTALL prompt. Focus changes fill, never width.
func singleLineButton(label string, focused, danger bool) string {
	if focused { // fill style inside the border, same width as unfocused
		bg, fg := lipgloss.Color("0"), ColAccent
		if danger {
			bg, fg = ColDanger, lipgloss.Color("0")
		}
		return "│" + lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(true).
			Width(Width(label)+4).Align(lipgloss.Center).Render(label) + "│"
	}
	if danger {
		return "│  " + lipgloss.NewStyle().Foreground(ColDanger).Render(label) + "  │"
	}
	return strings.Split(Button(label, false), "\n")[1]
}

// Overlay renders the centered panel over a body region spanning
// columns [0..width), starting at screen row `top` and `height` rows
// tall, recording the panel and button hit rects in screen space. The
// returned block is `height` full-width rows with the panel centered in
// it, so what renders is exactly what the rects address. A closed
// popup renders an empty string.
func (c *Confirm) Overlay(width, height, top int) string {
	if !c.open {
		return ""
	}

	cancel := singleLineButton(c.cancelLabel, !c.selOk, false)
	ok := singleLineButton(c.okLabel, c.selOk, c.danger)
	cancelW, okW := Width(cancel), Width(ok)
	const gap = 3
	btnW := cancelW + gap + okW

	maxw := btnW
	maxw = max(maxw, Width(" "+c.Title))
	for _, l := range c.Lines {
		maxw = max(maxw, Width(" "+l))
	}

	var rows []string
	rows = append(rows, " "+c.Title)
	for _, l := range c.Lines {
		rows = append(rows, " "+l)
	}
	rows = append(rows, strings.Repeat(" ", maxw))
	rows = append(rows, strings.Repeat(" ", maxw-btnW)+cancel+strings.Repeat(" ", gap)+ok)
	for i, r := range rows {
		if p := maxw - Width(r); p > 0 {
			rows[i] = r + strings.Repeat(" ", p)
		}
	}
	pw := maxw + 2
	px := max(0, (width-pw)/2)
	py := top + max(0, (height-len(rows))/2)
	c.panel = Rect{X: px, Y: py, W: pw, H: len(rows) + 2}
	buttonY := py + len(rows) // content row of the button (top border above)
	innerX := px + 1          // border column; content column follows
	c.cancelRect = Rect{X: innerX + (maxw - btnW), Y: buttonY, W: cancelW, H: 1}
	c.okRect = Rect{X: innerX + (maxw - btnW) + cancelW + gap, Y: buttonY, W: okW, H: 1}

	// the border chords the padded rows together; card colors make the
	// panel read as a separate layer over the body. Painted per line
	// (paint.go): the buttons' SGR resets would drop a wrapping style's
	// card background halfway through the panel.
	b := lipgloss.RoundedBorder()
	bordered := []string{b.TopLeft + strings.Repeat(b.Top, maxw+2) + b.TopRight}
	for _, row := range rows {
		bordered = append(bordered, b.Left+row+b.Right)
	}
	bordered = append(bordered, b.BottomLeft+strings.Repeat(b.Bottom, maxw+2)+b.BottomRight)
	panel := paintLines(strings.Join(bordered, "\n"),
		lipgloss.NewStyle().Background(ColCardBg).Foreground(ColCardFg), maxw+2)

	// compose the body block: the panel centered, everything else blank
	panelLines := strings.Split(panel, "\n")
	var out []string
	for y := top; y < top+height; y++ {
		if y < py || y >= py+len(panelLines) {
			out = append(out, strings.Repeat(" ", width))
			continue
		}
		lead := px
		if space := width - Width(panelLines[y-py]); space < lead {
			lead = space // clip instead of painting past the region
		}
		if lead > 0 {
			lead = max(0, lead)
			out = append(out, strings.Repeat(" ", lead)+panelLines[y-py])
		} else {
			out = append(out, panelLines[y-py])
		}
	}
	return strings.Join(out, "\n")
}
