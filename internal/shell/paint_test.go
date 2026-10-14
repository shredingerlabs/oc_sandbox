package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// withANSI256 pins the color profile: `go test` runs headless, where the
// profile would degrade to Ascii and the color assertions stay vacuous.
func withANSI256(t *testing.T) {
	t.Helper()
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(saved) })
}

// TestPaintRecoversAfterSGRReset: a line whose content resets SGR (e.g. a
// card border) must not drop the paint — the seq is re-established after
// every reset and the line is painted to the full width.
func TestPaintRecoversAfterSGRReset(t *testing.T) {
	withANSI256(t)
	seq := paintSeq(lipgloss.NewStyle().Background(ColWindowBg))
	line := "\x1b[38;5;39m╭╮\x1b[0mhello"

	got := paint(line, seq, 10)

	if !strings.HasPrefix(got, seq) {
		t.Fatalf("line must open with the paint seq %q, got %q", seq, got)
	}
	if !strings.Contains(got, "\x1b[0m"+seq) {
		t.Fatalf("after the inner reset the paint must be re-established, got %q", got)
	}
	if w := ansi.StringWidth(got); w != 10 {
		t.Fatalf("line must stay %d cells wide, got %d", 10, w)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("line must close with a reset, got %q", got)
	}
}

// TestPaintPlainLinePadsToWidth: a styleless line is opened, padded with
// painted spaces to the full width and closed.
func TestPaintPlainLinePadsToWidth(t *testing.T) {
	withANSI256(t)
	seq := paintSeq(lipgloss.NewStyle().Background(ColWindowBg))

	got := paint("abc", seq, 6)

	want := seq + "abc" + "   " + "\x1b[0m"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestPaintAsciiProfileIsInert: with the Ascii profile (tests, NO_COLOR)
// no escapes are emitted at all.
func TestPaintAsciiProfileIsInert(t *testing.T) {
	saved := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(saved)

	styled := "\x1b[38;5;39m─\x1b[0mrest"
	if got := paint(styled, "", 5); got != ansi.Truncate(styled, 5, "") {
		t.Fatalf("Ascii profile must just truncate, got %q", got)
	}
}
