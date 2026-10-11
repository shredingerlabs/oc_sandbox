package shell

import (
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shredingerlabs/oc-sandbox/internal/taskmgr"
)

// Guarded exit (#82): quitting while background work runs asks for
// confirmation first; a second quit passes, any other key cancels.

type probe struct {
	Model
	busy bool
	seen atomic.Int32 // DoneMsg deliveries (broadcast check)
}

func (p *probe) Body(w, h int) string { return "" }

func (p *probe) SubTabs() ([]string, int) { return nil, 0 }

func (p *probe) TextFocused() bool { return false }

func (p *probe) Busy() bool { return p.busy }

func (p *probe) Update(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(taskmgr.DoneMsg); ok {
		p.seen.Add(1)
	}
	return nil
}

func TestGuardedExitAsksConfirmationWhileBusy(t *testing.T) {
	root := New(&probe{busy: true}, &probe{busy: true})
	model, _ := root.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m := model.(Model)
	if m.Quit() {
		t.Fatal("quit passed through the guard while work ran")
	}
	if !m.ExitPending() {
		t.Fatal("no exit confirmation requested")
	}
	// any other key cancels the confirmation
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = model.(Model)
	if m.ExitPending() {
		t.Fatal("confirmation not cancelled by another key")
	}
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = model.(Model)
	if m.Quit() {
		t.Fatal("quit should stay blocked until confirmed")
	}
	if !m.ExitPending() {
		t.Fatal("second quit did not raise confirmation")
	}
	// idle → quit passes at once
	for _, p := range m.views {
		p.(*probe).busy = false
	}
	model, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !model.(Model).Quit() {
		t.Fatal("idle quit still blocked")
	}
}

// Task messages are broadcast to every center view (each view keeps its
// own owner filtering) and the queue's Update runs after them exactly
// once per delivered message.
func TestTaskMessagesBroadcastToEveryViewOnce(t *testing.T) {
	root := New(&probe{}, &probe{})
	root.Queue = taskmgr.New()
	msg := taskmgr.DoneMsg{ID: 1, Key: "k"}

	model, _ := root.Update(msg)
	for i, p := range model.(Model).views {
		if p.(*probe).seen.Load() != 1 {
			t.Fatalf("view %d missed the broadcast", i)
		}
	}
	// a duplicate delivery still reaches the views (idempotent for the queue)
	model, _ = model.(Model).Update(msg)
	for i, p := range model.(Model).views {
		if p.(*probe).seen.Load() != 2 {
			t.Fatalf("view %d missed the duplicate", i)
		}
	}
}
