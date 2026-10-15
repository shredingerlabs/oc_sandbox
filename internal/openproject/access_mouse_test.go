package openproject

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/sandboxcfg"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Mouse parity for the prompt-pane buttons (build prompt): the
// recorded hit rects (screen space) must line up with the rendered
// button middle lines, so a click there activates the option.
func TestBuildPromptMouseClickPicksOption(t *testing.T) {
	for _, w := range []int{80, 90, 100, 98, 120, 140} {
		e := newEnv(t, nil)
		e.m.state = stateBuild
		e.m.pending = e.m.projects[0]
		e.m.pendingCfg = *cfgOf(t, e.roots["alpha"])
		e.m.buildSel = 0
		lines := bodyOf(t, e.m, w, bodyH)
		btnRow, ok := findLine(lines, "Build now")
		if !ok {
			t.Fatalf("width %d: no button line rendered:\n%s", w, strings.Join(lines, "\n"))
		}
		if len(e.m.promptRects) != len(buildPromptOptions) {
			t.Fatalf("width %d: promptRects = %v", w, e.m.promptRects)
		}
		// the rect addresses the rendered label (alignment regression)
		if label := strings.TrimSpace(paneRowText(lines, e.m.promptRects[0])); !strings.HasPrefix(label, "│") {
			t.Fatalf("width %d: hit rect does not address the button line (got %q), rect=%v",
				w, label, e.m.promptRects[0])
		}
		// a click on the actual rendered position opens the build flow
		actualX := cellOf(lines[btnRow], "Build now")
		if actualX < 0 {
			t.Fatalf("width %d: button label not on its line: %q", w, lines[btnRow])
		}
		e.m.Update(tea.MouseMsg{X: actualX, Y: BodyTop + btnRow, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		if e.m.state != stateBusy {
			t.Fatalf("width %d: mouse click on the button did not start the build (x=%d)",
				w, actualX)
		}
	}
}

// cellOf returns the screen column (cell coordinate, ANSI/ASCII-safe) of
// the first occurrence of needle in a rendered line.
func cellOf(line, needle string) int {
	idx := strings.Index(line, needle)
	if idx < 0 {
		return -1
	}
	return shell.Width(line[:idx])
}

func cfgOf(t *testing.T, root string) *sandboxcfg.Config {
	t.Helper()
	cfg, err := sandboxcfg.Read(sandboxcfg.Path(root))
	if err != nil {
		t.Fatal(err)
	}
	return &cfg
}
