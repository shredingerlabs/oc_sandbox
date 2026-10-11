package shell

// Card-grid geometry shared by the project/edition card views
// (approved layout record #72): 30×4 cards with a one gutter cell
// between horizontal neighbors.
const (
	CardWidth  = 30 // card total width (border + 26 content cols)
	CardHeight = 4  // card total height (border + rows)
	CardGap    = 1  // horizontal gutter between neighbor cards
	CardStride = CardWidth + CardGap
)

// CardRects lays out num cards with cols cards per row, starting at
// (x, y); gapY is the vertical gap (some grids stack cards directly).
// The rects come back in card order (row-major) and pin the exact hit
// numbers the regression tests assert.
func CardRects(x, y, num, cols, gapY int) []Rect {
	out := make([]Rect, 0, max(0, num))
	for i := 0; i < num; i++ {
		c, r := i%cols, i/cols
		out = append(out, Rect{
			X: x + c*CardStride,
			Y: y + r*(CardHeight+gapY),
			W: CardWidth, H: CardHeight,
		})
	}
	return out
}
