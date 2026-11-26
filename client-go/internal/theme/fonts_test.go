package theme

import (
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestGeistAndCJK keeps the font stack honest: the family must draw CJK, not
// the .notdef box a Latin-only family draws for every han character. Two
// different characters are the check — if the family has no CJK glyphs they
// both come out as the same box.
func TestGeistAndCJK(t *testing.T) {
	if err := RegisterFonts(); err != nil {
		t.Fatalf("RegisterFonts: %v", err)
	}
	if FontForWindow() == "" {
		t.Skip("no CJK font on this machine; the window stays on the system font")
	}
	render := func(s string) *image.RGBA {
		tt := ui.NewTester(func(c *ui.Context) {
			Apply(c, Light())
			ui.Text(c, s).Font(FontForWindow()).FontSize(20).TextColor(Light().Foreground)
		}, 60, 40)
		tt.Frame()
		return tt.Image()
	}
	zhi, heng := render("知"), render("衡")
	if f, err := os.Create("/tmp/pilot-fonts.png"); err == nil {
		png.Encode(f, heng)
		f.Close()
	}
	if ink(zhi) < 20 {
		t.Fatalf("知 drew %d ink pixels, so the glyph is missing", ink(zhi))
	}
	if same(zhi, heng) {
		t.Fatal("知 and 衡 drew the same pixels: the family has no CJK glyphs")
	}
}

// ink counts pixels that are not the background.
func ink(img *image.RGBA) int {
	count := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if img.RGBAAt(x, y).R < 200 {
				count++
			}
		}
	}
	return count
}

// same reports whether two renders are identical.
func same(a, b *image.RGBA) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := 0; y < a.Bounds().Dy(); y++ {
		for x := 0; x < a.Bounds().Dx(); x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				return false
			}
		}
	}
	return true
}
