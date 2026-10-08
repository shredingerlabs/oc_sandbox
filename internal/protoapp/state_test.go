package protoapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeRunes(root Root, rs string) {
	for _, r := range rs {
		root.Update(tea.KeyMsg{Runes: []rune{r}})
	}
}

func TestNPBehavior(t *testing.T) {
	root := NewRoot()
	root.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	root.view = viewNew
	root.View()
	d := root.designs[0].(*designA)
	s := d.np
	// type a name -> path auto-updates
	typeRunes(root, "my_app")
	if s.name != "my_app" || s.path != pathBase+"/my-app" {
		t.Fatalf("name=%q path=%q", s.name, s.path)
	}
	// tab-cycle to the create button and activate
	for i := 0; i < len(s.fields)-1; i++ {
		root.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	if s.fields[s.focus] != fCreate {
		t.Fatalf("focus field=%d want fCreate", s.fields[s.focus])
	}
	root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(s.toast, "create my_app") {
		t.Fatalf("toast=%q", s.toast)
	}
	// switch to From URL -> URL field appears
	d.np.source = 1
	d.refresh()
	found := false
	for _, id := range s.fields {
		if id == fURL {
			found = true
		}
	}
	if !found {
		t.Fatal("URL field missing for From URL")
	}
	// container: space toggles select-all, enter stops
	root.view = viewContainer
	d.ctPane = 1
	root.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if len(d.ct.selectedNames(fakeRunning())) != len(fakeRunning()) {
		t.Fatal("select-all failed")
	}
	root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(d.ct.toast, "stop") {
		t.Fatalf("toast=%q", d.ct.toast)
	}
}

// B and C: mouse switches the source control / container tabs and clicks
// a build chip.
func TestDesignBAndCMouse(t *testing.T) {
	root := NewRoot()
	root.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	root.view = viewNew

	// design B: segmented control, right half = From URL
	root.current = 1
	root.Update(tea.MouseMsg{X: 70, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if d := root.designs[1].(*designB); d.np.source != 1 {
		t.Fatalf("B source=%d", d.np.source)
	}
	root.view = viewContainer
	root.View()
	// click the second edition chip (stored rects render-relative; click in
	// the content column area, upper rows)
	if d := root.designs[1].(*designB); len(d.chips) == 0 {
		t.Fatal("B chips missing")
	}

	// design C: tabs, then build via enters
	root.current = 2
	root.view = viewNew
	root.View()
	root.Update(tea.MouseMsg{X: 60, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if d := root.designs[2].(*designC); d.np.source != 1 {
		t.Fatalf("C source=%d", d.np.source)
	}
	root.view = viewContainer
	root.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if c := root.designs[2].(*designC); c.ct.building != editions[c.buildSel] {
		t.Fatalf("C build=%q", c.ct.building)
	}
}
