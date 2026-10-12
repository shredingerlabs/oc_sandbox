package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestCardWidthIsWidestContent(t *testing.T) {
	got := Card([]string{"abcdefgh", Toast("")})
	if w := Width(got); w != 8+2+2 {
		t.Errorf("card width = %d, want 12 (widest row + padding + border)", w)
	}
	lines := strings.Split(StripANSI(got), "\n")
	if len(lines) != 4 {
		t.Errorf("card lines = %d, want 4 (2 rows + border)", len(lines))
	}
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasPrefix(lines[3], "╰") {
		t.Errorf("card not rounded-bordered: %q / %q", lines[0], lines[3])
	}
}

func TestCardGeometryStableWithAndWithoutToast(t *testing.T) {
	empty := Card([]string{"Project name", "abcdefgh", Toast("")})
	full := Card([]string{"Project name", "abcdefgh", Toast("creating…")})
	if Width(empty) != Width(full) {
		t.Errorf("toast changed card width: %d vs %d", Width(empty), Width(full))
	}
	if lipgloss.Height(empty) != lipgloss.Height(full) {
		t.Errorf("toast changed card height: %d vs %d", lipgloss.Height(empty), lipgloss.Height(full))
	}
}

func TestButton(t *testing.T) {
	plain := Button("Create Project", false)
	focus := Button("Create Project", true)
	if Width(plain) != Width(focus) {
		t.Errorf("focus changed button width: %d vs %d", Width(plain), Width(focus))
	}
	if !strings.Contains(StripANSI(plain), "╭") {
		t.Errorf("unfocused button not bordered: %q", StripANSI(plain))
	}
	lines := ButtonLines("Create Project", true)
	if len(lines) != 3 {
		t.Fatalf("button lines = %d, want 3", len(lines))
	}
	if !strings.Contains(lines[1], "Create Project") {
		t.Errorf("focused button middle line = %q, want label", lines[1])
	}
}

func TestRectHit(t *testing.T) {
	r := Rect{X: 12, Y: 2, W: 13, H: 1}
	for _, c := range [][2]int{{12, 2}, {24, 2}} {
		if !r.Hit(c[0], c[1]) {
			t.Errorf("rect %v miss at %v", r, c)
		}
	}
	for _, c := range [][2]int{{11, 2}, {25, 2}, {12, 3}, {25, 3}} {
		if r.Hit(c[0], c[1]) {
			t.Errorf("rect %v hit at %v, want miss", r, c)
		}
	}
}

func TestPadLeftAlignRight(t *testing.T) {
	if PadLeft(2, "a\nb") != "  a\n  b" {
		t.Errorf("PadLeft = %q", PadLeft(2, "a\nb"))
	}
	if StripANSI(AlignRight(10, "ab")) != "        ab" {
		t.Errorf("AlignRight = %q", AlignRight(10, "ab"))
	}
}
