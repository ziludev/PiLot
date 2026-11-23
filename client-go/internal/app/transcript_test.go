package app

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
)

// The scroll bar belongs to the pane's edge. The transcript reads in a column
// capped at maxContent, and a bar drawn on that column's edge floats in the
// middle of a wide window: 265px left of the pane at 1600 wide, which is where
// a reader looks for it and does not find it. The bar is the only thing painted
// in the margin, so the test reads the frame and asks for the pane's edge.
func TestTranscriptScrollBarSitsAtPaneEdge(t *testing.T) {
	a := fixtureApp(t, false)
	th := a.thread(11)
	base := append([]protocol.Entry(nil), th.entries...)
	for round := 0; round < 6; round++ {
		for _, entry := range base {
			entry.ID += 1000 * (round + 1)
			th.entries = append(th.entries, entry)
		}
	}
	const width, height = 1600, 900
	tt := ui.NewTester(a.View, width, height)
	tt.SetDark(true)
	tt.Frame()
	// The bar shows while the list overflows and the pointer is over it.
	tt.Move(900, 400)
	tt.Frame()

	if _, last := a.vm.threadList.Visible(); last < 20 {
		t.Fatalf("transcript shows %d rows: nothing to scroll, so no bar", last+1)
	}
	img := tt.Image()
	bar := rightmostLitColumn(img, 280, width, 60, 800)
	if bar < 0 {
		t.Fatal("no scroll bar in the transcript: the picture is of a pane that fits")
	}
	if bar < width-40 {
		t.Fatalf("scroll bar at x=%d of %d: it should hug the pane's edge, not the reading column's", bar, width)
	}
}

// rightmostLitColumn is the rightmost column of the frame with a tall run of
// painted pixels: the scroll bar's thumb, against a background nobody drew on.
func rightmostLitColumn(img *image.RGBA, x0, x1, y0, y1 int) int {
	rightmost := -1
	for x := x0; x < x1; x++ {
		lit := 0
		for y := y0; y < y1; y++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 > 40 || g>>8 > 40 || b>>8 > 40 {
				lit++
			}
		}
		if lit > 100 {
			rightmost = x
		}
	}
	return rightmost
}

// The reading column is capped at maxContent, but a pane narrower than that cap
// has to keep the gutter: without it the text starts on the rail's divider and
// ends on the window's edge, which reads as a rendering fault rather than a
// layout. The gutter rides inside the cap, so this only shows below 860 wide.
func TestNarrowPaneKeepsItsGutter(t *testing.T) {
	const width, height = 900, 700
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, width, height)
	tt.SetDark(true)
	tt.Frame()
	img := tt.Image()
	// The rail ends at railWidth, then the canvas gap and the card's hairline:
	// the transcript's own gutter starts inside the card. Only text counts as a
	// fault here, so the threshold clears the hairline and the card's fill.
	cardEdge := railWidth + paneInset + 1
	for x := cardEdge + 1; x < cardEdge+gutter-4; x++ {
		for y := 60; y < height-120; y++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r>>8 > 120 || g>>8 > 120 || b>>8 > 120 {
				t.Fatalf("painted pixel at x=%d y=%d: the transcript is running into the rail", x, y)
			}
		}
	}
}
