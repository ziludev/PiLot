package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/theme"
)

// The report: with the pointer on the seam between two chats the list flickers.
// What this test asks is whether the rail settles there: a row that lifts under
// the pointer must not take its own hit box away, or the hover it just answered
// falls back to the row below and the two chase each other frame after frame.
func TestRailHoverOnTheSeamSettles(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.rail(c) }, 260, 560)
	tt.Frame()
	upper, ok := tt.Find("Feishu: deploy")
	if !ok {
		t.Fatalf("no upper row; texts=%q", tt.Texts())
	}
	if _, ok := tt.Find("New chat #13"); !ok {
		t.Fatalf("no lower row; texts=%q", tt.Texts())
	}
	// The rows are 28 DIPs tall and the label sits centred in the row, so the
	// seam between the two is one half row under the label's middle, less a
	// half DIP: inside the upper row, and inside its own lift.
	seam := upper.Y + upper.H/2 + 14 - 0.5
	tt.Move(upper.X+40, seam)

	lifts := make([]float32, 0, 8)
	for i := 0; i < 8; i++ {
		tt.Frame()
		tt.Advance(20 * time.Millisecond)
		r, ok := tt.Find("Feishu: deploy")
		if !ok {
			t.Fatalf("the row went away; texts=%q", tt.Texts())
		}
		lifts = append(lifts, r.Y-upper.Y)
	}
	// A settled row walks one way to where the pointer put it: at rest, or
	// lifted. A row whose own lift pushes the pointer off it turns back round
	// frame after frame, and the lift never lands.
	turns := 0
	for i := 2; i < len(lifts); i++ {
		was, is := lifts[i-1]-lifts[i-2], lifts[i]-lifts[i-1]
		if was*is < 0 {
			turns++
		}
	}
	t.Logf("lift over frames at the seam: %v", lifts)
	if turns > 1 {
		t.Fatalf("the row reversed %d times on the seam, never settling: %v", turns, lifts)
	}
}
