package protoapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func click(root Root, x, y int) {
	root.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

func TestCClickFlow(t *testing.T) {
	root := NewRoot()
	root.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	root.view = viewContainer
	root.current = 2
	root.View() // render -> rects stored as a mouse would see them
	d := root.designs[2].(*designC)
	// select chip 0 on the Build pane
	c0 := d.chipRects[0]
	click(root, c0.x+2, c0.y+2)
	if d.buildSel != 0 {
		t.Fatal("chip click did not select")
	}
	// double-click the same chip -> build toast
	click(root, c0.x+2, c0.y+2)
	if d.ct.building != editions[0] {
		t.Fatal("double click did not build")
	}
	// Build button
	if !d.ctBtn.hit(d.ctBtn.x+1, d.ctBtn.y+1) {
		t.Fatal("build button rect off")
	}
	// switch to Stop pane via stored tab rect
	root.View()
	tr := d.ctTabsRects
	click(root, tr[1].x+1, tr[1].y)
	if d.ctPane != 1 {
		t.Fatal("stop tab unreachable")
	}
	root.View()
	click(root, d.selAll.x+2, d.selAll.y)
	if len(d.ct.selectedNames(fakeRunning())) != len(fakeRunning()) {
		t.Fatal("select-all rect off")
	}
	click(root, d.stopRects[0].x+2, d.stopRects[0].y)
	click(root, d.ctBtn.x+1, d.ctBtn.y+1)
	names := d.ct.selectedNames(fakeRunning())
	if len(names) != 0 {
		t.Fatal("stop rows/select-all interaction broken")
	}
}

func TestNPPathEditable(t *testing.T) {
	root := NewRoot()
	root.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	root.view = viewNew
	root.current = 2
	root.View()
	d := root.designs[2].(*designC)
	s := d.np
	s.focus = 1 // fPath
	s.cursor = len(s.path)
	for _, r := range "x" {
		root.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if s.path != pathBase+"x" {
		t.Fatalf("path=%q", s.path)
	}
	if !s.pathManual {
		t.Fatal("path not marked manual")
	}
}
