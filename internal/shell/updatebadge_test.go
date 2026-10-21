package shell

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

// Footer update badge: a view may report one (update available); the
// shell renders it right after the keyboard hint and a click jumps to
// the owing view's update sub-tab.

type badgeView struct {
	fakeView
	text string
	tab  int
}

func (b *badgeView) UpdateBadge() (string, int) {
	if b.text == "" {
		return "", 0
	}
	return b.text, b.tab
}

func newBadgeModel(bv *badgeView) Model {
	m := New(bv)
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24}) // value receiver: take the mutated copy
	return mm.(Model)
}

func TestFooterBadgeRenderedAfterHint(t *testing.T) {
	footer, rect, ok := FooterRow(100, help.New(), NewKeys(), "update available: v9.9.9")
	if !ok || rect.W == 0 {
		t.Fatalf("no badge rect: rect=%v ok=%v", rect, ok)
	}
	plain := StripANSI(footer)
	if !strings.Contains(plain, "update available: v9.9.9") {
		t.Fatalf("footer missing badge:\n%q", plain)
	}
	if !strings.Contains(plain, "1 open") {
		t.Fatalf("footer lost the keyboard hint:\n%q", plain)
	}
}

func TestFooterWithoutBadge(t *testing.T) {
	m := newBadgeModel(&badgeView{})
	_, rect, ok := FooterRow(100, m.help, m.keys, "")
	if ok || rect.W != 0 {
		t.Fatalf("empty badge returned a rect: rect=%v ok=%v", rect, ok)
	}
	if plain := StripANSI(m.Footer()); strings.Contains(plain, "update available") {
		t.Fatalf("shell rendered a badge without a reporting view:\n%q", plain)
	}
}

func TestBadgeClickJumpsToOwningView(t *testing.T) {
	bv := &badgeView{text: "update available: v0.2.0", tab: 4}
	m := newBadgeModel(bv)
	rect := BadgeRect("update available: v0.2.0")
	if rect.X <= 0 || rect.W == 0 {
		t.Fatalf("badge rect off: %+v", rect)
	}
	mm, _ := m.Update(tea.MouseMsg{X: rect.X + 1, Y: 23, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	m = mm.(Model)
	if bv.sub != 4 {
		t.Fatalf("badge click did not route SubTabMsg to the update tab: sub=%d", bv.sub)
	}
	// A click outside the badge's x span must not route.
	bv2 := &badgeView{text: "update available: v0.2.0", tab: 4}
	m2 := newBadgeModel(bv2)
	mm, _ = m2.Update(tea.MouseMsg{X: 0, Y: 23, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	_ = mm
	if bv2.sub == 4 {
		t.Fatalf("off-badge click jumped to the update tab")
	}
}

func TestInitBatchesStartupCmds(t *testing.T) {
	m := newBadgeModel(&badgeView{})
	if cmd := m.Init(); cmd != nil {
		t.Fatalf("no init cmds wired, got %v", cmd)
	}
	m.AddInitCmd(func() tea.Msg { return tea.QuitMsg{} })
	m.AddInitCmd(nil) // nil cmds are skipped
	if len(m.initCmds) != 1 {
		t.Fatalf("initCmds = %v, want exactly the non-nil cmd", m.initCmds)
	}
}
