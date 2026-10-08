package protoapp

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Regression: every stored hit rect must land on its own element after
// the shell separator / card-frame reworks (feedback 3).
func TestAllRects(t *testing.T) {
	r := NewRoot()
	r.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	expect := func(name string, lines []string, rc rect, want string) {
		t.Helper()
		if rc.y >= len(lines) || rc.x > lipgloss.Width(stripANSI(lines[rc.y])) {
			t.Errorf("%s: rect(x=%d,y=%d) out of range", name, rc.x, rc.y)
			return
		}
		got := stripANSI(lines[rc.y])[rc.x:]
		if !strings.Contains(got, want) {
			t.Errorf("%s: rect(x=%d,y=%d) on %q, want %q", name, rc.x, rc.y, got, want)
		}
	}

	// Open Project
	r.view = viewOpen
	r.View()
	// shell separators everywhere: row 1 all "─"
	for _, v := range []int{viewOpen, viewNew, viewContainer, viewSettings} {
		r.view = v
		out := stripANSI(r.View())
		lines := strings.Split(out, "\n")
		if !strings.HasPrefix(lines[1], "─") {
			t.Errorf("view %d: no separator at row 1", v)
		}
		if !strings.HasPrefix(lines[len(lines)-1], "1 open") {
			t.Errorf("view %d: footer not last (len=%d)", v, len(lines))
		}
	}

	// New Project
	r.view = viewNew
	r.npct.np.source = 0
	r.View()
	dc := r.npct
	lines := strings.Split(stripANSI(r.View()), "\n")
	expect("np tab blank", lines, dc.tabsRects[0], "Blank Project")
	expect("np tab url", lines, dc.tabsRects[1], "From URL")
	// switch to From URL and re-check field rects
	dc.np.source = 1
	dc.refresh()
	tmp := r.View()
	_ = tmp
	lines = strings.Split(stripANSI(r.View()), "\n")
	expect("np url field", lines, rect{x: dc.fieldRects[2].x, y: dc.fieldRects[2].y + 1}, "https://")
	btnRow := stripANSI(lines[dc.npBtn.y])
	if !strings.Contains(btnRow, "Create Project") {
		t.Errorf("np create button rect y=%d on %q", dc.npBtn.y, btnRow)
	}

	// Container build
	r.view = viewContainer
	dc.ctPane = 0
	r.View()
	lines = strings.Split(stripANSI(r.View()), "\n")
	expect("ct build tab", lines, dc.ctTabsRects[0], "Build Container")
	expect("ct stop tab", lines, dc.ctTabsRects[1], "Stop Container")
	expect("ct chip0 title", lines, rect{x: dc.chipRects[0].x + 2, y: dc.chipRects[0].y + 1}, "edition swdev")
	if !strings.Contains(stripANSI(lines[dc.ctBtn.y]), "Build") {
		t.Errorf("ct build btn on %q", stripANSI(lines[dc.ctBtn.y+1]))
	}
	// stop
	dc.ctPane = 1
	r.View()
	lines = strings.Split(stripANSI(r.View()), "\n")
	expect("ct selall", lines, dc.selAll, "select all")
	expect("ct stoprow", lines, dc.stopRects[0], "swdev-core")
	if !strings.Contains(stripANSI(lines[dc.ctBtn.y]), "Stop") {
		t.Errorf("ct stop btn rect y=%d on %q", dc.ctBtn.y, stripANSI(lines[dc.ctBtn.y]))
	}

	// Settings
	r.view = viewSettings
	s := r.setInstance()
	r.View()
	lines = strings.Split(stripANSI(r.View()), "\n")
	for i, tr := range s.tabRects {
		expect("tab"+string(rune('0'+i)), lines, rect{x: tr.x + 1, y: tr.y}, settingsTabs[i][:5])
	}
	// import
	expect("imp path", lines, s.impRects[0], "/home/dev/work/")
	midY := s.impRects[1].y
	if !strings.Contains(stripANSI(lines[midY]), "Import Project") {
		t.Errorf("import btn midline y=%d on %q", midY, stripANSI(lines[midY]))
	}
	for i, fr := range s.foldRects {
		expect("fold", lines, fr, fakeFolders[i][:10])
	}
	// export stage 0
	s.sub = 1
	s.expStage = 0
	r.View()
	lines = strings.Split(stripANSI(r.View()), "\n")
	expect("exp btn", lines, rect{x: s.expRects[1].x + 3, y: s.expRects[1].y}, "Export")
	// stage 1
	s.expStage = 1
	r.View()
	lines = strings.Split(stripANSI(r.View()), "\n")
	expect("exp dest", lines, s.expRects[0], "/home/dev/oc-sandbox")
	expect("save", lines, s.expRects[1], "Save")
	expect("cancel", lines, s.expRects[2], "Cancel")
	// restore
	s.sub = 2
	r.View()
	lines = strings.Split(stripANSI(r.View()), "\n")
	expect("res btn", lines, s.resRects[1], "Restore")
	for i, br := range s.backRects {
		expect("back", lines, br, fakeBackups[i][:10])
	}
	// uninstall
	s.sub = 3
	r.View()
	lines = strings.Split(stripANSI(r.View()), "\n")
	for i := 0; i < 3; i++ {
		expect("unopt", lines, s.unRects[i], "☐")
	}
	expect("unconfirm", lines, s.unRects[3], "…")
	expect("unbtn", lines, s.unRects[4], "Uninstall")
}
