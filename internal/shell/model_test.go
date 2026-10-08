package shell

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// fakeView is a stand-in center view: everything it observes is rendered
// back through Body so tests assert on View() output only.
type fakeView struct {
	name        string
	subTabs     []string
	sub         int
	textFocused bool
	keys        []string
	typed       string
	lastMsg     string
}

func (f *fakeView) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		f.keys = append(f.keys, msg.String())
		if len(msg.Runes) == 1 {
			f.typed += string(msg.Runes)
		}
	case SubTabMsg:
		f.sub = msg.Index
		f.lastMsg = fmt.Sprintf("subtab:%d", msg.Index)
	case tea.MouseMsg:
		f.lastMsg = fmt.Sprintf("mouse:%d,%d", msg.X, msg.Y)
	}
	return nil
}

func (f *fakeView) Body(width, height int) string {
	return fmt.Sprintf("BODY:%s sub=%d typed=%q keys=%v last=%s h=%d",
		f.name, f.sub, f.typed, f.keys, f.lastMsg, height)
}

func (f *fakeView) SubTabs() ([]string, int) { return f.subTabs, f.sub }
func (f *fakeView) TextFocused() bool        { return f.textFocused }

func newTestModel() Model {
	return New(
		&fakeView{name: "one", subTabs: []string{"Alpha Tab", "Beta Tab"}},
		&fakeView{name: "two", subTabs: []string{"Blank Project", "From URL"}},
		&fakeView{name: "three", subTabs: []string{"Build Container", "Stop Container"}},
		&fakeView{name: "four", subTabs: []string{"Import project", "Export Config"}},
	)
}

func step(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func resized(m Model, w, h int) Model {
	m, _ = step(m, tea.WindowSizeMsg{Width: w, Height: h})
	return m
}

func keyMsg(s string) tea.Msg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func press(x, y int) tea.Msg {
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
}

func viewLines(t *testing.T, m Model) []string {
	t.Helper()
	return strings.Split(StripANSI(m.View()), "\n")
}

func mustQuit(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatalf("expected quit cmd, got nil")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg from cmd")
	}
}

func TestViewRendersChrome(t *testing.T) {
	m := resized(newTestModel(), 100, 30)
	lines := viewLines(t, m)
	if len(lines) != 30 {
		t.Fatalf("view has %d lines, want 30", len(lines))
	}
	if !strings.Contains(lines[0], "oc-sandbox") || !strings.Contains(lines[0], "Open Project") {
		t.Errorf("header row = %q", lines[0])
	}
	if lines[1] != strings.Repeat("─", 99) {
		t.Errorf("separator row not full-width: %q", lines[1])
	}
	if !strings.Contains(lines[2], "Alpha Tab") || !strings.Contains(lines[2], "Beta Tab") {
		t.Errorf("sub-menu row = %q", lines[2])
	}
	if !strings.Contains(lines[3], "BODY:one") {
		t.Errorf("body does not start at row 3: %q", lines[3])
	}
	footer := lines[len(lines)-1]
	if !strings.Contains(footer, "1 open · 2 new project") || !strings.Contains(footer, "quit") {
		t.Errorf("footer = %q", footer)
	}
	// body gets height-4 rows
	if !strings.Contains(lines[3], "h=26") {
		t.Errorf("body height = %q, want 26", lines[3])
	}
}

func TestHitRectsLandOnTheirElements(t *testing.T) {
	m := resized(newTestModel(), 100, 30)
	lines := viewLines(t, m)
	for i, r := range MenuRects() {
		got := lines[0][min(r.X, len(lines[0])):]
		if !strings.Contains(got[:min(r.W, len(got))], MenuItems[i][:4]) {
			t.Errorf("menu rect %d (x=%d) lands on %q, want %q", i, r.X, got[:min(r.W, len(got))], MenuItems[i])
		}
	}
	tabs := []string{"Alpha Tab", "Beta Tab"}
	for i, r := range m.SubTabRects() {
		if r.Y != SubTabRowY {
			t.Errorf("sub-tab rect %d y=%d, want %d", i, r.Y, SubTabRowY)
		}
		got := lines[r.Y][min(r.X, len(lines[r.Y])):]
		if !strings.Contains(got[:min(r.W, len(got))], tabs[i][:4]) {
			t.Errorf("sub-tab rect %d (x=%d) lands on %q, want %q", i, r.X, got[:min(r.W, len(got))], tabs[i])
		}
	}
}

func TestSubTabRectsStableAcrossSizesAndViews(t *testing.T) {
	m := resized(newTestModel(), 100, 30)
	rects := m.SubTabRects()
	m2 := resized(newTestModel(), 120, 40)
	if fmt.Sprint(m2.SubTabRects()) != fmt.Sprint(rects) {
		t.Errorf("sub-tab rects moved with window size: %v vs %v", m2.SubTabRects(), rects)
	}
	m3, _ := step(resized(newTestModel(), 120, 40), keyMsg("2"))
	want := []Rect{{X: TitleWidth(), Y: 2, W: Width("Blank Project") + 2, H: 1},
		{X: TitleWidth() + Width("Blank Project") + 2, Y: 2, W: Width("From URL") + 2, H: 1}}
	if fmt.Sprint(m3.SubTabRects()) != fmt.Sprint(want) {
		t.Errorf("view 2 sub-tab rects = %v, want %v", m3.SubTabRects(), want)
	}
}

func TestKeyTableViewSwitch(t *testing.T) {
	m := resized(newTestModel(), 100, 30)
	for key, want := range map[string]string{"1": "BODY:one", "2": "BODY:two", "3": "BODY:three", "4": "BODY:four"} {
		m, _ = step(m, keyMsg(key))
		if !strings.Contains(m.View(), want) {
			t.Errorf("key %q did not switch to %q", key, want)
		}
	}
}

func TestViewSwitchSuppressedWhileTextFocused(t *testing.T) {
	views := []*fakeView{
		{name: "one", textFocused: true, subTabs: []string{"Alpha Tab"}},
		{name: "two"},
	}
	m := resized(New(views[0], views[1]), 100, 30)

	m, cmd := step(m, keyMsg("2")) // suppressed: must type into the view
	if cmd != nil {
		t.Fatalf("key 2 while typing produced cmd %v", cmd)
	}
	if !strings.Contains(m.View(), "BODY:one") || m.Active() != 0 {
		t.Errorf("view switched while text focused (active=%d)", m.Active())
	}
	m, cmd = step(m, keyMsg("q")) // suppressed: must type, not quit
	if cmd != nil {
		t.Fatalf("q while typing quit the app")
	}
	if !strings.Contains(m.View(), `typed="2q"`) {
		t.Errorf("letters did not type into the view: %q", StripANSI(m.View()))
	}

	// focus released: 2 switches views, q quits
	views[0].textFocused = false
	m, _ = step(m, keyMsg("2"))
	if m.Active() != 1 || !strings.Contains(m.View(), "BODY:two") {
		t.Errorf("key 2 with focus released did not switch (active=%d)", m.Active())
	}
	_, cmd = step(m, keyMsg("q"))
	mustQuit(t, cmd)
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		m := resized(newTestModel(), 100, 30)
		_, cmd := step(m, keyMsg(k))
		mustQuit(t, cmd)
		if !strings.Contains(m.View(), "BODY:one") {
			t.Errorf("quit key %q changed the view", k)
		}
	}
}

func TestKeysForwardedToActiveView(t *testing.T) {
	m := resized(newTestModel(), 100, 30)
	for _, k := range []string{"down", "up", "j", "k", "tab", "shift+tab", "enter", " ", "x"} {
		m, _ = step(m, keyMsg(k))
	}
	if !strings.Contains(m.View(), "keys=[down up j k tab shift+tab enter   x]") {
		t.Errorf("keys not forwarded in order: %q", StripANSI(m.View()))
	}
}

func TestMouseHeaderMenu(t *testing.T) {
	m := resized(newTestModel(), 100, 30)
	rects := MenuRects()
	m, _ = step(m, press(rects[2].X+1, 0)) // "Container"
	if !strings.Contains(m.View(), "BODY:three") {
		t.Errorf("click on Container did not switch views")
	}
	m, cmd := step(m, press(rects[4].X+1, 0)) // Exit
	mustQuit(t, cmd)
}

func TestMouseSubTabsAndBody(t *testing.T) {
	m := resized(newTestModel(), 100, 30)
	rects := m.SubTabRects()
	m, _ = step(m, press(rects[1].X+1, SubTabRowY)) // "Beta Tab"
	if !strings.Contains(m.View(), "sub=1") {
		t.Errorf("sub-tab click did not select tab 1: %q", StripANSI(m.View()))
	}
	m, _ = step(m, press(5, 10)) // body area
	if !strings.Contains(m.View(), "last=mouse:5,10") {
		t.Errorf("body click not forwarded to view")
	}
}

func TestTooSmallGuardAsciiSnapshot(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })

	m := newTestModel()
	got := m.View() // 0x0 before any WindowSizeMsg
	want := "Terminal too small — recommended 80x24, got 0x0.\nResize to continue."
	if got != want {
		t.Errorf("initial guard = %q, want %q", got, want)
	}
	if strings.Contains(got, "\x1b") {
		t.Errorf("guard screen contains escapes")
	}
	for _, sz := range [][2]int{{79, 24}, {80, 23}, {40, 10}} {
		m = resized(newTestModel(), sz[0], sz[1])
		want := fmt.Sprintf("Terminal too small — recommended 80x24, got %dx%d.\nResize to continue.", sz[0], sz[1])
		if m.View() != want {
			t.Errorf("%dx%d guard = %q, want %q", sz[0], sz[1], m.View(), want)
		}
	}
	// restore on resize
	m, _ = step(m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !strings.Contains(m.View(), "BODY:one") {
		t.Errorf("resize to 80x24 did not restore the app: %q", StripANSI(m.View()))
	}
	// shrink again re-arms the guard
	m, _ = step(m, tea.WindowSizeMsg{Width: 79, Height: 24})
	if !strings.HasPrefix(m.View(), "Terminal too small") {
		t.Errorf("shrink did not re-arm the guard")
	}
}
