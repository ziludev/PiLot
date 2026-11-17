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
	// The language button is the row's last child, so its right edge is where
	// the row's own right padding shows: it must not move when the title bar
	// appears, or the inset has eaten the room the name column needs.
	next := "EN"
	if i18n.Current() != i18n.ZH {
		next = "中"
	}
	base := ui.NewTester(a.View, 1180, 820)
	base.Frame()
	lang, ok := base.Find(next)
	if !ok {
		t.Fatalf("language label %q not drawn", next)
	}
	wantRight := lang.X + lang.W
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
		// Padding's two-value form adds the inset to both sides, which leaves
		// the name column an ellipsis wide; the four-value form pads the left.
		if lang, ok := tt.Find(next); ok {
			if got := lang.X + lang.W; got != wantRight {
				t.Fatalf("language label ends at x=%.1f with title bar %+v, want %.1f: the inset is padding the right too", got, bar, wantRight)
			}
		}
	}
}
