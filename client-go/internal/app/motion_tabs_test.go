package app

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// tabMarker finds the underline under the panel's tabs: the longest run of dark
// pixels in the rows just below the tab labels, which the filled tab pill and
// the panel's own rules do not reach.
func tabMarker(img *image.RGBA, left int, labelBottom float32) (lo, hi int) {
	bounds := img.Bounds()
	// Starts below the tab pill, so a descender in a label is not mistaken for
	// the marker.
	for y := int(labelBottom) + 5; y < int(labelBottom)+16 && y < bounds.Max.Y; y++ {
		run, start := 0, -1
		for x := left; x < bounds.Max.X; x++ {
			c := img.RGBAAt(x, y)
			lum := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
			if lum < 150 {
				if run == 0 {
					start = x
				}
				run++
				continue
			}
			if run > hi-lo {
				lo, hi = start, start+run
			}
			run = 0
		}
		if run > hi-lo {
			lo, hi = start, start+run
		}
	}
	// A marker is as wide as a tab; a few dark pixels are text.
	if hi-lo < 20 {
		return 0, 0
	}
	return lo, hi
}

func mid(a, b int) float32 { return float32(a+b) / 2 }

// TestTabMarkerTravels is the proof that the underline under the side panel's
// tabs slides from the tab that was picked to the one just picked instead of
// blinking across: half way through it sits between the two, and it lands under
// the new tab.
func TestTabMarkerTravels(t *testing.T) {
	a := snapshotData(t)
	a.sideOpen = true
	a.sideTab = SideChanges
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()

	changes, ok := tt.Find("Changes")
	if !ok {
		t.Skipf("the changes tab is not on screen; texts: %v", tt.Texts())
	}
	usage, ok := tt.Find("Usage")
	if !ok {
		t.Fatal("the usage tab is not on screen")
	}
	left := int(changes.X) - 20
	startLo, startHi := tabMarker(tt.Image(), left, changes.Y+changes.H)
	if startHi <= startLo {
		t.Fatal("no underline under the picked tab")
	}
	writePNG(t, motionShot("pitta-motion-tabs-start.png"), tt.Image())

	a.apply(func() { a.sideTab = SideUsage; a.invalidate() })
	tt.Frame()
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	movingLo, movingHi := tabMarker(tt.Image(), left, changes.Y+changes.H)
	writePNG(t, motionShot("pitta-motion-tabs-mid.png"), tt.Image())

	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	endLo, endHi := tabMarker(tt.Image(), left, changes.Y+changes.H)
	writePNG(t, motionShot("pitta-motion-tabs-end.png"), tt.Image())

	if endHi <= endLo {
		t.Fatal("the underline went missing after the switch")
	}
	// It has to end up under the tab it marks, not beside it.
	if float32(endLo) < usage.X-16 || float32(endHi) > usage.X+usage.W+16 {
		t.Fatalf("the underline sits at %d…%d, want it under the usage tab at %.0f…%.0f",
			endLo, endHi, usage.X, usage.X+usage.W)
	}
	if mid(endLo, endHi) <= mid(startLo, startHi) {
		t.Fatalf("the underline went from %d…%d to %d…%d: it did not travel right",
			startLo, startHi, endLo, endHi)
	}
	// Mid-way it is somewhere in between: a marker that blinked would already
	// be at the end.
	if movingLo == endLo && movingHi == endHi {
		t.Fatalf("the underline jumped to %d…%d instead of travelling", movingLo, movingHi)
	}
	if mid(movingLo, movingHi) < mid(startLo, startHi)-1 {
		t.Fatalf("the underline overshot backwards: %d…%d", movingLo, movingHi)
	}
	if mid(movingLo, movingHi) > mid(endLo, endHi)+1 {
		t.Fatalf("the underline overshot forwards: %d…%d", movingLo, movingHi)
	}
}
