package protoapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSmokeViews(t *testing.T) {
	root := NewRoot()
	root.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for v := 0; v < 3; v++ {
		for d := 0; d < 3; d++ {
			root.view = v
			root.current = d
			out := root.View()
			if out == "" {
				t.Fatal("empty view")
			}
		}
	}
}
