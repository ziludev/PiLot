package app

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"

	"pitta/internal/theme"
)

// TestRailSearchIsCentred pins the search field's placeholder to the middle of
// its own box: forcing a height smaller than the text needs leaves the glyphs
// sitting low, which is what it looked like.
func TestRailSearchIsCentred(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	tokens := theme.Light()
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, tokens)
		ui.Column(c).Width(240).Padding(8).Children(func() {
			a.railSearch(c, tokens)
		})
	}, 260, 90)
	tt.Frame()
	img := tt.Image()

	boxTop, boxBottom := -1, -1
	for y := 0; y < img.Bounds().Dy(); y++ {
		if isBorder(img, 120, y, tokens.Border) {
			if boxTop < 0 {
				boxTop = y
			}
			boxBottom = y
		}
	}
	if boxTop < 0 || boxBottom-boxTop < 16 {
		t.Fatalf("search box not found: top=%d bottom=%d", boxTop, boxBottom)
	}
	textTop, textBottom := -1, -1
	for y := boxTop + 1; y < boxBottom; y++ {
		if hasDarkPixel(img, 20, 100, y, tokens.Foreground) {
			if textTop < 0 {
				textTop = y
			}
			textBottom = y
		}
	}
	if textTop < 0 {
		t.Fatal("placeholder is not drawn")
	}
	textMid := float32(textTop+textBottom) / 2
	boxMid := float32(boxTop+boxBottom) / 2
	if diff := textMid - boxMid; diff > 1.5 || diff < -1.5 {
		t.Fatalf("placeholder sits %.1fpx off centre (text %d..%d, box %d..%d)",
			diff, textTop, textBottom, boxTop, boxBottom)
	}
}

// isBorder reports whether the pixel at x,y is the border color.
func isBorder(img *image.RGBA, x, y int, border ui.Color) bool {
	if x >= img.Bounds().Dx() || y >= img.Bounds().Dy() {
		return false
	}
	pixel := img.RGBAAt(x, y)
	return nearColor(pixel.R, border.R) && nearColor(pixel.G, border.G) && nearColor(pixel.B, border.B)
}

// hasDarkPixel reports whether a row holds glyph pixels between x0 and x1.
func hasDarkPixel(img *image.RGBA, x0, x1, y int, ink ui.Color) bool {
	// Anything darker than the field's fill is text, placeholder included: the
	// placeholder is muted, not the foreground, so compare against the fill.
	for x := x0; x < x1 && x < img.Bounds().Dx(); x++ {
		pixel := img.RGBAAt(x, y)
		if pixel.R < 200 && pixel.G < 200 && pixel.B < 200 {
			return true
		}
	}
	return false
}

// TestRailHeaderClearsTheTrafficLights checks the layout the hidden title bar
// asks for: with controls taking the top-left corner, the rail's first row
// starts to their right, so the avatar is never drawn under them.
func TestRailHeaderClearsTheTrafficLights(t *testing.T) {
	a := snapshotData(t)
	a.prefs.RailWidth = 260
	// The name is the row's first text after the avatar, so it moves with the
	// inset: what it must not do is move by more than the inset, which would
	// mean the padding was added to both sides and the column squeezed.
	base := ui.NewTester(a.View, 1180, 820)
	base.Frame()
	name, ok := base.Find(i18n.T("appName"))
	if !ok {
		t.Fatalf("app name not drawn")
	}
	for _, bar := range []ui.TitleBar{{}, {Height: 28, Left: 78}} {
		tt := ui.NewTester(a.View, 1180, 820)
		tt.SetTitleBar(bar)
		tt.Frame()
		rect, ok := tt.Find(i18n.T("appName"))
		if !ok {
			t.Fatalf("app name not drawn with title bar %+v", bar)
		}
		if want := bar.Left + 12; rect.X < want {
			t.Fatalf("app name at x=%.1f, under the controls (want >= %.1f)", rect.X, want)
		}
		if want := bar.Bottom + 8; rect.Y < want {
			t.Fatalf("app name at y=%.1f, under the controls (want >= %.1f)", rect.Y, want)
		}
		// The inset pads the left and the top only; the name keeps the column
		// the row's Grow gives it either way.
		if got, want := rect.X-name.X, bar.Left; got != want {
			t.Fatalf("app name moved %.1f px right with title bar %+v, want %.1f", got, bar, want)
		}
	}
}
