package avatar

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// TestOutlineWalksTheBody checks the flattened outline a frame is drawn from:
// its length is the body's own, and walking it returns to where it started.
func TestOutlineWalksTheBody(t *testing.T) {
	if outline.length < 200 || outline.length > 400 {
		t.Fatalf("outline length %.1f is not the body's", outline.length)
	}
	if len(outline.points) < 100 {
		t.Fatalf("outline has %d points: the curves are barely sampled", len(outline.points))
	}
	x, y, ok := outline.at(0)
	if !ok || x != 50 || y != 11 {
		t.Fatalf("the outline starts at %.1f,%.1f; the body starts at 50,11", x, y)
	}
	end, endY, _ := outline.at(1)
	if end != 50 || endY != 11 {
		t.Fatalf("the outline ends at %.1f,%.1f; the body closes where it started", end, endY)
	}
	// Every fraction of the way is a point, and the points move.
	prevX, prevY, _ := outline.at(0)
	moved := 0
	for i := 1; i <= 20; i++ {
		x, y, ok := outline.at(float64(i) / 20)
		if !ok {
			t.Fatalf("no point at %d/20 of the outline", i)
		}
		if x != prevX || y != prevY {
			moved++
		}
		prevX, prevY = x, y
	}
	if moved < 15 {
		t.Fatalf("only %d of 20 samples moved: the walk is stuck", moved)
	}
}

// TestDrawMarkupRevealsTheStroke checks that a frame draws more of the line the
// further it has got, that the fill arrives late, and that the pen sits where the
// ink stops.
func TestDrawMarkupRevealsTheStroke(t *testing.T) {
	offsetAt := func(p float64) float64 {
		markup := drawMarkup(Tones.Idle, p)
		var offset float64
		at := strings.Index(markup, "stroke-dashoffset=")
		if at < 0 {
			t.Fatalf("no dash offset in the frame at %.2f", p)
		}
		if _, err := fmt.Sscanf(markup[at:], `stroke-dashoffset="%f"`, &offset); err != nil {
			t.Fatalf("unreadable dash offset at %.2f: %v", p, err)
		}
		return offset
	}
	if got := offsetAt(0); math.Abs(got-outline.length) > 0.01 {
		t.Fatalf("at the start the offset is %.2f, want the whole length %.2f", got, outline.length)
	}
	if mid := offsetAt(0.5); mid <= 0 || mid >= outline.length {
		t.Fatalf("half way the offset is %.2f, want half of %.2f", mid, outline.length)
	}
	if offsetAt(0.5) <= offsetAt(0.9) {
		t.Fatal("the offset does not shrink as the drawing advances")
	}

	// The fill and the eyes wait until the line is nearly closed, so the drawing
	// reads as a pen rather than as a faded-in shape.
	if !strings.Contains(drawMarkup(Tones.Idle, 0.5), `fill-opacity="0.000"`) {
		t.Fatal("the body is filled before the line has closed")
	}
	if drawMarkup(Tones.Idle, 0.95) == drawMarkup(Tones.Idle, 0.5) {
		t.Fatal("the frame does not change across the drawing")
	}
	if !strings.Contains(drawMarkup(Tones.Idle, 0.5), "<circle") {
		t.Fatal("no pen at the end of the ink")
	}
}

// TestFlattenRejectsAnUnknownCommand pins the guard: the path is a constant, so a
// command the walker cannot follow has to fail loudly rather than draw a face
// slightly wrong.
func TestFlattenRejectsAnUnknownCommand(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("flatten accepted a command it cannot walk")
		}
	}()
	flatten("M10 10 H20", 4)
}
