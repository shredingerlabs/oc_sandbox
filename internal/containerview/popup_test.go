package containerview

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Build confirmation popup (ADR-0021): the idle triggers show the popup
// with the edition context; esc/q cancels without running; while an op
// runs the trigger skips the popup (busy priority).

func TestBuildPopupContentAndCancel(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	m.Body(80, 22)
	m.Update(keyType(tea.KeyEnter)) // build trigger → popup
	if got := body(m); !strings.Contains(got, "Build the all image?") || !strings.Contains(got, "edition: all") {
		t.Fatalf("popup content wrong:\n%s", got)
	}

	m.Update(keyType(tea.KeyEsc))
	if m.ConfirmOpen() {
		t.Fatal("esc did not cancel the build popup")
	}
	if n := len(fake.Calls); n != 0 {
		t.Fatalf("cancelled popup ran a build: %v", fake.Calls)
	}
}

func TestBuildPopupOnlyWhenIdle(t *testing.T) {
	m, fake, _, _ := newFixture(t)
	m.Body(80, 22)
	m.Update(keyType(tea.KeyEnter)) // popup (ADR-0021)
	confirmKeys(m)

	if !m.Busy() {
		t.Fatal("confirmed build did not start")
	}
	// second trigger while busy: no popup again, existing busy priority
	prev := len(fake.Calls)
	m.Update(keyType(tea.KeyEnter)) // busy → "operation already in progress" toast, no popup
	if m.ConfirmOpen() {
		t.Fatal("trigger while busy opened a second popup")
	}
	if len(fake.Calls) != prev {
		t.Fatalf("busy trigger started a second build: %v", fake.Calls)
	}
}

// confirmKeys steps through the popup: tab focuses Confirm, enter runs.
func confirmKeys(m *Model) tea.Cmd {
	m.Update(keyType(tea.KeyTab))
	return m.Update(keyType(tea.KeyEnter))
}
