package brand

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/theme"
)

// TestMarkupIsTheShippedGlyph pins the two paths to the ones in
// resources/pitta-logo.svg: the mark is the shipped one, not a re-estimate.
func TestMarkupIsTheShippedGlyph(t *testing.T) {
	markup := frameMarkup(MoodIdle, 0, "#111111")
	for _, want := range []string{
		`fill-rule="evenodd"`,
		flame,
		hole,
		stand,
		`stroke-width="17"`,
		`viewBox="0 0 256 256"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("the mark is missing %q", want)
		}
	}
}

// TestOutlineWalksTheFlame checks the walk the pen takes: it starts at the flame's
// base, moves, and comes back to where it started.
func TestOutlineWalksTheFlame(t *testing.T) {
	if len(flameOutline.points) < 100 {
		t.Fatalf("the flame has %d points: the curves are barely sampled", len(flameOutline.points))
	}
	if flameOutline.length < 200 || flameOutline.length > 400 {
		t.Fatalf("flame length %.1f is not the glyph's", flameOutline.length)
	}
	if x, y, ok := flameOutline.at(0); !ok || x != baseX || y != baseY {
		t.Fatalf("the walk starts at %.1f,%.1f; the flame starts at %.0f,%.0f", x, y, baseX, baseY)
	}
	endX, endY, _ := flameOutline.at(1)
	if endX != baseX || endY != baseY {
		t.Fatalf("the walk ends at %.1f,%.1f; the flame closes where it started", endX, endY)
	}
	if standOutline.length < 100 {
		t.Fatalf("the stand measures %.1f, too short to be the line under the flame", standOutline.length)
	}
}

// TestDrawingRevealsTheStroke checks the self-drawing frames: the flame's outline
// comes first, the stand after it, the filled shape last, and the pen sits on the
// ink until the shape arrives.
func TestDrawingRevealsTheStroke(t *testing.T) {
	offsetAt := func(p float64) float64 {
		markup := drawingMarkup(p, "#111111")
		at := strings.Index(markup, "stroke-dashoffset=")
		if at < 0 {
			t.Fatalf("no dash offset at %.2f", p)
		}
		var offset float64
		if _, err := fmt.Sscanf(markup[at:], `stroke-dashoffset="%f"`, &offset); err != nil {
			t.Fatalf("unreadable dash offset at %.2f: %v", p, err)
		}
		return offset
	}
	if got := offsetAt(0); got < flameOutline.length-0.01 {
		t.Fatalf("at the start the offset is %.2f, want the whole outline %.2f", got, flameOutline.length)
	}
	if offsetAt(0.4) >= offsetAt(0.1) {
		t.Fatal("the outline is not drawn a little more as the pen advances")
	}
	if strings.Contains(drawingMarkup(0.3, "#111111"), `stroke-width="17"`) {
		t.Fatal("the stand is drawn before the flame has closed")
	}
	if !strings.Contains(drawingMarkup(0.86, "#111111"), `stroke-width="17"`) {
		t.Fatal("the stand is not drawn once the flame has closed")
	}
	if strings.Contains(drawingMarkup(0.5, "#111111"), `fill-opacity`) {
		t.Fatal("the shape is filled before the pen has finished")
	}
	if !strings.Contains(drawingMarkup(1, "#111111"), `fill-opacity="1.000"`) {
		t.Fatal("the finished drawing is not the filled mark")
	}
	if !strings.Contains(drawingMarkup(0.5, "#111111"), "<circle") {
		t.Fatal("no pen while the outline is being drawn")
	}
	if strings.Contains(drawingMarkup(1, "#111111"), "<circle") {
		t.Fatal("the pen is still there after the mark is drawn")
	}
}

// TestMoodsMoveTheFlame checks that each mood animates, that they are told apart,
// and that the stand never moves: it is the floor the flame moves over.
func TestMoodsMoveTheFlame(t *testing.T) {
	stand := `d="` + stand + `"`
	frames := func(mood Mood) map[string]bool {
		seen := map[string]bool{}
		for f := 0; f < Frames; f++ {
			markup := frameMarkup(mood, f, "#111111")
			if !strings.Contains(markup, stand) {
				t.Fatalf("mood %d frame %d moved the stand", mood, f)
			}
			seen[markup] = true
		}
		return seen
	}
	idle, thinking, happy := frames(MoodIdle), frames(MoodThinking), frames(MoodHappy)
	if len(idle) < 4 || len(thinking) < 8 || len(happy) < 3 {
		t.Fatalf("distinct frames: idle %d, thinking %d, happy %d", len(idle), len(thinking), len(happy))
	}
	for markup := range thinking {
		if idle[markup] {
			t.Fatal("the flickering mood repeats an idle frame")
		}
	}
	// The beat is short: most of the happy loop is the still mark.
	if len(happy) > Frames/2 {
		t.Fatalf("the beat covers %d of %d frames", len(happy), Frames)
	}
}

// TestFlattenRejectsAnUnknownCommand pins the guard: the paths are authored by
// hand, so a command the walker cannot follow fails loudly.
func TestFlattenRejectsAnUnknownCommand(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("flatten accepted a command it cannot walk")
		}
	}()
	flatten("M10 10 H20", 4)
}

// TestInkMatchesTheTheme checks the colour the mark is painted in, since the SVG
// needs a literal and the window gives a ui.Color.
func TestInkMatchesTheTheme(t *testing.T) {
	if got := Ink(ui.Hex("#0a0a0a")); got != "#0a0a0a" {
		t.Fatalf("Ink(#0a0a0a) = %q", got)
	}
}

// TestDrawMovesOverTime checks the loop is wired to the window: two frames a few
// hundred milliseconds apart are not the same picture.
func TestDrawMovesOverTime(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Light())
		Draw(c, 32, MoodThinking, "#111111")
	}, 48, 48)
	tt.Frame()
	first := tt.Image()
	before := make([]byte, len(first.Pix))
	copy(before, first.Pix)
	tt.Advance(240 * time.Millisecond)
	tt.Frame()
	if bytes.Equal(before, tt.Image().Pix) {
		t.Fatal("the mark did not move: the animation is not reaching the window")
	}
}
