package theme

import (
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestGeistAndCJK keeps the embedded font honest: Geist must load, and CJK
// must fall back instead of turning into tofu boxes.
func TestGeistAndCJK(t *testing.T) {
	if err := RegisterFonts(); err != nil {
		t.Fatalf("RegisterFonts: %v", err)
	}
	tt := ui.NewTester(func(c *ui.Context) {
		v := Light()
		Apply(c, v)
		ui.Column(c).Fill().Padding(10).Children(func() {
			ui.Text(c, "Pitta Geist 12346").Font(FontFamily).FontSize(20)
			ui.Text(c, "中文测试 请审阅").Font(FontFamily).FontSize(20)
		})
	}, 300, 120)
	tt.Frame()
	img := tt.Image()
	if f, err := os.Create("/tmp/pitta-fonts.png"); err == nil {
		png.Encode(f, img)
		f.Close()
	}
	if !tt.HasText("中文测试 请审阅") {
		t.Fatalf("texts: %q", tt.Texts())
	}
}
