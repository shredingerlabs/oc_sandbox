// Double-click detection: BubbleTea v1 has no ClickCount (research #64).
// Track last press per cell; second press of same button/cell within
// dblClickWindow counts. THROWAWAY prototype (issue #69).
package protoapp

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const dblClickWindow = 400 * time.Millisecond

type dblClickTracker struct {
	lastTime  time.Time
	lastX     int
	lastY     int
	lastTea   tea.MouseButton
	pendingID uint64
	nextID    uint64
}

// press consumes a press event; returns n=1 (single), 2 (double), 0 (ignore).
func (d *dblClickTracker) press(msg tea.MouseMsg) int {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return 0
	}
	now := time.Now()
	if !d.lastTime.IsZero() &&
		now.Sub(d.lastTime) <= dblClickWindow &&
		msg.X == d.lastX && msg.Y == d.lastY && msg.Button == d.lastTea {
		// Double: consume so a third press starts fresh.
		d.lastTime = time.Time{}
		return 2
	}
	d.lastTime = now
	d.lastX, d.lastY, d.lastTea = msg.X, msg.Y, msg.Button
	return 1
}
