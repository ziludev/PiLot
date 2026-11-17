package app

import (
	"image"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/brand"
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

// railHeaderRects draws the window at a rail width and returns where the mark
// and the app name landed, so the header's geometry can be compared across
// title bar insets instead of against a hardcoded edge.
func railHeaderRects(t *testing.T, windowWidth int, railWidth float32, bar ui.TitleBar) (ui.Rect, ui.Rect) {
	t.Helper()
	a := snapshotData(t)
	a.prefs.RailWidth = railWidth
	a.railWidth = railWidth
	tt := ui.NewTester(a.View, windowWidth, 400)
	tt.SetTitleBar(bar)
	tt.Frame()
	mark, ok := tt.Find(brand.MarkLabel)
	if !ok {
		t.Fatalf("the mark is not drawn with title bar %+v", bar)
	}
	name, ok := tt.Find(i18n.T("appName"))
	if !ok {
		t.Fatalf("app name not drawn with title bar %+v", bar)
	}
	return mark, name
}

// TestRailHeaderClearsTheTrafficLights checks the layout the hidden title bar
// asks for on a rail wide enough to hold both: the row starts to the right of
// the controls, and only by their width, because padding added to both sides
// squeezes the name column to an ellipsis.
func TestRailHeaderClearsTheTrafficLights(t *testing.T) {
	baseMark, baseName := railHeaderRects(t, 1180, 460, ui.TitleBar{})
	for _, bar := range []ui.TitleBar{{Height: 28, Left: 78}} {
		mark, name := railHeaderRects(t, 1180, 460, bar)
		if want := bar.Bottom + 8; name.Y < want {
			t.Fatalf("app name at y=%.1f, under the controls (want >= %.1f)", name.Y, want)
		}
		if got, want := mark.X-baseMark.X, bar.Left; got != want {
			t.Fatalf("mark moved %.1f px right with title bar %+v, want %.1f", got, bar, want)
		}
		if got, want := name.X-baseName.X, mark.X-baseMark.X; got != want {
			t.Fatalf("name moved %.1f px, mark moved %.1f px: the column was squeezed", got, want)
		}
		// The mark and the name sit a gap apart: the animation box must not be a
		// row child of its own, or the gap doubles to the mark's own width.
		if got, want := name.X-(mark.X+mark.W), float32(8); got != want {
			t.Fatalf("gap between the mark and the name is %.1f px, want %.1f", got, want)
		}
	}
}

// TestRailHeaderStacksUnderNarrowControls pins the narrow rail: there is no
// room for the controls and the name side by side, so the name goes under them
// at the rail's own edge instead of floating in the middle of a sidebar that
// was told to look like a corner.
func TestRailHeaderStacksUnderNarrowControls(t *testing.T) {
	bar := ui.TitleBar{Height: 42, Left: 74, Bottom: 28}
	baseMark, _ := railHeaderRects(t, 560, 252, ui.TitleBar{})
	mark, name := railHeaderRects(t, 560, 252, bar)
	if mark.X != baseMark.X {
		t.Fatalf("mark at x=%.1f, want it at the rail's edge x=%.1f", mark.X, baseMark.X)
	}
	// The rail's 12 px padding plus its 1 px border: anything further right is
	// a second box in the row, the animation's own, taking the mark's width.
	if mark.X > 14 {
		t.Fatalf("mark at x=%.1f, want it at the rail's padding (<= 14)", mark.X)
	}
	if name.Y < bar.Bottom {
		t.Fatalf("app name at y=%.1f, under the controls (want >= %.1f)", name.Y, bar.Bottom)
	}
	if name.X != mark.X+mark.W+8 {
		t.Fatalf("app name at x=%.1f, want it beside the mark at %.1f", name.X, mark.X+mark.W+8)
	}
}
