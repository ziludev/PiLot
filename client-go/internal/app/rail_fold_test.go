package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/motion"
)

// TestArchivedRowFoldsOut checks that archiving a chat closes its row instead of
// deleting it between two frames: the row keeps its place — and its title — for
// motion.RowLeave, and the rows under it slide up as it shrinks.
//
// The fold is an AnimateTo on the row, not an Exit motion: a ui.List row is gone
// the moment the data says so, so there is nothing left to run an exit on (see
// docs/mygo-version.md §5).
func TestArchivedRowFoldsOut(t *testing.T) {
	const railWidth = 210.0
	const victim = 13 // the untitled fixture chat, "New chat #13"

	a := snapshotData(t)
	tt := ui.NewTester(func(c *ui.Context) {
		a.railWidth = railWidth
		a.rail(c)
	}, int(railWidth), 700)
	tt.Frame()

	rest, ok := tt.Find("Feishu: deploy")
	if !ok {
		t.Skipf("the fixture rail is not laid out as expected: %v", tt.Texts())
	}

	a.apply(func() { a.markArchived(victim, true) })
	tt.Frame()
	if _, still := tt.Find("New chat #13"); !still {
		t.Fatal("the archived row left the rail in the same frame — no fold")
	}
	if start, ok := tt.Find("Feishu: deploy"); !ok || start.Y != rest.Y {
		t.Fatalf("the row below jumped while the fold started: rest %v now %v", rest, start)
	}
	writePNG(t, motionShot("pitta-motion-rail-fold-start.png"), tt.Image())

	tt.Advance(motion.RowLeave / 2)
	tt.Frame()
	writePNG(t, motionShot("pitta-motion-rail-fold-mid.png"), tt.Image())
	mid, ok := tt.Find("Feishu: deploy")
	if !ok || mid.Y >= rest.Y {
		t.Fatalf("the row below has not slid up half way through the fold: rest %v mid %v", rest, mid)
	}

	// The prune is a real timer, so the test waits it out.
	time.Sleep(motion.RowLeave + 40*time.Millisecond)
	tt.Frame()
	if _, gone := tt.Find("New chat #13"); gone {
		t.Fatal("the folded row is still in the rail")
	}
	settled, ok := tt.Find("Feishu: deploy")
	if !ok {
		t.Fatal("the row below the fold vanished")
	}
	if settled.Y >= mid.Y {
		t.Fatalf("the rows under the fold did not finish sliding up: mid %v settled %v", mid, settled)
	}
	writePNG(t, motionShot("pitta-motion-rail-fold-settled.png"), tt.Image())

	// Undoing it while the fold is in flight must bring the row straight back.
	a.apply(func() { a.markArchived(victim, false) })
	tt.Frame()
	if _, back := tt.Find("New chat #13"); !back {
		t.Fatal("un-archiving did not restore the row")
	}
}
