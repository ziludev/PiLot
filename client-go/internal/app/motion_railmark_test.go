package app

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/motion"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// markInk is the darkest pixel in the column a row gives to its mark: a filled
// dot is darkest at its middle, a ring at its edge, and both fade by turning
// towards the page under them.
func markInk(img *image.RGBA, r ui.Rect) int {
	darkest := 0
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			if x < 0 || y < 0 || x >= img.Bounds().Max.X || y >= img.Bounds().Max.Y {
				continue
			}
			p := img.RGBAAt(x, y)
			if lum := 255 - (int(p.R)+int(p.G)+int(p.B))/3; lum > darkest {
				darkest = lum
			}
		}
	}
	return darkest
}

// markColumn finds the slot of one chat's row.
func markColumn(t *testing.T, tt *ui.Tester, conversationID int) (ui.Rect, bool) {
	t.Helper()
	return tt.Find(railMarkLabelPrefix + itoa(conversationID))
}

// TestRailMarkFadesOutWhenTheWorkIsDone is the state change a row has to show:
// a chat that stops working loses its mark over motion.MarkFade rather than
// between two polls, so the row is seen to settle.
func TestRailMarkFadesOutWhenTheWorkIsDone(t *testing.T) {
	a := snapshotData(t)
	a.unread = map[int]bool{13: true}
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.railWidth = 210; a.rail(c) }, 210, 420)
	tt.Frame()
	column, ok := markColumn(t, tt, 13)
	if !ok {
		t.Fatalf("the unread row kept no column for its mark: %v", tt.Texts())
	}
	full := markInk(tt.Image(), column)
	if full < 40 {
		t.Fatalf("the unread mark is too faint to measure: %d", full)
	}

	// Read: the mark has to leave.
	a.apply(func() { a.unread = map[int]bool{} })
	tt.Frame()
	if ink := markInk(tt.Image(), column); ink < full/2 {
		t.Fatalf("the mark dropped from %d to %d in one frame: it blinked rather than faded", full, ink)
	}
	tt.Advance(motion.MarkFade / 2)
	tt.Frame()
	mid := markInk(tt.Image(), column)
	if mid > full*9/10 || mid < full/10 {
		t.Fatalf("half way through the fade the mark is %d, between %d and nothing: not a fade", mid, full)
	}
	tt.Advance(motion.MarkFade)
	tt.Frame()
	if _, still := markColumn(t, tt, 13); still {
		t.Fatalf("the mark is still drawn %v after the work finished", motion.MarkFade)
	}
}

// TestRunningRingBreathes checks the other half: a ring that sits still reads as
// a state, so the ring of a chat that is working moves while it works.
func TestRunningRingBreathes(t *testing.T) {
	a := snapshotData(t)
	a.tasks = []protocol.Task{{ID: 6, ConversationID: 13, State: "running"}}
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.railWidth = 210; a.rail(c) }, 210, 420)
	tt.Frame()
	column, ok := markColumn(t, tt, 13)
	if !ok {
		t.Fatalf("the running row kept no column for its ring: %v", tt.Texts())
	}
	early := markInk(tt.Image(), column)

	tt.Advance(motion.Breathe / 2)
	tt.Frame()
	late := markInk(tt.Image(), column)
	if absInt(early-late) < 12 {
		t.Fatalf("the ring reads %d at the start of a breath and %d half a period later: it does not move", early, late)
	}
}
