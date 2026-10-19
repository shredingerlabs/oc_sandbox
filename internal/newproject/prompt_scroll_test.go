package newproject

// Regression tests for the scroll-height report: while the selected
// edition has no container image, the inline "Image build" radio row
// (Build now / Build later, directly under Editions) must stay clickable
// at small terminal heights where the card scrolls, and the Create
// Project button stays at the bottom of the card, reachable by focus.

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

// TestBuildRowClickableAtSmallHeight: the "Image build" radio row sits
// directly under the Editions row while the image is missing; clicking
// "Build later" records the choice and Create (reached by focus-follow at
// the small height) runs the create tail and registers the project as
// stopped (blank h20 = 80x24).
func TestBuildRowClickableAtSmallHeight(t *testing.T) {
	for _, h := range []int{28, 26, 24, 22, 20} {
		t.Run(fmt.Sprintf("blank_h%d", h), func(t *testing.T) {
			testBuildRowClick(t, srcBlank, h)
		})
		t.Run(fmt.Sprintf("url_h%d", h), func(t *testing.T) {
			testBuildRowClick(t, srcURL, h)
		})
	}
}

func testBuildRowClick(t *testing.T, source int, h int) {
	m, ops, reg := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) { withoutImage(run) })
	if source == srcURL {
		// switch to the From URL source tab: the Repo URL row is first, so
		// focus already sits there; type the repo URL (name/path prefills
		// from it, and validation requires one).
		m.Update(shell.SubTabMsg{Index: srcURL})
		typeKeys(t, m, "https://github.com/user/deferred.git")
	} else {
		typeKeys(t, m, "deferred")
	}
	b := strings.Join(linesAt(t, m, h), "\n")
	if !strings.Contains(b, "Image build") || !strings.Contains(b, "Build now") || !strings.Contains(b, "Build later") {
		t.Fatalf("build radio row not rendered at height %d:\n%s", h, b)
	}
	edIdx := strings.Index(b, "Edition")
	buildIdx := strings.Index(b, "Image build")
	if !(edIdx >= 0 && edIdx < buildIdx) {
		t.Fatalf("Image build row not under Editions at height %d:\n%s", h, b)
	}
	clickH(t, m, h, "Build later") // record the radio choice
	createViaKeyboard(t, m)
	if !ops.has("init-project.sh") {
		t.Fatalf("init not run after Build later: %v", ops.Calls)
	}
	p, err := reg.ByName("deferred")
	if err != nil || p.ContainerStatus != "stopped" {
		t.Fatalf("Build later must register stopped: %+v %v", p, err)
	}
}

// TestCreateButtonVisibleWithBuildRowAtSmallHeight: even with the extra
// "Image build" row the Create Project button stays at the bottom of the
// card — tabbing to it scrolls the card so the focused button is in the
// window (checked in the height's own render, where the follow pulls).
func TestCreateButtonVisibleWithBuildRowAtSmallHeight(t *testing.T) {
	m, _, _ := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) { withoutImage(run) })
	typeKeys(t, m, "deferred")
	for i := 0; i < 20; i++ {
		m.Update(tea.KeyMsg{Type: tea.KeyTab})
		for _, l := range linesAt(t, m, scrollH) {
			if strings.Contains(l, "▸ Create Project") {
				return
			}
		}
	}
	t.Fatalf("Create button not reachable with build row present at height %d:\n%s",
		scrollH, strings.Join(linesAt(t, m, scrollH), "\n"))
}
