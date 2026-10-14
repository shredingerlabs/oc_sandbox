package openproject

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Mouse parity for the prompt-pane buttons (access/build prompt): the
// recorded hit rects (screen space) must line up with the rendered
// button middle lines, so a click there activates the option.
func TestAccessPromptMouseClickStartsHandoff(t *testing.T) {
	for _, w := range []int{80, 90, 100, 98, 120, 140} {
		var execed []string
		e := newEnv(t, nil)
		e.m = e.buildWithExec(t, &execed)
		e.m.state = stateAccess
		e.m.pending = e.m.projects[0]
		e.m.accessOpts = []string{"Console (bash)"}
		e.m.accessSel = 0
		lines := bodyOf(t, e.m, w, bodyH)
		btnRow, ok := findLine(lines, "Console (bash)")
		if !ok {
			t.Fatalf("width %d: no button line rendered:\n%s", w, strings.Join(lines, "\n"))
		}
		if len(e.m.promptRects) != 1 {
			t.Fatalf("width %d: promptRects = %v", w, e.m.promptRects)
		}
		// the rect addresses the rendered label (alignment regression)
		if label := strings.TrimSpace(paneRowText(lines, e.m.promptRects[0])); !strings.HasPrefix(label, "│") {
			t.Fatalf("width %d: hit rect does not address the button line (got %q), rect=%v",
				w, label, e.m.promptRects[0])
		}
		// a click on the actual rendered position starts the handoff
		actualX := strings.Index(lines[btnRow], "│")
		e.m.Update(tea.MouseMsg{X: actualX, Y: BodyTop + btnRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if len(execed) != 1 {
			t.Fatalf("width %d: mouse click on the button did not start the handoff (x=%d)",
				w, actualX)
		}
	}
}
