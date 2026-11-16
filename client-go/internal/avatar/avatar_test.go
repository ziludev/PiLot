package avatar

import (
	"bytes"
	"image"
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// draw renders one face in a frame of its own size and returns the pixels.
func draw(t *testing.T, size int, mood Mood, tone Tone) *image.RGBA {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) { View(c, size, mood, tone) }, size, size)
	tt.Frame()
	return tt.Image()
}

// ink is what the face painted: how many pixels differ from the frame's top
// left one, which the round body never covers, and the box they lie in.
func ink(img *image.RGBA) (n int, box image.Rectangle) {
	for y := 0; y < img.Rect.Dy(); y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			i := y*img.Stride + x*4
			if bytes.Equal(img.Pix[i:i+4], img.Pix[0:4]) {
				continue
			}
			n++
			box = box.Union(image.Rect(x, y, x+1, y+1))
		}
	}
	return n, box
}

// diff counts the pixels two frames of one size disagree on.
func diff(a, b *image.RGBA) int {
	n := 0
	for y := 0; y < a.Rect.Dy(); y++ {
		for x := 0; x < a.Rect.Dx(); x++ {
			i := y*a.Stride + x*4
			if !bytes.Equal(a.Pix[i:i+4], b.Pix[i:i+4]) {
				n++
			}
		}
	}
	return n
}

// TestViewDrawsAtBothSizes is the rail's 26 and a larger 40: the face fits its
// box and actually paints something in both.
func TestViewDrawsAtBothSizes(t *testing.T) {
	for _, size := range []int{26, 40} {
		for _, mood := range []Mood{MoodIdle, MoodThinking, MoodHappy} {
			img := draw(t, size, mood, Tones.Idle)
			if got := img.Bounds().Size(); got.X != size || got.Y != size {
				t.Fatalf("size %d mood %d: frame %v, want %dx%d", size, mood, got, size, size)
			}
			// The body fills most of the box: a few pixels at the corners are all
			// the empty space there is.
			if n, _ := ink(img); n < size*size/3 {
				t.Fatalf("size %d mood %d: %d of %d pixels painted, want the body", size, mood, n, size*size)
			}
		}
	}
}

// TestViewIsDeterministic guards the cache and the drawing: the same face
// twice must be the same pixels, or every frame would flicker.
func TestViewIsDeterministic(t *testing.T) {
	for _, size := range []int{26, 40} {
		for _, mood := range []Mood{MoodIdle, MoodThinking, MoodHappy} {
			for _, tone := range []Tone{Tones.Idle, Tones.Delegated, Tones.Done} {
				first, second := draw(t, size, mood, tone), draw(t, size, mood, tone)
				if !bytes.Equal(first.Pix, second.Pix) {
					t.Fatalf("size %d mood %d tone %v: %d pixels changed between two renders",
						size, mood, tone, diff(first, second))
				}
			}
		}
	}
}

// TestViewScalesWithSize keeps size a scale of the definition's box, not a
// fixed drawing: the same body at 40 is the 26 one times 40/26, give or take
// the antialiasing of an edge pixel.
func TestViewScalesWithSize(t *testing.T) {
	_, small := ink(draw(t, 26, MoodIdle, Tones.Idle))
	_, large := ink(draw(t, 40, MoodIdle, Tones.Idle))
	for _, s := range []struct {
		name       string
		at26, at40 int
	}{{"width", small.Dx(), large.Dx()}, {"height", small.Dy(), large.Dy()}} {
		want := float64(s.at26) * 40 / 26
		if math.Abs(float64(s.at40)-want) > 3 {
			t.Errorf("%s: %d at 40, want %.1f as 26's %d scaled up", s.name, s.at40, want, s.at26)
		}
	}
}

// TestMoodsDiffer keeps the eyes carrying the mood: a still face that looks the
// same in every mood would say nothing about the work.
func TestMoodsDiffer(t *testing.T) {
	for _, size := range []int{26, 40} {
		idle := draw(t, size, MoodIdle, Tones.Idle)
		thinking := draw(t, size, MoodThinking, Tones.Idle)
		happy := draw(t, size, MoodHappy, Tones.Idle)
		if n := diff(idle, thinking); n == 0 {
			t.Errorf("size %d: thinking looks exactly like idle", size)
		}
		if n := diff(idle, happy); n == 0 {
			t.Errorf("size %d: happy looks exactly like idle", size)
		}
		if n := diff(thinking, happy); n == 0 {
			t.Errorf("size %d: thinking looks exactly like happy", size)
		}
	}
}

// TestToneColorsTheBody checks the tone reaches the pixels and does not come
// from the theme: each tone's own colour covers some of the body.
func TestToneColorsTheBody(t *testing.T) {
	for _, tc := range []struct {
		name string
		tone Tone
	}{
		{"idle", Tones.Idle},
		{"delegated", Tones.Delegated},
		{"done", Tones.Done},
	} {
		img := draw(t, 40, MoodIdle, tc.tone)
		want := [4]byte{tc.tone.Color.R, tc.tone.Color.G, tc.tone.Color.B, 255}
		found := 0
		for y := 0; y < img.Rect.Dy(); y++ {
			for x := 0; x < img.Rect.Dx(); x++ {
				i := y*img.Stride + x*4
				if got := [4]byte{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}; got == want {
					found++
				}
			}
		}
		if found == 0 {
			t.Errorf("%s tone: no pixel of the body is %s", tc.name, hex(tc.tone.Color))
		}
	}
}
