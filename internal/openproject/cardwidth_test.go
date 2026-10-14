package openproject

import (
	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// Regression: rows that exceed cardInnerW make the card frame wider than
// CardWidth, breaking the card-grid stride (padLines then clips the
// border). Wide runes previously bypassed the rune-counting truncate.
func TestTruncateCutsByDisplayWidth(t *testing.T) {
	cases := []struct {
		in  string
		n   int
		out string
	}{
		{"short", 26, "short"},
		{strings.Repeat("x", 40), 26, strings.Repeat("x", 25) + "…"},
		{strings.Repeat("日", 9), 10, strings.Repeat("日", 4) + "…"},
	}
	for _, c := range cases {
		got := truncate(c.in, c.n)
		if got != c.out {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.out)
		}
		if w := shell.Width(got); w > c.n {
			t.Errorf("truncate(%q, %d) = %q is %d cells wide, let %d", c.in, c.n, got, w, c.n)
		}
	}
}

func TestCardRowsFitCardWidth(t *testing.T) {
	wideFit := strings.Repeat("日", 24)  // 24 cells: bare fit as name
	wideOver := strings.Repeat("日", 40) // overflows, must truncate
	longAscii := strings.Repeat("a", 60)
	m := &Model{deps: Deps{}}

	for _, p := range []registry.Project{
		{ContainerID: "c1", Name: wideFit, Path: longAscii},
		{ContainerID: "c2", Name: wideOver, Path: longAscii},
		{ContainerID: "c3", Name: longAscii, Path: longAscii},
	} {
		rows := m.cardRows(p, true)
		if w := shell.Width(shell.Card(rows)); w != shell.CardWidth {
			t.Errorf("project %q: card width = %d, want %d", p.Name, w, shell.CardWidth)
		}
	}
}
