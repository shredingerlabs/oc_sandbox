package shell

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"
)

// Title is the header app title; menu items are fixed by the approved
// layout record (#72): the four center views, Exit last.
var (
	Title     = "oc-sandbox"
	MenuItems = []string{"Open Project", "New Project", "Container", "Settings", "Exit"}
)

// Screen rows owned by the shell: header (0), full-width separator (1),
// sub-menu row (2); the body follows from row 3, the footer is the last.
const (
	SubTabRowY = 2
	// BodyRows is the line count passed to CenterView.Body: rows 3..h-2.
	BodyRows = 4
)

// TitleWidth is the printed header title width; the first menu item and
// the sub-menu row both start at this x.
func TitleWidth() int { return Width(StyleHeaderTitle.Render(Title)) }

// MenuRects returns the screen-space hit boxes of the header menu items
// (row 0), one per MenuItems entry.
func MenuRects() []Rect {
	x := TitleWidth()
	out := make([]Rect, len(MenuItems))
	for i, item := range MenuItems {
		w := Width(item) + 2 // padding 0,1
		out[i] = Rect{X: x, Y: 0, W: w, H: 1}
		x += w
	}
	return out
}

// HeaderRow renders the title plus the menu, the active view as an
// accent pill, padded to the full width.
func HeaderRow(selected, width int) string {
	var items []string
	for i, item := range MenuItems {
		if i == selected && i < len(MenuItems)-1 {
			items = append(items, StyleHeaderItemSel.Render(item))
		} else {
			items = append(items, StyleHeaderItem.Render(item))
		}
	}
	title := StyleHeaderTitle.Render(Title)
	left := Width(title + strings.Join(items, ""))
	gap := width - left
	if gap < 1 {
		gap = 1
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(title + strings.Join(items, "") + strings.Repeat(" ", gap))
}

// SeparatorRow renders the full-width rule between header and sub-menu
// row (present in every view).
func SeparatorRow(width int) string {
	return StyleSeparator.Render(strings.Repeat("─", max(0, width-1)))
}

// SubTabsRow renders sub-menu tab pills (padding 0,1, accent when
// selected) and returns their real-pill-width hit rects on SubTabRowY.
func SubTabsRow(items []string, selected int) (string, []Rect) {
	st := lipgloss.NewStyle().Padding(0, 1)
	stSel := st.Background(ColAccent).Foreground(lipgloss.Color("0")).Bold(true)
	var b strings.Builder
	var offs []int
	x := 0
	for i, it := range items {
		offs = append(offs, x)
		s := st.Render(it)
		if i == selected {
			s = stSel.Render(it)
		}
		b.WriteString(s)
		x += Width(s)
	}
	rects := make([]Rect, len(items))
	for i, it := range items {
		rects[i] = Rect{X: TitleWidth() + offs[i], Y: SubTabRowY, W: Width(it) + 2, H: 1}
	}
	return b.String(), rects
}

// FooterRow renders the keyboard hint on the left and the bubbles help
// for the shell KeyMap on the right, one full-width line.
func FooterRow(width int, h help.Model, k KeyMap) string {
	hint := StyleFooter.Render("1 open · 2 new project · 3 container · 4 settings")
	helpView := h.View(k)
	lead := width - Width(hint) - Width(helpView)
	footer := hint
	if lead >= 1 {
		footer = hint + strings.Repeat(" ", lead) + helpView
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(footer)
}
