package diff

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The test palette is opaque and loud so a rendered background is easy to
// count; Light and Dark are for the eye, not for assertions.
func testColors() Colors {
	return Colors{
		Text:     ui.Hex("#000000"),
		Number:   ui.Hex("#000000"),
		AddFg:    ui.Hex("#000000"),
		AddBg:    ui.RGB(0, 200, 0),
		DelFg:    ui.Hex("#000000"),
		DelBg:    ui.RGB(200, 0, 0),
		HunkFg:   ui.Hex("#000000"),
		HunkBg:   ui.RGB(0, 0, 200),
		MetaFg:   ui.Hex("#000000"),
		HeaderFg: ui.Hex("#000000"),
		Border:   ui.Hex("#808080"),
	}
}

func TestRender(t *testing.T) {
	files := Parse(twoHunkPatch)
	var v View
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Padding(8).Children(func() {
			v.Render(c, files, Options{Colors: testColors()})
		})
	}, 360, 260)

	img := tt.Image()
	added, removed, hunk := countColor(img, testColors().AddBg), countColor(img, testColors().DelBg), countColor(img, testColors().HunkBg)
	if added == 0 {
		t.Error("no added line background")
	}
	if removed == 0 {
		t.Error("no removed line background")
	}
	if hunk == 0 {
		t.Error("no hunk header background")
	}
	if !textsContain(tt.Texts(), "@@ -5,3 +5,3 @@") {
		t.Errorf("hunk header not drawn, texts %q", tt.Texts())
	}
	if !textsContain(tt.Texts(), "f") {
		t.Errorf("path not drawn, texts %q", tt.Texts())
	}

	// A click on the header folds the file's body away.
	tt.Move(100, 18)
	tt.ClickAt(100, 18)
	tt.Frame()
	if got := countColor(tt.Image(), testColors().AddBg); got != 0 {
		t.Errorf("folded file still draws %d added background pixels", got)
	}
	if !textsContain(tt.Texts(), "f") {
		t.Error("folded file lost its header")
	}
	if !textsContain(tt.Texts(), "\u25b8") {
		t.Errorf("folded header points nowhere, texts %q", tt.Texts())
	}
}

func TestRenderFragment(t *testing.T) {
	// A hunk with no file header has nothing to fold and no card around it.
	files := Parse("@@ -1,2 +1,2 @@\n ctx\n-a\n+b\n")
	if files[0].Path != "" {
		t.Fatalf("path = %q, want empty", files[0].Path)
	}
	var v View
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Padding(8).Children(func() {
			v.Render(c, files, Options{Colors: testColors()})
		})
	}, 240, 120)
	if countColor(tt.Image(), testColors().AddBg) == 0 {
		t.Error("fragment drew no added line background")
	}
}

func TestRenderScrollsLongLines(t *testing.T) {
	long := strings.Repeat("x", 200)
	files := Parse("--- a/f\n+++ b/f\n@@ -1,1 +1,1 @@\n-" + long + "\n+" + long + "y\n")
	var v View
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).FillWidth().Children(func() {
			v.Render(c, files, Options{Colors: testColors()})
		})
	}, 240, 120)

	// Move first: a hover on a row repaints it, and this measures the wheel.
	tt.Move(120, 40)
	tt.Advance(300 * time.Millisecond)
	before := tt.Image()
	tt.Wheel(120, 40, 120, 0)
	tt.Advance(300 * time.Millisecond)
	if changedPixels(before, tt.Image()) == 0 {
		t.Error("a line wider than the card did not scroll")
	}
}

func countColor(img *image.RGBA, want ui.Color) int {
	n := 0
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			c := img.RGBAAt(x, y)
			if abs(int(c.R)-int(want.R)) <= 8 && abs(int(c.G)-int(want.G)) <= 8 && abs(int(c.B)-int(want.B)) <= 8 {
				n++
			}
		}
	}
	return n
}

func changedPixels(a, b *image.RGBA) int {
	n := 0
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				n++
			}
		}
	}
	return n
}

func textsContain(texts []string, want string) bool {
	for _, s := range texts {
		if strings.Contains(s, want) {
			return true
		}
	}
	return false
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
