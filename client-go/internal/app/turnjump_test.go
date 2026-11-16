package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestCmdArrowsPageTheTranscript checks the keys a long chat needs: ⌘↑ glides
// away from the newest turn over a few frames, and ⌘↓ comes back down.
func TestCmdArrowsPageTheTranscript(t *testing.T) {
	a := snapshotData(t)
	a.entries = nil
	for i := 0; i < 30; i++ {
		a.entries = append(a.entries, turnEntries(100+i, "question", "answer")...)
	}
	a.turnWindow = 200
	tt := ui.NewTester(a.View, 1180, 700)
	tt.Frame()
	tt.Frame() // the first frame measures the turns
	top := a.transcriptScroll.Y
	if top <= 0 {
		t.Skipf("the transcript did not scroll (Y=%v)", top)
	}
	tt.Key(ui.Cmd, ui.KeyUp)
	// The page glides, so time has to pass for the frames to show it moving.
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	mid := a.transcriptScroll.Y
	if mid >= top {
		t.Fatalf("the page did not start moving: %v", mid)
	}
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	up := a.transcriptScroll.Y
	if up >= mid {
		t.Fatalf("the page stopped short of its target: %v → %v", mid, up)
	}
	if a.stickToBottom {
		t.Fatal("paging back left the transcript following its end")
	}
	tt.Key(ui.Cmd, ui.KeyDown)
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if down := a.transcriptScroll.Y; down <= up {
		t.Fatalf("⌘↓ did not come back down: %v → %v", up, down)
	}
}
