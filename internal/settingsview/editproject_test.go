package settingsview

// Ticket #108: the Settings center's sub-menu tabs gain "Edit Project
// settings" (index 4). Selecting it hands the pane surface (Update keys/
// mouse, Body, Rects, TextFocused) to the projectsettings view. The
// other four tabs' core is untouched.

import (
	"time"

	"strings"
	"testing"

	"github.com/shredingerlabs/oc-sandbox/internal/registry"
	"github.com/shredingerlabs/oc-sandbox/internal/shell"
)

// editProjectsFixture registers two projects on home: alpha first (the
// open project — last used) and beta.
func editProjectsFixture(t *testing.T) string {
	t.Helper()
	home := homeFixture(t)
	// receding stamps per insert: AllOrdered sorts by last_used desc, so
	// alpha (added first, stamped latest) sorts first = the open project
	seq := 0
	reg := registry.Registry{Home: home, Now: func() time.Time {
		seq--
		return time.Now().Add(time.Duration(seq+1) * time.Minute)
	}}
	root := sandboxRoot(t, home, "alpha", "python", []string{"bridge"}, "terminal")
	if err := reg.AddProject(registry.AddRequest{Name: "alpha", Path: root, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	root2 := sandboxRoot(t, home, "beta", "python", []string{"bridge"}, "terminal")
	if err := reg.AddProject(registry.AddRequest{Name: "beta", Path: root2, GitTracking: "none"}); err != nil {
		t.Fatal(err)
	}
	return home
}

// clickRect clicks a rect from the view's current geometry.
func clickRect(t *testing.T, v *View, x, y int) {
	t.Helper()
	clickAt(v, x, y)
}

func TestEditProjectTabListedFirst(t *testing.T) {
	home := homeFixture(t)
	v := newSettings(t, home, nil)
	items, _ := v.SubTabs()
	if len(items) != 5 {
		t.Fatalf("tabs = %v, want 5 entries", items)
	}
	if got := items[0]; got != "Edit Project settings" {
		t.Fatalf("tab 1 = %q, want Edit Project settings", got)
	}
}

func TestEditProjectTabShowsProjectCards(t *testing.T) {
	home := editProjectsFixture(t)
	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: subEdit})
	lines := bodyLines(t, v)
	for _, want := range []string{"alpha", "beta", "vcs  AI Provider", "select a card, then vcs, AI Provider"} {
		if !strings.Contains(strings.Join(lines, "\n"), want) {
			t.Fatalf("screen missing %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	// Rects come from the delegated screen: the open project's (first
	// card, geometry-pinned) vcs / AI Provider buttons.
	rects := v.Rects()
	if len(rects) != 3 || rects[0].Y != 7 || rects[0].X != 3 {
		t.Fatalf("delegated rects wrong: %+v", rects)
	}
}

func TestEditProjectTabClicksResolveInPane(t *testing.T) {
	home := editProjectsFixture(t)
	v := newSettings(t, home, nil)
	v.Update(shell.SubTabMsg{Index: subEdit})
	// open the vcs placeholder on the open project
	v.Rects() // ensure fresh geometry
	b := body(v)
	if !strings.Contains(b, "vcs") {
		t.Fatalf("button row missing:\n%s", b)
	}
	r := v.Rects()[0]
	clickRect(t, v, r.X, r.Y)
	if b := body(v); !strings.Contains(b, "VCS — alpha") {
		t.Fatalf("vcs pane missing:\n%s", b)
	}
	// clicking the other card resets the pane to the hint (beta is one
	// rowStride = 7 down, same grid column)
	clickRect(t, v, 3, 14)
	if b := body(v); !strings.Contains(b, "select a card, then vcs, AI Provider") || strings.Contains(b, "VCS —") {
		t.Fatalf("pane not reset after card click:\n%s", b)
	}
}

func TestEditProjectTabCoreTabsUnaffected(t *testing.T) {
	home := editProjectsFixture(t)
	v := newSettings(t, home, nil)
	// before: the Restore tab shows its backup list (core tabs intact)
	v.Update(shell.SubTabMsg{Index: subRestore})
	before := body(v)
	if !strings.Contains(before, "Restore") {
		t.Fatalf("restore pane missing:\n%s", before)
	}
	// into the Edit Project settings tab and back: core pane identical
	v.Update(shell.SubTabMsg{Index: subEdit})
	items, sel := v.SubTabs()
	if sel != subEdit || items[subEdit] != "Edit Project settings" {
		t.Fatalf("tab not on Edit Project settings: sel=%d", sel)
	}
	b := body(v)
	if !strings.Contains(b, "alpha") {
		t.Fatalf("edit-project pane missing:\n%s", b)
	}
	v.Update(shell.SubTabMsg{Index: subRestore})
	if got := body(v); got != before {
		t.Fatalf("core tab changed by the visit:\n%s", got)
	}
	// the delegated button geometry no longer leaks once another tab is
	// active (restore pane has its own rows)
	for _, r := range v.Rects() {
		if (shell.Rect{X: 3, Y: 7, W: 3, H: 1}) == r {
			t.Fatalf("edit-project button rect leaked into a core tab: %+v", v.Rects())
		}
	}
}
