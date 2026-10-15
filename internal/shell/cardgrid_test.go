package shell

import "testing"

func TestCardRectsSharedGeometry(t *testing.T) {
	got := CardRects(1, 3, 5, 2, 1)
	want := []Rect{
		{X: 1, Y: 3, W: 30, H: 5},
		{X: 32, Y: 3, W: 30, H: 5},
		{X: 1, Y: 9, W: 30, H: 5},
		{X: 32, Y: 9, W: 30, H: 5},
		{X: 1, Y: 15, W: 30, H: 5},
	}
	for i, r := range want {
		if got[i] != r {
			t.Fatalf("card rect %d = %+v, want %+v", i, got[i], r)
		}
	}
	noGap := CardRects(1, 3, 3, 2, 0)
	if noGap[2].Y != 8 { // second row stacks directly on the first
		t.Fatalf("gapY=0 row 2 Y = %d, want 8", noGap[2].Y)
	}
}
