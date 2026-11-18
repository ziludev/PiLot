package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestRailRowColourGlides checks a session row tints over time instead of
// snapping: the frame 30 ms into the hover is neither the resting row nor the
// hovered one.
//
// The row's lift is the app's own AnimateTo, but colour and shadow belong to
// the framework, via motion.Hover() on the row root. That was left off for a
// long time because a Transition inside ui.List used to paint the rail as
// duplicated rows and white blocks (docs/ui-motion-todo.md §4). With mygo
// v0.10.0 the hovered, scrolled snapshots come out clean, so it is on.
func TestRailRowColourGlides(t *testing.T) {
	const railWidth = 210.0
	a := snapshotData(t)
	tt := ui.NewTester(func(c *ui.Context) {
		a.railWidth = railWidth
		a.rail(c)
	}, int(railWidth), 700)
	tt.Frame()
	row, ok := tt.Find("New chat #13")
	if !ok {
		t.Skipf("the fixture row is not on screen; texts: %v", tt.Texts())
	}
	// A pixel in the row's own padding, below the title: nothing but the row's
	// background reaches it, and the 2 px lift does not move it.
	px, py := int(row.X)-14, int(row.Y+row.H)+4
	rest := tt.Image().RGBAAt(px, py)

	tt.Move(row.X+40, row.Y+row.H/2)
	tt.Advance(30 * time.Millisecond)
	tt.Frame()
	writePNG(t, motionShot("pitta-motion-row-hover-mid.png"), tt.Image())
	early := tt.Image().RGBAAt(px, py)

	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	writePNG(t, motionShot("pitta-motion-row-hover.png"), tt.Image())
	settled := tt.Image().RGBAAt(px, py)
	t.Logf("row background: rest %v, 30 ms in %v, settled %v", rest, early, settled)

	if settled.R > rest.R-4 {
		t.Fatalf("hovering the row did not tint it: rest %v settled %v", rest, settled)
	}
	if early.R == settled.R || early.R == rest.R {
		t.Fatalf("the tint landed in one frame — no Transition on the row: %v %v %v", rest, early, settled)
	}
	if early.R > rest.R || early.R < settled.R {
		t.Fatalf("the tint overshot: rest %v early %v settled %v", rest, early, settled)
	}

	// Rows are recycled by ui.List, and scrolling mid-transition is where the old
	// duplicate-row and white-block mess came from: the snapshot is the assertion,
	// it is looked at by eye.
	tt.Scroll(railWidth/2, 400, 0, -160)
	tt.Advance(16 * time.Millisecond)
	tt.Frame()
	writePNG(t, motionShot("pitta-motion-row-scroll.png"), tt.Image())
}
