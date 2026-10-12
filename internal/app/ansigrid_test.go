package app_test

// tiny ANSI-screen renderer for headless assertions: understands cursor
// addressing (ESC[<r>;<c>H / ESC[H), clear screen (ESC[2J), erase line
// (ESC[<n>K), and ignores SGR/OSC/other CSI sequences. Runes count one
// cell (the TUI is ASCII + box-charset/symbol singles).

import (
	"fmt"
	"strconv"
	"strings"
)

type ansiGrid struct {
	w, h       int
	rows       [][]rune
	curR, curC int
}

func newAnsiGrid(w, h int) *ansiGrid {
	g := &ansiGrid{w: w, h: h}
	g.reset()
	return g
}

func (g *ansiGrid) reset() {
	g.rows = make([][]rune, g.h)
	for i := range g.rows {
		g.rows[i] = []rune(strings.Repeat(" ", g.w))
	}
	g.curR, g.curC = 0, 0
}

func (g *ansiGrid) put(r rune) {
	switch r {
	case '\n':
		g.curC = 0
		g.curR++
		return
	case '\r':
		g.curC = 0
		return
	}
	if g.curR < 0 || g.curR >= g.h || g.curC < 0 || g.curC >= g.w {
		return
	}
	g.rows[g.curR][g.curC] = r
	g.curC++
}

func (g *ansiGrid) wipeRight() {
	if g.curR < 0 || g.curR >= g.h {
		return
	}
	for c := max(g.curC, 0); c < g.w; c++ {
		g.rows[g.curR][c] = ' '
	}
}

func (g *ansiGrid) String() string {
	var b strings.Builder
	for i, row := range g.rows {
		fmt.Fprintf(&b, "%2d|%s|\n", i, string(row))
	}
	return b.String()
}

func (g *ansiGrid) text() string {
	var ls []string
	for _, row := range g.rows {
		ls = append(ls, strings.TrimRight(string(row), " "))
	}
	return strings.Join(ls, "\n")
}

// findFrom finds the first occurrence at or after column minC.
func (g *ansiGrid) findFrom(sub string, minC int) (r, c int, ok bool) {
	r, c, ok = g.find(sub)
	if !ok || c < minC {
		return 0, 0, false
	}
	return r, c, true
}

func (g *ansiGrid) find(sub string) (r, c int, ok bool) {
	rr := []rune(sub)
	for i := range g.rows {
		for j := 0; j+len(rr) <= g.w; j++ {
			match := true
			for k, cr := range rr {
				if g.rows[i][j+k] != cr {
					match = false
					break
				}
			}
			if match {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// renderScreen re-renders a captured ANSI stream into a grid.
func renderScreen(w, h int, stream string) *ansiGrid {
	g := newAnsiGrid(w, h)
	rs := []rune(stream)
	i := 0
	for i < len(rs) {
		r := rs[i]
		if r != '\x1b' {
			g.put(r)
			i++
			continue
		}
		i++
		if i >= len(rs) {
			break
		}
		switch rs[i] {
		case '[':
			i++
			var params string
			for i < len(rs) && !isSeqFinal(rs[i]) {
				params += string(rs[i])
				i++
			}
			if i < len(rs) {
				final := rs[i]
				i++
				applyCSI(g, params, final)
			}
		case ']', 'P', 'X', '^', '_':
			for i < len(rs) {
				if rs[i] == '\a' || (rs[i] == '\x1b' && i+1 < len(rs) && rs[i+1] == '\\') {
					i++
					break
				}
				i++
			}
		}
	}
	return g
}

func applyCSI(g *ansiGrid, params string, final rune) {
	p := string(params)
	switch final {
	case 'H', 'f':
		g.curR, g.curC = parseAddr(p, g.h, g.w)
	case 'A':
		g.curR -= atoiOr(p, 1)
	case 'B':
		g.curR += atoiOr(p, 1)
	case 'C':
		g.curC += atoiOr(p, 1)
	case 'D':
		g.curC -= atoiOr(p, 1)
	case 'G':
		g.curC = atoiOr(p, 1) - 1
	case 'J':
		if p == "2" {
			g.reset()
		}
	case 'K', '0':
		// erase-to-right: no-op (diff lines carry full content)
	}
}

func isSeqFinal(r rune) bool { return r >= '@' && r <= '~' }

// parseAddr turns ESC[r;cH params into 0-based row/col (bad rows clap to -1).
func parseAddr(params string, h, w int) (int, int) {
	params = strings.TrimPrefix(params, "?")
	parts := strings.Split(params, ";")
	r, c := 0, 0
	if len(parts) >= 1 && parts[0] != "" {
		r = atoiOr(parts[0], 1) - 1
	}
	if len(parts) >= 2 && parts[1] != "" {
		c = atoiOr(parts[1], 1) - 1
	}
	if r < 0 || r >= h {
		return -1, -1
	}
	return r, c
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
