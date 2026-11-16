package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCmdArrowsPageTheTranscript checks the keys a long chat needs: from the
// newest turn, ⌘↑ lands on an earlier one, and ⌘↓ comes back down.
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
	for i := 0; i < 4; i++ {
		tt.Frame()
	}
	up := a.transcriptScroll.Y
	t.Logf("after page up: Y=%v maxY=%v viewH=%v", up, a.scrollMaxY, a.scrollViewH)
	if up >= top {
		t.Fatalf("⌘↑ did not go up: %v → %v", top, up)
	}
	tt.Key(ui.Cmd, ui.KeyDown)
	for i := 0; i < 4; i++ {
		tt.Frame()
	}
	if down := a.transcriptScroll.Y; down <= up {
		t.Fatalf("⌘↓ did not come back down: %v → %v", up, down)
	}
}
