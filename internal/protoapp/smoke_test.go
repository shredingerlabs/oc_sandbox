package protoapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSmokeViews(t *testing.T) {
	root := NewRoot()
	root.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for v := 0; v < 4; v++ {
		root.view = v
		out := root.View()
		if out == "" {
			t.Fatal("empty view")
		}
	}
}
