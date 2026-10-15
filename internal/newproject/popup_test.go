package newproject

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/cmdrun"
)

// Create confirmation popup (ADR-0021): the trigger shows the current
// config summary (name/path/edition/modes/start/VCS) with Confirm/Back;
// esc cancels without running anything.
func TestCreatePopupShowsConfigSummary(t *testing.T) {
	m, ops, _ := newTestModel(t)
	typeKeys(t, m, "popproj")
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if !m.ConfirmOpen() {
		t.Fatal("Create Project button did not open the popup")
	}
	b := plain(t, m)
	for _, want := range []string{
		"Create this project?",
		"name: popproj",
		"edition: ",
		"modes: none",
		"start: console",
		"VCS tracking: none",
		"Back",
		"Create",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("popup missing %q:\n%s", want, b)
		}
	}

	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.ConfirmOpen() {
		t.Fatal("esc did not cancel the popup")
	}
	if ops.total() != 0 {
		t.Fatalf("cancelled popup ran scripts: %v", ops.Calls)
	}
}

// While the selected edition's image is missing, the popup summary also
// lists the recorded "Image build" plan (ADR-0021 Create popup).
func TestCreatePopupShowsBuildPlanWhenImageMissing(t *testing.T) {
	m, ops, _ := newTestModel(t, func(ops *recOps, run *cmdrun.Fake) { withoutImage(run) })
	typeKeys(t, m, "buildpop")
	m.tabToField(t, "Image build")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // flip to Build later
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	b := plain(t, m)
	if !m.ConfirmOpen() {
		t.Fatalf("popup missing:\n%s", b)
	}
	if !strings.Contains(b, "image build: Build later") {
		t.Fatalf("popup missing the recorded build plan:\n%s", b)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc}) // cancel, nothing runs
	if ops.total() != 0 {
		t.Fatalf("cancelled popup ran scripts: %v", ops.Calls)
	}
}

func TestCreatePopupKeyboardSteps(t *testing.T) {
	m, ops, reg := newTestModel(t)
	typeKeys(t, m, "stepspop")
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // popup
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // enter on Cancel: no run
	if ops.total() != 0 {
		t.Fatalf("enter on the default focus ran the create: %v", ops.Calls)
	}
	// trigger again, then confirm via tab+enter
	m.tabToField(t, "Create Project")
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.ConfirmOpen() {
		t.Fatalf("popup missing on second trigger:\n%s", plain(t, m))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if p, err := reg.ByName("stepspop"); err != nil || p.ContainerStatus != "stopped" {
		t.Fatalf("tab+enter did not confirm the create: %+v %v", p, err)
	}
}
