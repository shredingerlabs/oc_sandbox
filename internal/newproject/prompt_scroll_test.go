package newproject

// Regression tests for the scroll-height report: with no prebuilt
// container image, the build prompt's options (Build now / Build later /
// Go back) must stay clickable at small terminal heights where the card
// scrolls, and Go back must make the Create Project button visible again.

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// linesAt renders the windowed body at height h, ANSI stripped.
func linesAt(t *testing.T, m *Model, h int) []string {
	t.Helper()
	return strings.Split(shell.StripANSI(m.Body(80, h)), "\n")
}

// clickH synthesizes a left press on the token within the windowed render
// at height h (Body line k occupies screen row 3+k; click()).
func clickH(t *testing.T, m *Model, h int, token string) tea.Cmd {
	t.Helper()
	body := linesAt(t, m, h)
	for i, l := range body {
		off := tokenCol(l, token)
		if off < 0 {
			continue
		}
		x := shell.Width(l[:off])
		return m.Update(tea.MouseMsg{X: x, Y: 3 + i, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}
	t.Fatalf("token %q not in %d-row body:\n%s", token, h, strings.Join(body, "\n"))
	return nil
}

// TestBuildPromptClickableAtSmallHeight: entering the build prompt phase
// re-lands the cursor (fPrompt inserted before fCreate) and the follow
// must re-pull the option row into the window — clicking "Build later"
// runs the create tail and registers the project as stopped, at every
// height where the card scrolls (blank h20 = 80x24).
func TestBuildPromptClickableAtSmallHeight(t *testing.T) {
	for _, h := range []int{28, 26, 24, 22, 20} {
		t.Run(fmt.Sprintf("blank_h%d", h), func(t *testing.T) {
			testBuildPromptClick(t, srcBlank, h)
		})
		t.Run(fmt.Sprintf("url_h%d", h), func(t *testing.T) {
			testBuildPromptClick(t, srcURL, h)
		})
	}
}

func testBuildPromptClick(t *testing.T, source int, h int) {
	m, ops, reg := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) { withoutImage(run) })
	if source == srcURL {
		// switch to the From URL source tab, fill name, then tab to the
		// URL field and give it a repo URL (validation requires one).
		m.Update(shell.SubTabMsg{Index: srcURL})
		typeKeys(t, m, "deferred")
		m.Update(tea.KeyMsg{Type: tea.KeyTab}) // fName → fPath
		m.Update(tea.KeyMsg{Type: tea.KeyTab}) // fPath → fURL
		typeKeys(t, m, "https://github.com/user/deferred.git")
	} else {
		typeKeys(t, m, "deferred")
	}
	createViaKeyboard(t, m)
	if ops.total() != 0 {
		t.Fatalf("scripts before prompt: %v", ops.Calls)
	}
	b := strings.Join(linesAt(t, m, h), "\n")
	if !strings.Contains(b, "Build now") || !strings.Contains(b, "Build later") {
		t.Fatalf("build prompt not rendered at height %d:\n%s", h, b)
	}
	clickH(t, m, h, "Build later")
	if !ops.has("init-project.sh") {
		t.Fatalf("init not run after Build later: %v", ops.Calls)
	}
	p, err := reg.ByName("deferred")
	if err != nil || p.ContainerStatus != "stopped" {
		t.Fatalf("Build later must register stopped: %+v %v", p, err)
	}
}

// TestBuildPromptGoBackRestoresCreateButton: choosing Go back returns to
// the form phase and re-lands the cursor on fCreate — the Create Project
// button must be pulled back into the window at the same height.
func TestBuildPromptGoBackRestoresCreateButton(t *testing.T) {
	m, ops, _ := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) { withoutImage(run) })
	typeKeys(t, m, "deferred")
	createViaKeyboard(t, m)
	if ops.total() != 0 {
		t.Fatalf("scripts before prompt: %v", ops.Calls)
	}
	clickH(t, m, scrollH, "Go back") // resolving the prompt returns nil
	if m.phase != phaseForm {
		t.Fatalf("go back did not return to the form phase")
	}
	found := false
	for _, l := range linesAt(t, m, scrollH) {
		if strings.Contains(l, "Create Project") {
			found = true
		}
	}
	if !found {
		t.Fatalf("Create button not visible after go back at height %d:\n%s", scrollH, strings.Join(linesAt(t, m, scrollH), "\n"))
	}
}
