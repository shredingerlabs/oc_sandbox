package shell

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func keyOf(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

func TestConfirmFocusModel(t *testing.T) {
	var c Confirm
	c.Open("Uninstall oc-sandbox?", "Uninstall", "Cancel", []string{"context"}, true,
		func() tea.Cmd { return tea.Quit })

	if !c.IsOpen() {
		t.Fatal("not open after Open")
	}
	// Enter on the default focus (Cancel) closes without running.
	if cmd := c.Key(enterKey()); cmd != nil {
		t.Fatal("enter on default focus must not run")
	}
	if c.IsOpen() {
		t.Fatal("enter on Cancel must close")
	}
	// switch focus, then Enter runs the continuation.
	c.Open("t", "OK", "Cancel", nil, false, func() tea.Cmd { return tea.Quit })
	c.Key(keyOf("tab"))
	if cmd := c.Key(enterKey()); cmd == nil {
		t.Fatal("enter after tab must run the continuation")
	}
	if c.IsOpen() {
		t.Fatal("confirm must close")
	}
	// esc/q cancels
	c.Open("t", "OK", "Cancel", nil, false, func() tea.Cmd { return tea.Quit })
	if c.Key(keyOf("q")) != nil {
		t.Fatal("q yields no cmd")
	}
	if c.IsOpen() {
		t.Fatal("q must cancel")
	}
}

func TestConfirmLeftRightSwitch(t *testing.T) {
	var c Confirm
	c.Open("t", "OK", "Cancel", nil, false, func() tea.Cmd { return tea.Quit })
	c.Key(keyOf("right"))
	if cmd := c.Key(enterKey()); cmd == nil {
		t.Fatal("right then enter must reach confirm")
	}
}

func TestConfirmClickModel(t *testing.T) {
	var c Confirm
	ran := false
	run := func() tea.Cmd { ran = true; return func() tea.Msg { return struct{}{} } }
	c.Open("Build image?", "Build", "Cancel", []string{"edition: web"}, false, run)
	width, height, top := 80, 20, 3
	_ = c.Overlay(width, height, top)

	// press inside the panel but on no button: absorbed, stays open
	if _, ok := c.Click(c.panel.X+1, c.panel.Y+1); !ok {
		t.Fatal("panel-body press must be consumed by the modal")
	}
	if !c.IsOpen() {
		t.Fatal("panel press must not close")
	}
	// press outside the panel cancels
	if _, ok := c.Click(0, top); !ok {
		t.Fatal("outside press consumed")
	}
	if c.IsOpen() {
		t.Fatal("outside press cancels")
	}
	// press the confirm button runs
	c.Open("Build image?", "Build", "Cancel", nil, false, run)
	_ = c.Overlay(width, height, top)
	if _, ok := c.Click(c.okRect.X+1, c.okRect.Y); !ok {
		t.Fatal("confirm press consumed")
	}
	if !ran {
		t.Fatal("confirm press ran the continuation")
	}
	// press the cancel rect closes without running
	ran = false
	c.Open("Build image?", "Build", "Cancel", nil, false, run)
	_ = c.Overlay(width, height, top)
	if _, ok := c.Click(c.cancelRect.X+1, c.cancelRect.Y); !ok {
		t.Fatal("cancel press consumed")
	}
	if ran || c.IsOpen() {
		t.Fatal("cancel press closed without running")
	}
}

func TestConfirmOverlayGeometry(t *testing.T) {
	var c Confirm
	c.Open("Build the web image?", "Build", "Cancel", []string{"edition: web"}, false,
		func() tea.Cmd { return nil })
	out := c.Overlay(80, 20, 3)

	plain := StripANSI(out)
	for _, want := range []string{"Build the web image?", "edition: web", "Cancel", "Build"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("overlay missing %q:\n%s", want, plain)
		}
	}
	// the buttons sit on the button row of the rendered panel
	var btnRow string
	for _, l := range strings.Split(plain, "\n") {
		if strings.Contains(l, "Cancel") && strings.Contains(l, "Build") {
			btnRow = l
		}
	}
	if btnRow == "" {
		t.Fatalf("no row with both buttons:\n%s", plain)
	}
	if strings.Index(btnRow, "Cancel") > strings.Index(btnRow, "Build") {
		t.Fatalf("cancel must be left of confirm:\n%s", btnRow)
	}
	// button rects live inside the panel rect
	if !c.panel.Hit(c.okRect.X, c.okRect.Y) || !c.panel.Hit(c.cancelRect.X, c.cancelRect.Y) {
		t.Fatal("button rects must be inside the panel rect")
	}
	// the panel is centered horizontally
	if got, want := c.panel.X, (80-c.panel.W)/2; got != want {
		t.Fatalf("panel x = %d, want %d", got, want)
	}
	if c.panel.Y < 3 {
		t.Fatalf("panel y = %d, below the body region", c.panel.Y)
	}
	// closed popup renders nothing
	c.Close()
	if c.Overlay(80, 20, 3) != "" {
		t.Fatal("closed popup must render empty")
	}
}

// What renders is what the rects address: the button's hit rect x/y
// must equal the display column/row of its label inside the rendered
// overlay (before the fix the panel rendered top-left while the rects
// pointed to the center — every visible click landed "outside").
func TestConfirmOverlayRenderMatchesRects(t *testing.T) {
	var c Confirm
	c.Open("Build the web image?", "Build", "Cancel", []string{"edition: web"}, false,
		func() tea.Cmd { return nil })
	width, height, top := 80, 18, 3
	out := c.Overlay(width, height, top)
	lines := strings.Split(StripANSI(out), "\n")

	cancelLine := lines[c.cancelRect.Y-top]
	cancelCol := displayCol(cancelLine, "Cancel")
	if cancelCol < 0 || cancelCol != c.cancelRect.X+3 { // border + inner padding
		t.Fatalf("cancel rect x=%d, rendered label at %d:\n%s", c.cancelRect.X, cancelCol, out)
	}
	if c.cancelRect.Y < top || c.cancelRect.Y >= top+height {
		t.Fatalf("button row %d outside the region", c.cancelRect.Y)
	}
}

// displayCol returns the cell column (runes, ANSI-stripped) of the
// first token occurrence in a line.
func displayCol(line, token string) int {
	runesBefore := strings.Split(line, token)[0]
	return len([]rune(runesBefore))
}

// A danger confirm renders unlike the plain one. Exact colors degrade
// with the terminal color profile, so this test forces an ANSI profile
// on the default renderer.
func TestConfirmDangerStyling(t *testing.T) {
	renderer := lipgloss.DefaultRenderer()
	renderer.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { renderer.SetColorProfile(termenv.Ascii) })

	var red, plain Confirm
	red.Open("Uninstall oc-sandbox?", "Uninstall", "Cancel", nil, true, func() tea.Cmd { return nil })
	plain.Open("Uninstall oc-sandbox?", "Uninstall", "Cancel", nil, false, func() tea.Cmd { return nil })
	if StripANSI(red.Overlay(80, 20, 3)) != StripANSI(plain.Overlay(80, 20, 3)) {
		t.Fatal("danger and plain confirm share the same geometry")
	}
	if red.Overlay(80, 20, 3) == plain.Overlay(80, 20, 3) {
		t.Fatal("danger confirm must render unlike the plain confirm")
	}
}
