package shell

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// DblClickWindow is the inter-press window for double-click detection
// (terminal norms ≈ 350–500 ms).
const DblClickWindow = 400 * time.Millisecond

// DblClick tracks the last left-button press per cell. BubbleTea v1 has
// no ClickCount (research #64), so views share this tracker: Press
// returns 1 (single click), 2 (double click), or 0 (not a left press).
type DblClick struct {
	lastTime time.Time
	lastX    int
	lastY    int
}

// Press consumes a mouse message; a second left press of the same cell
// within DblClickWindow counts as a double (and is consumed so a third
// press starts fresh).
func (d *DblClick) Press(msg tea.MouseMsg) int {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return 0
	}
	now := time.Now()
	if !d.lastTime.IsZero() &&
		now.Sub(d.lastTime) <= DblClickWindow &&
		msg.X == d.lastX && msg.Y == d.lastY {
		d.lastTime = time.Time{}
		return 2
	}
	d.lastTime = now
	d.lastX, d.lastY = msg.X, msg.Y
	return 1
}
