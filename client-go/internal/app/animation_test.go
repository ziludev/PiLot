package app

import (
	"bytes"
	"encoding/json"
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

// TestStreamingCaretBlinks checks the caret at the end of a streaming answer
// comes and goes. It follows the caret's own element rather than a band of
// pixels: the logo beside the phase animates now, and a band also caught the
// card's edge as the caret came and went.
func TestStreamingCaretBlinks(t *testing.T) {
	a := snapshotData(t)
	generation, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "writing"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	a.live = &protocol.LiveDoc{Run: json.RawMessage(`{"id":"run_1"}`), Generation: generation}
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if _, ok := tt.Find("writing"); !ok {
		t.Skip("the streaming text is not on screen")
	}
	caret, ok := tt.Find(caretLabel)
	if !ok {
		t.Fatalf("the caret is not drawn while the answer streams")
	}
	band := image.Rect(int(caret.X), int(caret.Y), int(caret.X+caret.W), int(caret.Y+caret.H))
	// The caret's colour, read off the caret: the test does not need to know
	// which theme the window is in to know that its mark went away.
	c := tt.Image().RGBAAt(band.Min.X+band.Dx()/2, band.Min.Y+band.Dy()/2)
	ink := ui.Color{R: c.R, G: c.G, B: c.B, A: c.A}
	if on := countColour(tt.Image(), band, ink); on == 0 {
		t.Fatalf("the caret is laid out but painted in nothing")
	}
	// Half a period later it is gone from the frame, not merely moved.
	tt.Advance(560 * time.Millisecond)
	tt.Frame()
	if _, ok := tt.Find(caretLabel); ok {
		t.Fatalf("the caret is still drawn half a blink later")
	}
	if off := countColour(tt.Image(), band, ink); off != 0 {
		t.Fatalf("the caret left %d pixels of its colour behind", off)
	}
}

// countColour counts the pixels of one colour inside a band, within the same
// small tolerance the window colours are read with.
func countColour(img *image.RGBA, band image.Rectangle, want ui.Color) int {
	n := 0
	for y := max(band.Min.Y, 0); y < min(band.Max.Y, img.Bounds().Dy()); y++ {
		for x := max(band.Min.X, 0); x < min(band.Max.X, img.Bounds().Dx()); x++ {
			c := img.RGBAAt(x, y)
			if nearColor(c.R, want.R) && nearColor(c.G, want.G) && nearColor(c.B, want.B) {
				n++
			}
		}
	}
	return n
}
