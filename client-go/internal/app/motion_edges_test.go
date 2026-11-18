package app

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/motion"
	"pitta/internal/theme"
)

// This file holds the boundary and failure cases of the motion work the app
// added on top of the framework: toasts that are stacked, toasts with no
// deadline and no clock, a bar under a pointer, the end of the tab marker's
// first frame, and a panel that must not replay its enter on a plain repaint.
// The happy paths live next to each feature in motion_toast_test.go,
// motion_step_test.go and motion_tabs_test.go.

// rows is a horizontal band of a frame: the rows it covers, and how much ink it
// holds.
type rows struct {
	top, bottom, ink int
	lo, hi           int
}

// pillBands splits the ink in the band the toast viewport draws in into
// horizontal bands, one per pill. The pane colour is the only thing behind the
// toasts, so any pixel that is not near it belongs to one.
func pillBands(t *testing.T, img *image.RGBA, bg ui.Color) []rows {
	t.Helper()
	bounds := img.Bounds()
	var out []rows
	cur := rows{top: -1, lo: -1}
	for y := bounds.Max.Y - 120; y < bounds.Max.Y; y++ {
		n, lo, hi := 0, -1, -1
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			got := img.RGBAAt(x, y)
			if nearColor(got.R, bg.R) && nearColor(got.G, bg.G) && nearColor(got.B, bg.B) {
				continue
			}
			n++
			if lo < 0 {
				lo = x
			}
			hi = x
		}
		if n == 0 {
			// A toast draws as a pill with its hairline a few rows below the
			// rounded bottom edge, so one pill can come back as two bands:
			// only a real gap ends one.
			if cur.top >= 0 && y-cur.bottom > 6 {
				out = append(out, cur)
				cur = rows{top: -1, lo: -1}
			}
			continue
		}
		if cur.top < 0 {
			cur = rows{top: y, lo: lo, hi: hi}
		}
		cur.bottom = y
		cur.ink += n
		if lo < cur.lo {
			cur.lo = lo
		}
		if hi > cur.hi {
			cur.hi = hi
		}
	}
	if cur.top >= 0 {
		out = append(out, cur)
	}
	return out
}

// TestToastPairStacksWithoutGhosts covers two toasts on screen at once: the
// second one arrives while the first is still settling, and closing the first
// must leave the second whole. A ghost or a doubled pill from the enter/exit
// transitions would show up here as a third band or as ink above the survivor.
func mid(a, b int) float32 { return float32(a+b) / 2 }

func TestToastPairStacksWithoutGhosts(t *testing.T) {
	const w, h = 520, 300
	tok := theme.Light()
	a := New(Config{})
	a.previewImage = -1
	a.prefs = prefs{Theme: "light"}

	var firstAdded, secondAdded, closeFirst bool
	var first, second string
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, tok)
		ui.Column(c).Fill().Background(tok.Background).Children(func() {
			ui.Text(c, "under the glass").FontSize(13).TextColor(tok.Foreground).Padding(24, 24)
		})
		if firstAdded && first == "" {
			first = c.AddToast(ui.Toast{Title: "Copied", Timeout: motion.ToastLife})
		}
		if secondAdded && second == "" {
			second = c.AddToast(ui.Toast{Title: "That failed", Timeout: motion.ToastLife})
		}
		if closeFirst && first != "" {
			c.CloseToast(first)
		}
		a.toasts(c)
	}, w, h)

	tt.Frame() // the viewport is older than the first toast
	firstAdded = true
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()

	// The second toast arrives while the first is on screen.
	secondAdded = true
	tt.Frame()
	pairBorn := tt.Image()
	if got := len(pillBands(t, pairBorn, tok.Background)); got > 2 {
		t.Fatalf("two toasts made %d bands on the frame the second one appears", got)
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	pair := tt.Image()
	writePNG(t, motionShot("pitta-motion-toast-pair.png"), pair)

	bands := pillBands(t, pair, tok.Background)
	if len(bands) != 2 {
		t.Fatalf("two toasts drew %d bands, want 2: %+v", len(bands), bands)
	}
	for _, b := range bands {
		if b.bottom-b.top < 16 {
			t.Fatalf("a toast is only %d rows tall: %+v", b.bottom-b.top, b)
		}
	}
	if bands[1].top-bands[0].bottom < 2 {
		t.Fatalf("the two toasts overlap: %+v", bands)
	}
	if c0, c1 := mid(bands[0].lo, bands[0].hi), mid(bands[1].lo, bands[1].hi); c0-c1 > 8 || c1-c0 > 8 {
		t.Fatalf("the toasts are not stacked on one centre line: %.0f and %.0f", c0, c1)
	}

	// Closing the first one must not take the second with it, and must not
	// leave a copy of the first behind.
	closeFirst = true
	tt.Frame()
	tt.Advance(500 * time.Millisecond)
	tt.Frame()
	after := tt.Image()
	writePNG(t, motionShot("pitta-motion-toast-pair-after.png"), after)
	left := pillBands(t, after, tok.Background)
	if len(left) != 1 {
		t.Fatalf("after one of two toasts closed %d bands are left, want 1: %+v", len(left), left)
	}
	if above := left[0].top - bands[1].top; above > 12 || above < -12 {
		t.Fatalf("the survivor moved from y=%d to y=%d", bands[1].top, left[0].top)
	}
}

// TestPanelContentKeepsStillOnRepaint guards the place the content enter is
// mounted: it plays when the panel's column is born, not on every repaint. A
// plain frame after the animation is over must not nudge the content again.
func TestPanelContentKeepsStillOnRepaint(t *testing.T) {
	a := snapshotData(t)
	a.sideOpen = true
	a.sideTab = SideChanges
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	tt.Frame()
	mid := tt.Image()
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	settled := tt.Image()
	tabs, ok := tt.Find("Changes")
	if !ok {
		t.Skipf("the changes tab is not on screen; texts: %v", tt.Texts())
	}
	// The panel's column only: right of the tabs, so the transcript and the
	// composer behind it are not part of the comparison.
	x0 := int(tabs.X) + int(tabs.W) + 8
	writePNG(t, motionShot("pitta-motion-panel-repaint.png"), settled)

	// A positive control: the same comparison has to see the content move while
	// the enter is running, or "no difference" below would mean nothing.
	if firstDifferingColumn(t, mid, settled, x0) < 0 {
		t.Fatal("the mid-enter frame looks settled already: this test cannot tell a replay from a still panel")
	}

	tt.Advance(16 * time.Millisecond)
	tt.Frame()
	if nudge := firstDifferingColumn(t, settled, tt.Image(), x0); nudge >= 0 {
		t.Fatalf("one frame after settling the panel's content differs at x=%d: the enter replayed", nudge)
	}
	tt.Advance(400 * time.Millisecond)
	tt.Frame()
	if nudge := firstDifferingColumn(t, settled, tt.Image(), x0); nudge >= 0 {
		t.Fatalf("the panel's content differs at x=%d on a later repaint", nudge)
	}
}

// firstDifferingColumn reports the leftmost column at or right of x0 where two
// frames differ, or -1 when they agree. Comparing pixels column by column is
// what tells a still panel from one that shifted: counting ink alone would not.
func firstDifferingColumn(t *testing.T, a, b *image.RGBA, x0 int) int {
	t.Helper()
	if a.Bounds() != b.Bounds() {
		t.Fatalf("frames differ in size: %v and %v", a.Bounds(), b.Bounds())
	}
	bounds := a.Bounds()
	for x := x0; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			ca, cb := a.RGBAAt(x, y), b.RGBAAt(x, y)
			if ca != cb {
				return x
			}
		}
	}
	return -1
}

// TestToastArrivesSettledWithReduceMotion covers the desktop asking for less
// motion: a toast must not slide at all, it is simply there, centered, on the
// first frame it is drawn. With motion on, that same frame has it ~70px right
// of centre, so the two are easy to tell apart.
func TestToastArrivesSettledWithReduceMotion(t *testing.T) {
	const w, h = 520, 260
	tok := theme.Light()
	a := New(Config{})
	a.previewImage = -1
	a.prefs = prefs{Theme: "light"}

	var added bool
	var id string
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, tok)
		ui.Column(c).Fill().Background(tok.Background).Children(func() {
			ui.Text(c, "under the glass").FontSize(13).TextColor(tok.Foreground).Padding(24, 24)
		})
		if added && id == "" {
			id = c.AddToast(ui.Toast{Title: "Copied", Timeout: motion.ToastLife})
		}
		a.toasts(c)
	}, w, h)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})

	tt.Frame()
	added = true
	tt.Frame()
	tt.Advance(16 * time.Millisecond)
	tt.Frame() // the frame the toast is drawn in
	born := tt.Image()
	writePNG(t, motionShot("pitta-motion-toast-reduced.png"), born)

	bands := pillBands(t, born, tok.Background)
	if len(bands) != 1 {
		t.Fatalf("with less motion the toast drew %d bands, want 1: %+v", len(bands), bands)
	}
	if centre := mid(bands[0].lo, bands[0].hi); centre < w/2-12 || centre > w/2+12 {
		t.Fatalf("with less motion the toast first appears centred at %.0f, want %.0f: it slid in", centre, float32(w)/2)
	}
}

// TestRunningStepHoldsStillWithReduceMotion is the same promise for the running
// step's dot. It does not hold today: motion.Loop keeps frames coming and keeps
// turning whatever the desktop asks for (myo v0.9.1, ui/widgets.go:71), so the
// dot breathes with less motion on and the app never idles while a turn runs.
// Whether a live indicator may keep moving is a product call, so the check is
// parked here, ready to be turned on with the guard.
func TestRunningStepHoldsStillWithReduceMotion(t *testing.T) {
	_, tt, _ := stepStage(t, "bash", true)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true, TextScale: 1})
	tt.Frame()
	label, ok := tt.Find("bash")
	if !ok {
		t.Skip("the step row is not on screen")
	}
	first := dotColour(t, tt.Image(), label)
	tt.Advance(motion.Pulse / 2)
	tt.Frame()
	second := dotColour(t, tt.Image(), label)
	writePNG(t, motionShot("pitta-motion-step-reduced.png"), tt.Image())
	if first != second {
		t.Skipf("the running dot still breathes with less motion on: %v then %v. "+
			"Guard the Loop with the preference, then drop this skip.", first, second)
	}
}

// TestCopyingTwiceKeepsOneToast is the path the app really takes: every copy
// raises a toast under the fixed id "copied", so a fast second copy has to land
// on the toast already there instead of stacking a twin or leaving a ghost when
// the first one's life ends.
func TestCopyingTwiceKeepsOneToast(t *testing.T) {
	const w, h = 520, 260
	tok := theme.Light()
	a := New(Config{})
	a.previewImage = -1
	a.prefs = prefs{Theme: "light"}

	copies := 0
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, tok)
		ui.Column(c).Fill().Background(tok.Background).Children(func() {
			ui.Text(c, "under the glass").FontSize(13).TextColor(tok.Foreground).Padding(24, 24)
		})
		for ; copies < 2; copies++ {
			c.AddToast(ui.Toast{ID: "copied", Title: "Copied", Timeout: motion.ToastLife})
		}
		a.toasts(c)
	}, w, h)

	tt.Frame()
	for i := 0; i < 4; i++ {
		tt.Advance(100 * time.Millisecond)
		tt.Frame()
		if bands := pillBands(t, tt.Image(), tok.Background); len(bands) != 1 {
			t.Fatalf("after two copies %d toasts are on screen, want 1: %+v", len(bands), bands)
		}
	}
	writePNG(t, motionShot("pitta-motion-toast-copied-twice.png"), tt.Image())

	// One life later both are gone, and neither leaves a copy of itself behind.
	tt.Advance(2 * time.Second)
	tt.Frame()
	writePNG(t, motionShot("pitta-motion-toast-copied-gone.png"), tt.Image())
	if bands := pillBands(t, tt.Image(), tok.Background); len(bands) != 0 {
		t.Fatalf("the toast outlived its life: %+v", bands)
	}
}
