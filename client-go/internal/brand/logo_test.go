package brand

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/theme"
)

// TestMarkupIsTheShippedGlyph pins the parts the frames are built from: the four
// modules, the stem, the arrow the mask cuts out and the square box the mark is
// padded into.
func TestMarkupIsTheShippedGlyph(t *testing.T) {
	markup := frameMarkup(MoodIdle, 0, "#111111")
	for _, want := range []string{
		`viewBox="0 -31 350 350"`,
		`id="symbol-arrow-cutout"`,
		`id="blue-module"`,
		`id="coral-module"`,
		`id="purple-module"`,
		`id="yellow-module"`,
		`id="pi-stem"`,
		`fill="#111111"`,
	} {
		if !strings.Contains(markup, want) {
			t.Fatalf("the mark is missing %q", want)
		}
	}
	if strings.Contains(markup, "__") {
		t.Fatal("a slot in mark.svg was left unfilled")
	}
}

// TestDrawingLandsTheModules checks the assembling frames: nothing is there at
// the start, the tiles come before the stem, and the finished drawing is the mark
// with every module at full strength.
func TestDrawingLandsTheModules(t *testing.T) {
	opacity := func(markup, module string) float64 {
		at := strings.Index(markup, `id="`+module+`" opacity="`)
		if at < 0 {
			t.Fatalf("no opacity for %s", module)
		}
		rest := markup[at+len(`id="`+module+`" opacity="`):]
		end := strings.IndexByte(rest, '"')
		v, err := strconv.ParseFloat(rest[:end], 64)
		if err != nil {
			t.Fatalf("unreadable opacity for %s: %v", module, err)
		}
		return v
	}
	start := drawingMarkup(0, "#111111")
	for _, name := range []string{"blue-module", "coral-module", "purple-module", "yellow-module", "pi-stem"} {
		if got := opacity(start, name); got != 0 {
			t.Fatalf("%s is already at %.2f before the mark starts drawing", name, got)
		}
	}
	half := drawingMarkup(0.4, "#111111")
	if opacity(half, "blue-module") == 0 {
		t.Fatal("the first tile has not landed by the middle of the drawing")
	}
	if opacity(half, "pi-stem") != 0 {
		t.Fatal("the stem lands before the tiles it sits on")
	}
	done := drawingMarkup(1, "#111111")
	for _, name := range []string{"blue-module", "coral-module", "purple-module", "yellow-module", "pi-stem"} {
		if got := opacity(done, name); got != 1 {
			t.Fatalf("%s is at %.2f when the mark is done, want 1", name, got)
		}
	}
}

// TestMoodsMoveTheMark checks that each mood animates and that they are told
// apart: the frames are one mark seen moving, not five drawings.
func TestMoodsMoveTheMark(t *testing.T) {
	frames := func(mood Mood) map[string]bool {
		seen := map[string]bool{}
		for f := 0; f < Frames; f++ {
			markup := frameMarkup(mood, f, "#111111")
			if strings.Count(markup, `id="blue-module"`) != 1 {
				t.Fatalf("mood %d frame %d drew the mark twice", mood, f)
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
