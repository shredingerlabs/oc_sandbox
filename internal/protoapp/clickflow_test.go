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
	root.View() // render -> rects stored as a mouse would see them
	d := root.npct
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
	if !d.ctBtn.hit(d.ctBtn.x+1, d.ctBtn.y) {
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
	click(root, d.ctBtn.x+1, d.ctBtn.y)
	names := d.ct.selectedNames(fakeRunning())
	if len(names) != 0 {
		t.Fatal("stop rows/select-all interaction broken")
	}
}

func TestNPOptionClick(t *testing.T) {
	// regression: clicking option tokens used to panic on stale rects
	root := NewRoot()
	root.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	root.view = viewNew
	root.View() // 1st layout
	root.View() // 2nd layout on top (previously accumulated rects -> panic)
	d := root.npct
	s := d.np
	// click "yes" in the Use proxy row (fixed order: no=●, yes=○)
	for _, or := range d.optionRects {
		id := s.fields[or.fi]
		if id == fProxy && or.val == "yes" {
			click(root, or.r.x+1, or.r.y)
		}
	}
	if !s.proxy {
		t.Fatal("option click did not activate 'yes'")
	}
	// activate a specific VCS option by clicking its token
	for _, or := range d.optionRects {
		id := s.fields[or.fi]
		if id == fVCS && or.val == "gitlab.com" {
			click(root, or.r.x+1, or.r.y)
		}
	}
	if s.vcs != "gitlab.com" {
		t.Fatalf("vcs=%q", s.vcs)
	}
	// option order intact: none still first in VCS list
	if first := optionList(fVCS)[0]; first != "none" {
		t.Fatalf("order changed: %q", first)
	}
}

func TestNPPathEditable(t *testing.T) {
	root := NewRoot()
	root.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	root.view = viewNew
	root.View()
	d := root.npct
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
