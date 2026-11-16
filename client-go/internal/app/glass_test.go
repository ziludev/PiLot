package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/theme"
)

// TestGlassSamplesWhatIsUnderIt is the point of the material: a pane over red
// content shows red through its frost instead of an opaque surface colour. It
// also pins the CPU renderer, which the tester uses, to the same look as the
// GPU's.
func TestGlassSamplesWhatIsUnderIt(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Dark())
		ui.Box(c).Fill().Background(ui.RGB(220, 40, 40))
		ui.Column(c).Absolute().Left(40).Top(40).Width(200).Height(120).Radius(12).
			Material(panelGlass())
	}, 320, 240)
	tt.Frame()
	img := tt.Image()

	inside := func(x, y int) (r, g, b float64) {
		cr, cg, cb, _ := img.At(x, y).RGBA()
		return float64(cr >> 8), float64(cg >> 8), float64(cb >> 8)
	}
	// Under the pane, away from its rim: red must still lead.
	r, g, b := inside(140, 100)
	if !(r > g+8 && r > b+8) {
		t.Fatalf("the pane at (140,100) is %v/%v/%v: not the red under it, so the glass drew opaque", r, g, b)
	}
	// Outside every pane the red is untouched; the frost lightens what it
	// covers, as glass.Regular does in dark mode.
	or, og, ob := inside(10, 230)
	if or+og+ob >= r+g+b {
		t.Fatalf("outside %v/%v/%v vs inside %v/%v/%v: the glass did not frost what it covers", or, og, ob, r, g, b)
	}
	if og != 40 || ob != 40 {
		t.Fatalf("outside is %v/%v/%v, want the untouched red 220/40/40", or, og, ob)
	}
	// The rim lights up more than the middle, as Liquid Glass does. It is a
	// hairline, so the brightest row above the middle is what counts.
	middle := r + g + b
	brightest := middle
	for y := 40; y < 60; y++ {
		yr, yg, yb := inside(140, y)
		if sum := yr + yg + yb; sum > brightest {
			brightest = sum
		}
	}
	if brightest <= middle {
		t.Fatalf("the rim is not lighter than the middle: %v vs %v", brightest, middle)
	}
}
