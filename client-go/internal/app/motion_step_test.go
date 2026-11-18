package app

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/motion"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// stepStage draws one timeline step and nothing else, so the dot is the only
// mark in the frame the test has to find.
func stepStage(t *testing.T, label string, live bool) (*App, *ui.Tester, theme.Tokens) {
	t.Helper()
	a := New(Config{})
	a.previewImage = -1
	a.prefs = prefs{Theme: "light"}
	tok := theme.Light()
	step := turns.Step{Kind: turns.StepTool, Label: label, Start: time.Now().UnixMilli()}
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, tok)
		ui.Column(c).Fill().Background(tok.Background).Padding(24, 24).Children(func() {
			a.stepRow(c, tok, step, live)
		})
	}, 420, 160)
	return a, tt, tok
}

// dotColour finds the step's dot in a frame: it is the leftmost thing drawn in
// the row the label sits on, five DIPs square.
func dotColour(t *testing.T, img *image.RGBA, label ui.Rect) ui.Color {
	t.Helper()
	y := int(label.Y+label.H/2) - 1
	lo := -1
	for x := int(label.X) - 60; x < int(label.X); x++ {
		c := img.RGBAAt(x, y)
		if !nearColor(c.R, theme.Light().Background.R) || !nearColor(c.G, theme.Light().Background.G) ||
			!nearColor(c.B, theme.Light().Background.B) {
			if lo < 0 {
				lo = x
			}
		}
	}
	if lo < 0 {
		t.Fatalf("no dot left of the step label at y=%d", y)
	}
	c := img.RGBAAt(lo+2, y)
	return ui.Color{R: c.R, G: c.G, B: c.B, A: c.A}
}

// TestRunningStepBreathes is the proof that a running step's dot moves between
// two tones of its own colour: half a breath apart the pixel is a different
// shade, and a step that has finished keeps one.
func TestRunningStepBreathes(t *testing.T) {
	_, tt, tok := stepStage(t, "bash", true)
	tt.Frame()
	label, ok := tt.Find("bash")
	if !ok {
		t.Skip("the step row is not on screen")
	}
	start := dotColour(t, tt.Image(), label)
	writePNG(t, motionShot("pitta-motion-step-a.png"), tt.Image())

	// Half of motion.Pulse in, the dot is at the other end of its breath.
	tt.Advance(motion.Pulse / 2)
	tt.Frame()
	half := dotColour(t, tt.Image(), label)
	writePNG(t, motionShot("pitta-motion-step-b.png"), tt.Image())

	if start == half {
		t.Fatalf("the running dot is %v both at the top and the bottom of its breath", start)
	}
	if lum(start) == lum(half) || absInt(lum(start)-lum(half)) < 20 {
		t.Fatalf("the running dot only moved from %v to %v: too little to see", start, half)
	}
	// It has to stay the step's own colour, just fainter, not turn into another
	// one: a dot that goes grey reads as a finished step.
	if start.R < tok.Primary.R || start.B < tok.Primary.B {
		t.Fatalf("the dot is %v, want a tone of the step colour %v", start, tok.Primary)
	}
}

// TestFinishedStepKeepsItsDot checks the breath belongs to the running step
// alone: a finished row is the same on both frames, so a column of them does
// not shimmer.
func TestFinishedStepKeepsItsDot(t *testing.T) {
	_, tt, _ := stepStage(t, "bash", false)
	tt.Frame()
	label, ok := tt.Find("bash")
	if !ok {
		t.Skip("the step row is not on screen")
	}
	first := dotColour(t, tt.Image(), label)
	tt.Advance(motion.Pulse / 2)
	tt.Frame()
	if second := dotColour(t, tt.Image(), label); second != first {
		t.Fatalf("the finished dot went from %v to %v", first, second)
	}
}

func lum(c ui.Color) int {
	return (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
