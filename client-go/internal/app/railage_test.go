package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
)

// railBand is one horizontal run of painted rows in the folded rail, with the
// horizontal centre of what was painted.
type railBand struct {
	top, bottom int
	centre      float64
	colourful   bool
}

// railBands reads the folded rail out of a frame: a run of rows that painted
// anything at all inside the rail's column.
func railBands(img image_RGBA, railWidth int) []railBand {
	// The rail's canvas, taken from the strip between the logo and the icons, so
	// the test reads a light window and a dark one the same way: a glyph is what
	// differs from it.
	bgR, bgG, bgB, _ := img.at(2, img.h/2)
	bg := int(bgR + bgG + bgB)
	var bands []railBand
	open := -1
	var sumX, count float64
	colourful := false
	flush := func(bottom int) {
		if open >= 0 && count > 0 {
			bands = append(bands, railBand{top: open, bottom: bottom, centre: sumX / count, colourful: colourful})
		}
		open, sumX, count, colourful = -1, 0, 0, false
	}
	for y := 0; y < img.h; y++ {
		painted := false
		for x := 0; x < railWidth && x < img.w; x++ {
			r, g, b, a := img.at(x, y)
			if a < 128 {
				continue
			}
			// The rail's own canvas is near 23,24,26; anything well above it is a
			// glyph, the avatar, or a chip.
			if d := int(r+g+b) - bg; d > 60 || d < -60 {
				painted = true
				sumX += float64(x)
				count++
				hi, lo := max3(r, g, b), min3(r, g, b)
				if hi-lo > 40 {
					colourful = true
				}
			}
		}
		if painted && open < 0 {
			open = y
		}
		if !painted && open >= 0 {
			if y-open > 4 {
				flush(y)
			} else {
				open, sumX, count, colourful = -1, 0, 0, false
			}
		}
	}
	flush(img.h)
	return bands
}

type image_RGBA struct {
	w, h int
	pix  func(x, y int) (uint32, uint32, uint32, uint32)
}

func (i image_RGBA) at(x, y int) (uint32, uint32, uint32, uint32) { return i.pix(x, y) }

func max3(a, b, c uint32) uint32 {
	if b > a {
		a = b
	}
	if c > a {
		a = c
	}
	return a
}

func min3(a, b, c uint32) uint32 {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// TestFoldedRailPutsTheLogoOnTheIconsAxis: folded, the rail is one column of
// icons, so the workspace logo sits on that axis too. It used to sit six points
// to the left of it.
func TestFoldedRailPutsTheLogoOnTheIconsAxis(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.toggleRailCollapsed()
	tt := ui.NewTester(a.View, 900, 620)
	tt.Frame()
	defer writePNG(t, "/tmp/clientv2-rail-folded.png", tt.Image())

	shot := tt.Image()
	b := shot.Bounds()
	img := image_RGBA{w: b.Dx(), h: b.Dy(), pix: func(x, y int) (uint32, uint32, uint32, uint32) {
		return shot.At(x, y).RGBA()
	}}
	bands := railBands(img, railCollapsedWidth)
	if len(bands) < 3 {
		t.Fatalf("folded rail painted %d bands, wanted the logo and the nav icons", len(bands))
	}
	// The traffic lights are the colourful band at the very top; the logo is the
	// next coloured one, and the icons follow it as grey bands.
	var logo, icons railBand
	seen := 0
	for _, band := range bands {
		if band.top < 40 {
			continue
		}
		if band.colourful && seen == 0 {
			logo, seen = band, 1
			continue
		}
		if seen == 1 {
			icons = band
			break
		}
	}
	if logo.bottom == 0 || icons.bottom == 0 {
		t.Fatalf("no logo band above the icons: %+v", bands)
	}
	if diff := logo.centre - icons.centre; diff > 1.5 || diff < -1.5 {
		t.Fatalf("logo is %.1f points off the icons' axis; logo %+v icons %+v",
			diff, logo, icons)
	}
}
