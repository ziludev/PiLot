package app

import (
	"image"
	"image/color"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/motion"
	"pitta/internal/theme"
)

// motionShot is where the motion frames go: /tmp, never the repo. These are
// frames to look at while reviewing an animation, not references to keep.
func motionShot(name string) string { return "/tmp/" + name }

// toastStage drives the client's toast layer on its own over a pane colour, so a
// snapshot shows the pill and nothing else. It adds the toast once the viewport
// has already been drawn, which is what an app that finally copies something
// does, and closes it when the frames want the exit.
type toastStage struct {
	label   string
	added   bool
	closing bool
	id      string
	app     *App
	tester  *ui.Tester
}

func newToastStage(t *testing.T, w, h int) *toastStage {
	t.Helper()
	a := New(Config{})
	a.previewImage = -1
	a.prefs = prefs{Theme: "light"}
	s := &toastStage{label: "Copied", app: a}
	tok := theme.Light()
	s.tester = ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, tok)
		ui.Column(c).Fill().Background(tok.Background).Children(func() {
			ui.Text(c, "under the glass").FontSize(13).TextColor(tok.Foreground).Padding(24, 24)
		})
		if s.added && !s.closing && s.id == "" {
			s.id = c.AddToast(ui.Toast{Title: s.label, Timeout: motion.ToastLife})
		}
		if s.closing && s.id != "" {
			c.CloseToast(s.id)
		}
		a.toasts(c)
	}, w, h)
	return s
}

// frame draws one frame, optionally after the stage's own changes.
func (s *toastStage) frame() *image.RGBA {
	s.tester.Frame()
	return s.tester.Image()
}

// pill reports where the toast's pixels sit in the band the viewport draws in:
// how many there are, and where the leftmost and rightmost are. The pane colour
// is the only thing behind them, so anything else is the pill.
func pill(t *testing.T, img *image.RGBA, bg ui.Color) (count, lo, hi int) {
	t.Helper()
	bounds := img.Bounds()
	lo, hi = -1, -1
	// The viewport sits 24 DIPs off the bottom; the pill is ~40 DIPs tall.
	for y := bounds.Max.Y - 80; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
			if nearColor(got.R, bg.R) && nearColor(got.G, bg.G) && nearColor(got.B, bg.B) {
				continue
			}
			count++
			if lo < 0 || x < lo {
				lo = x
			}
			if x > hi {
				hi = x
			}
		}
	}
	return count, lo, hi
}

// TestToastSlidesInFromTheEdge is the snapshot proof that a toast comes in from
// the right and leaves the same way: mid-frame it is still right of where it
// settles, and after the exit no copy of it is left behind. The PNGs are the
// frames themselves, checked by eye for a doubled pill or a white block.
func TestToastSlidesInFromTheEdge(t *testing.T) {
	const w, h = 520, 260
	s := newToastStage(t, w, h)
	bg := theme.Light().Background

	// A frame with no toast: the viewport is drawn, so the toast that arrives
	// next has a parent older than itself and its enter plays.
	s.frame()
	s.added = true
	in0 := s.frame()
	writePNG(t, motionShot("pitta-motion-toast-in-0.png"), in0)
	if n, _, _ := pill(t, in0, bg); n > 40 {
		t.Fatalf("the toast shows %d pixels on the frame it appears, want it faded out", n)
	}

	s.tester.Advance(120 * time.Millisecond)
	inMid := s.frame()
	writePNG(t, motionShot("pitta-motion-toast-in-mid.png"), inMid)
	midCount, midLo, _ := pill(t, inMid, bg)
	if midCount == 0 {
		t.Fatal("the toast is invisible 120 ms into its enter")
	}

	s.tester.Advance(300 * time.Millisecond)
	end := s.frame()
	writePNG(t, motionShot("pitta-motion-toast-end.png"), end)
	endCount, endLo, _ := pill(t, end, bg)
	if endCount == 0 {
		t.Fatal("the toast is invisible after its enter")
	}
	if max := w/2 + 120; endLo > max {
		t.Fatalf("the settled toast starts at x=%d, want it in the middle of %d", endLo, w)
	}
	if midLo <= endLo+40 {
		t.Fatalf("the toast starts at x=%d mid-enter and x=%d settled: it did not slide in from the right",
			midLo, endLo)
	}

	// Out: mid-exit it is still there and heading right, and once the exit is
	// over no ghost is left over the pane.
	s.closing = true
	exit0 := s.frame()
	writePNG(t, motionShot("pitta-motion-toast-out-0.png"), exit0)
	s.tester.Advance(140 * time.Millisecond)
	outMid := s.frame()
	writePNG(t, motionShot("pitta-motion-toast-out-mid.png"), outMid)
	outCount, outLo, _ := pill(t, outMid, bg)
	if outCount == 0 {
		t.Fatal("the toast vanished instead of sliding out")
	}
	if outLo <= endLo+20 {
		t.Fatalf("the toast starts at x=%d mid-exit and x=%d settled: it did not slide out to the right",
			outLo, endLo)
	}

	s.tester.Advance(400 * time.Millisecond)
	gone := s.frame()
	writePNG(t, motionShot("pitta-motion-toast-gone.png"), gone)
	if n, _, _ := pill(t, gone, bg); n > 40 {
		t.Fatalf("%d pixels left after the exit: the ghost stayed", n)
	}
}
