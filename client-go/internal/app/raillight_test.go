package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
)

// litBands counts the rows of the folded rail that carry a background of their own
// — the chip that marks a destination — because two of them say nothing.
func litBands(shot *image_RGBAProxy, railWidth int) int {
	bgR, bgG, bgB, _ := shot.at(2, shot.h/2)
	bg := float64(bgR + bgG + bgB)
	count, open := 0, false
	for y := 0; y < shot.h; y++ {
		lit := 0
		for x := 4; x < railWidth-4; x++ {
			r, g, b, a := shot.at(x, y)
			if a < 128 {
				continue
			}
			// A lit row is a chip a shade off the canvas, lighter or darker; glyphs
			// are far off it and never cover most of a row.
			if d := float64(r+g+b) - bg; d < -2000 && d > -12000 || d > 2000 && d < 12000 {
				lit++
			}
		}
		if lit > (railWidth-8)*3/5 && !open {
			count++
			open = true
		}
		if lit <= (railWidth-8)/4 {
			open = false
		}
	}
	return count
}

type image_RGBAProxy struct {
	w, h int
	pix  func(x, y int) (uint32, uint32, uint32, uint32)
}

func (i *image_RGBAProxy) at(x, y int) (uint32, uint32, uint32, uint32) { return i.pix(x, y) }

// TestFoldedRailLightsOneRow: with the projects drawer open and the rail showing
// extensions, both 项目 and 扩展 used to be lit. A drawer is not a section of the
// rail, so only the section is lit.
func TestFoldedRailLightsOneRow(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.setRail(railExtensions)
	a.projects.open = true
	a.toggleRailCollapsed()
	tt := ui.NewTester(a.View, 900, 620)
	tt.Frame()
	shot := tt.Image()
	b := shot.Bounds()
	img := &image_RGBAProxy{w: b.Dx(), h: b.Dy(), pix: func(x, y int) (uint32, uint32, uint32, uint32) {
		return shot.At(x, y).RGBA()
	}}
	if got := litBands(img, railCollapsedWidth); got != 1 {
		t.Fatalf("folded rail lit %d rows, wanted 1 (the section it shows)", got)
	}
	writePNG(t, "/tmp/clientv2-rail-lit.png", shot)
}
