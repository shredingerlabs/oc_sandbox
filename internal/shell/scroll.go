package shell

// WheelStep is the scroll distance of one mouse-wheel notch, shared by
// every scrollable center view.
const WheelStep = 3

// FollowMargin is how many body rows around a focused row stay visible
// when the scroll follows it (the margin-mode panes; the flush panes
// keep the row flush with the window bottom).
const FollowMargin = 3

// Follow is the shared follow bookkeeping of one scrollable pane: all
// four center views hand-rolled the same wheel + focus-follow pattern
// with four different state shapes — wheel free-scroll pauses the
// follow, keyboard navigation re-arms it, and the pull only fires when
// the focused cursor moved since the last follow (wheel scrolling never
// moves the focus, so it never re-triggers the pull).
type Follow struct {
	on     bool // follow engaged: the focused row pulls the window
	prevID int  // focused cursor of the last pass (pull only on move)
	force  bool // next Pull runs regardless of the cursor (forced re-arm)
	pause  bool // wheel scrolling disengages the follow
	margin bool // margin-mode pull geometry
}

// NewFollow returns a disengaged follow; wheelPauses selects Wheel
// behavior (wheel scrolling disengages vs. leaves engaged), margin the
// pull geometry (window-flush vs. FollowMargin context rows).
func NewFollow(wheelPauses, margin bool) Follow {
	return Follow{pause: wheelPauses, margin: margin}
}

// Arm re-engages the follow (keyboard navigation re-arms the pull).
func (f *Follow) Arm() { f.on = true }

// Force makes the next Pull fire even when the cursor did not move
// (a re-armed follow, e.g. a fresh running-container list).
func (f *Follow) Force() { f.force = true }

// Wheel applies one wheel notch to the offset (dir +1 wheel-down,
// -1 wheel-up) and, when set, disengages the follow.
func (f *Follow) Wheel(offset *int, dir int) {
	if f.pause {
		f.on = false
	}
	*offset = max(0, *offset+dir*WheelStep)
}

// Pull runs the shared follow recipe: when the follow is engaged and
// the focused cursor moved since the last pass (or Force armed it),
// pull the offset so focusRow stays inside the window [offset,
// offset+avail); the offset is then clamped to [0, rows-avail).
func (f *Follow) Pull(offset *int, id, focusRow, rows, avail int) {
	moved := f.force || f.prevID != id
	f.force, f.prevID = false, id
	if f.on && moved {
		pullWindow(offset, focusRow, rows, avail, f.margin)
	}
	clampScroll(offset, rows, avail)
}

// pullWindow adjusts the offset so focusRow stays visible: pulled down
// it keeps `top` margin rows above the row; pulled up it lands the row
// flush with the window bottom (margin=false) or with FollowMargin-1
// slack rows below it (margin=true).
func pullWindow(offset *int, focusRow, rows, avail int, margin bool) {
	if focusRow < 0 { // focus outside the laid-out rows: nothing to keep
		return
	}
	land, top := avail-1, 0
	if margin {
		land, top = avail-FollowMargin, FollowMargin
	}
	if focusRow < *offset {
		*offset = max(0, focusRow-top)
		return
	}
	if focusRow >= *offset+land+1 {
		*offset = min(max(0, rows-avail), focusRow-land)
	}
}

// clampScroll bounds the offset to [0, max(0, rows-avail)].
func clampScroll(offset *int, rows, avail int) {
	*offset = min(max(0, rows-avail), max(0, *offset))
}
