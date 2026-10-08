package shell

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
)

func mouse(action tea.MouseAction, button tea.MouseButton, x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: action, Button: button, X: x, Y: y}
}

func TestDblClickCounts(t *testing.T) {
	var d DblClick
	if got := d.Press(mouse(tea.MouseActionPress, tea.MouseButtonLeft, 5, 5)); got != 1 {
		t.Fatalf("first press = %d, want 1", got)
	}
	if got := d.Press(mouse(tea.MouseActionPress, tea.MouseButtonLeft, 5, 5)); got != 2 {
		t.Fatalf("second press same cell = %d, want 2", got)
	}
	// a third press starts fresh (the double was consumed)
	if got := d.Press(mouse(tea.MouseActionPress, tea.MouseButtonLeft, 5, 5)); got != 1 {
		t.Fatalf("third press = %d, want 1", got)
	}
	// different cell
	if got := d.Press(mouse(tea.MouseActionPress, tea.MouseButtonLeft, 6, 5)); got != 1 {
		t.Fatalf("press other cell = %d, want 1", got)
	}
	// non-left button and releases are ignored
	if got := d.Press(mouse(tea.MouseActionPress, tea.MouseButtonRight, 6, 5)); got != 0 {
		t.Fatalf("right press = %d, want 0", got)
	}
	if got := d.Press(mouse(tea.MouseActionRelease, tea.MouseButtonLeft, 6, 5)); got != 0 {
		t.Fatalf("release = %d, want 0", got)
	}
}

func TestDblClickWindowExpiry(t *testing.T) {
	var d DblClick
	d.Press(mouse(tea.MouseActionPress, tea.MouseButtonLeft, 5, 5))
	// backdate the recorded press past the window
	d.lastTime = d.lastTime.Add(-DblClickWindow - time.Millisecond)
	if got := d.Press(mouse(tea.MouseActionPress, tea.MouseButtonLeft, 5, 5)); got != 1 {
		t.Fatalf("press after window = %d, want 1", got)
	}
}

func TestLogPaneStreamsAndFollows(t *testing.T) {
	l := NewLogPane().SetSize(40, 5) // 1 spinner row + 4 viewport rows
	l, _ = l.Update(LineMsg("building image"))
	l, _ = l.Update(LineMsg("step 1"))
	if got := StripANSI(l.View()); !strings.Contains(got, "building image") || !strings.Contains(got, "step 1") {
		t.Errorf("streamed lines not visible: %q", got)
	}
	// overflow follows the tail
	for i := 0; i < 10; i++ {
		l, _ = l.Update(LineMsg(fmt.Sprintf("line %d", i)))
	}
	got := StripANSI(l.View())
	if !strings.Contains(got, "line 9") {
		t.Errorf("pane does not follow the tail: %q", got)
	}
	if strings.Contains(got, "line 0") {
		t.Errorf("pane still shows the head: %q", got)
	}
}

func TestLogPaneScrollSuspendsFollow(t *testing.T) {
	l := NewLogPane().SetSize(40, 5)
	for i := 0; i < 10; i++ {
		l, _ = l.Update(LineMsg(fmt.Sprintf("line %d", i)))
	}
	// wheel up: suspends follow, head visible
	for i := 0; i < 10; i++ {
		l, _ = l.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelUp})
	}
	got := StripANSI(l.View())
	if !strings.Contains(got, "line 0") {
		t.Errorf("wheel up did not reveal the head: %q", got)
	}
	// new line does not yank the view to the bottom
	l, _ = l.Update(LineMsg("line 10"))
	got = StripANSI(l.View())
	if !strings.Contains(got, "line 0") || strings.Contains(got, "line 10") {
		t.Errorf("new line jumped the pane while scrolled up: %q", got)
	}
	// back at the bottom: follow resumes
	for i := 0; i < 20; i++ {
		l, _ = l.Update(tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	}
	l, _ = l.Update(LineMsg("line 11"))
	got = StripANSI(l.View())
	if !strings.Contains(got, "line 11") {
		t.Errorf("follow did not resume at the bottom: %q", got)
	}
}

func TestLogPaneSpinner(t *testing.T) {
	l := NewLogPane().Run(true).SetSize(40, 5)
	if !l.Spinning() {
		t.Fatalf("Run(true) did not spin")
	}
	cmd := l.Spin()
	if cmd == nil {
		t.Fatalf("Spin() nil while running")
	}
	msg := cmd()
	if _, ok := msg.(spinner.TickMsg); !ok {
		t.Fatalf("Spin() produced %T, want spinner.TickMsg", msg)
	}
	l, next := l.Update(msg)
	if next == nil {
		t.Fatalf("spinner did not schedule the next tick")
	}
	view := StripANSI(l.View())
	first := strings.SplitN(view, "\n", 2)[0]
	if strings.TrimSpace(first) == "" {
		t.Errorf("spinner row blank while running: %q", view)
	}

	// idle: no spin cmd, blank spinner row
	l = l.Run(false)
	if l.Spinning() {
		t.Errorf("Run(false) still spinning")
	}
	if l.Spin() != nil {
		t.Errorf("Spin() non-nil while idle")
	}
	if first := strings.SplitN(StripANSI(l.View()), "\n", 2)[0]; strings.TrimSpace(first) != "" {
		t.Errorf("idle spinner row not blank: %q", first)
	}
}
