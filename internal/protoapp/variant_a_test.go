package protoapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Regression for the click-offset feedback on #69: a press at screen cell
// (x,y) must select the card under the cursor. Screen row 0 is the shell
// header; the grid starts at screen row y=1.
func TestHitCard(t *testing.T) {
	root := NewRoot()
	root.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	root.View() // settles geometry (cols) for the size
	click := func(x, y int) {
		root.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}
	// 80x24: gridW = 38 -> one card column, settings pane on the right.
	click(10, 3) // card row 0
	va := root.open
	if va.focus != 0 {
		t.Errorf("click (10,3): focus=%d, want 0", va.focus)
	}
	click(10, 10) // localRow=(10-1)/7=1 -> second card
	if va.focus != 1 {
		t.Errorf("click (10,10): focus=%d, want 1", va.focus)
	}
	click(45, 5) // settings pane: must not move focus
	if va.focus != 1 {
		t.Errorf("click on settings pane: focus=%d, want 1", va.focus)
	}
	click(2, 0) // header row: must not move focus
	if va.focus != 1 {
		t.Errorf("click on header: focus=%d, want 1", va.focus)
	}
	click(10, 3) // single press: select
	click(10, 3) // second press: double -> start toast
	if va.focus != 0 {
		t.Errorf("double click (10,3): focus=%d, want 0", va.focus)
	}
	if va.toast == "" {
		t.Errorf("double click should set the start toast")
	}
}
