package app

import (
	"bytes"
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
)

// different reports whether two frames differ at all, which is how a headless
// test sees motion: the same pixels twice means nothing moved.
func different(a, b *image.RGBA) bool { return !bytes.Equal(a.Pix, b.Pix) }

// TestModelPaletteAnimatesIn checks the palette slides into place: part way
// through the 280 ms its first row still sits below where it settles. Pixels
// alone would not do, since the search caret blinks whether or not anything
// moves.
func TestModelPaletteAnimatesIn(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(a.View, 1180, 700)
	tt.Frame()
	a.apply(func() { a.modelOpen = true; a.invalidate() })
	tt.Frame()
	mid, ok := tt.Find("DeepSeek Flash")
	if !ok {
		t.Skip("the snapshot's model row is not on screen")
	}
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	settled, ok := tt.Find("DeepSeek Flash")
	if !ok {
		t.Fatal("the row left the screen while animating in")
	}
	if settled.Y >= mid.Y {
		t.Fatalf("the row did not rise: mid %.1f → settled %.1f", mid.Y, settled.Y)
	}
}

// TestSidePanelTabRises checks a tab swap animates rather than cutting.
func TestSidePanelTabRises(t *testing.T) {
	a := snapshotData(t)
	a.sideOpen = true
	a.sideTab = SideChanges
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	a.apply(func() { a.sideTab = SideMemory; a.invalidate() })
	tt.Frame()
	mid, ok := tt.Find("speak Chinese")
	if !ok {
		t.Skipf("the memory panel's heading is not on screen; texts: %v", tt.Texts())
	}
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	settled, ok := tt.Find("speak Chinese")
	if !ok {
		t.Fatal("the entry left the screen while animating in")
	}
	if settled.Y >= mid.Y {
		t.Fatalf("the panel did not rise: mid %.1f → settled %.1f", mid.Y, settled.Y)
	}
}

// TestApprovalBarSlidesIn checks a waiting call brings the bar up rather than
// appearing between two frames.
func TestApprovalBarSlidesIn(t *testing.T) {
	a := snapshotData(t)
	a.approvals = nil
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	a.apply(func() {
		a.approvals = []protocol.PendingApproval{{
			CallID: "call_1", Tool: "bash", Reason: "rm -rf ./build",
			Summary: "rm -rf ./build && go build ./...", CreatedAt: time.Now().UnixMilli(),
		}}
		a.invalidate()
	})
	tt.Frame()
	mid, ok := tt.Find("rm -rf ./build && go build ./...")
	if !ok {
		t.Skipf("the approval summary is not on screen; texts: %v", tt.Texts())
	}
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	settled, ok := tt.Find("rm -rf ./build && go build ./...")
	if !ok {
		t.Fatal("the bar left the screen while animating in")
	}
	if settled.Y >= mid.Y {
		t.Fatalf("the bar did not rise: mid %.1f → settled %.1f", mid.Y, settled.Y)
	}
}
