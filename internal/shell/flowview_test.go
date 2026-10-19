package shell

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func trimViewPadding(s string) string { return strings.TrimRight(s, " ") }

// Appended hidden flow view (#81/#84): the first-run setup flow is
// plugged in after the four menu views and is reachable only through
// root-level handoff messages — no header item of its own, Exit keeps
// clicking as Exit.

type fakeFlow struct {
	fakeView
	consume bool
}

func (f *fakeFlow) HookMsg(msg tea.Msg) (bool, tea.Cmd) {
	if _, ok := msg.(RecoverMsg); !ok {
		return false, nil
	}
	return f.consume, nil
}

func flowModel(consume bool) Model {
	m := newTestModel()
	m.views = append(m.views, &fakeFlow{fakeView: fakeView{name: "flow"}, consume: consume})
	return m
}

func TestRecoverMsgActivatesAppendedFlowView(t *testing.T) {
	m := flowModel(true)
	m, cmd := step(m, RecoverMsg{Path: "/tmp/proj"})
	if cmd != nil {
		t.Fatalf("hook cmd leaked: %v", cmd)
	}
	if m.Active() != 4 {
		t.Fatalf("active = %d, want the flow view slot 4", m.Active())
	}
	lines := viewLines(t, resized(m, 80, 41))
	if !strings.Contains(lines[3], `BODY:flow sub=0 typed="" keys=[] last= h=37`) {
		t.Fatalf("flow body not rendered: %q", lines[3])
	}
}

func TestUninterestedHookPassesRecoverMsgThrough(t *testing.T) {
	m := flowModel(false)
	m, _ = step(m, RecoverMsg{Path: "/tmp/proj"})
	if m.Active() != 0 {
		t.Fatalf("active = %d, want navigation unchanged", m.Active())
	}
}

func TestExitClickStillQuitsWithFlowViewAppended(t *testing.T) {
	m := flowModel(true)
	m = resized(m, 80, 41)
	rects := MenuRects()
	exit := rects[len(rects)-1]
	m, cmd := step(m, press(exit.X, exit.Y))
	mustQuit(t, cmd)
}

func TestFlowViewNotSelectableByHeaderClick(t *testing.T) {
	m := flowModel(true)
	m = resized(m, 80, 41)
	for _, r := range MenuRects()[:len(MenuItems)-1] {
		m, _ = step(m, press(r.X, r.Y))
		if m.Active() == 4 {
			t.Fatal("flow view reachable through a header item click")
		}
	}
	if m.Active() != 3 {
		t.Fatalf("active = %d, want back on the last menu view", m.Active())
	}
}

func TestFourViewsMenuParityUnchanged(t *testing.T) {
	m := resized(newTestModel(), 80, 41)
	rects := MenuRects()
	m, cmd := step(m, press(rects[len(rects)-1].X, rects[len(rects)-1].Y))
	mustQuit(t, cmd)
}
