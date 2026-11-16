package theme

import (
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// frame draws a view that applies the theme the way the app does, and returns
// the pixels it painted. Rendering is how the tokens are checked: the point of
// Apply is what ends up on screen, not the values it was handed.
func frame(t *testing.T, dark bool) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		tk := Current(c)
		Apply(c, tk)
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, "Pitta").FontSize(13)
		})
	}, 200, 120)
	tt.SetDark(dark)
	tt.Frame()
	savePNG(t, tt, dark)
	return tt
}

// savePNG writes the frame next to the test output so a human can look at what
// the palette actually produced.
func savePNG(t *testing.T, tt *ui.Tester, dark bool) {
	t.Helper()
	name := "/tmp/pitta-theme-light.png"
	if dark {
		name = "/tmp/pitta-theme-dark.png"
	}
	f, err := os.Create(name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatalf("encode %s: %v", name, err)
	}
}

func TestBackgroundFollowsAppearance(t *testing.T) {
	cases := []struct {
		dark bool
		want ui.Color
	}{
		{false, Light().Background},
		{true, Dark().Background},
	}
	for _, tc := range cases {
		img := frame(t, tc.dark).Image()
		got := img.RGBAAt(1, 1)
		want := tc.want
		if int(got.R) != int(want.R) || int(got.G) != int(want.G) || int(got.B) != int(want.B) {
			t.Fatalf("dark=%v: background pixel = #%02x%02x%02x, want #%02x%02x%02x",
				tc.dark, got.R, got.G, got.B, want.R, want.G, want.B)
		}
	}
}

// TestTextContrastsBackground catches a palette that renders text in the
// background's own color, which a pixel test on the background alone misses.
func TestTextContrastsBackground(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tt := frame(t, dark)
		img := tt.Image()
		bg := img.RGBAAt(1, 1)
		found := false
		for y := 0; y < img.Bounds().Dy(); y++ {
			for x := 0; x < img.Bounds().Dx(); x++ {
				px := img.RGBAAt(x, y)
				if px != bg {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			t.Fatalf("dark=%v: the frame has only the background color; text did not paint", dark)
		}
	}
}

// TestAccentIsPrimary checks that Apply wired the shadcn --primary (ink, not a
// hue) into the widget theme's accent, which is what a primary button uses.
func TestAccentIsPrimary(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tk := Light()
		if dark {
			tk = Dark()
		}
		tt := ui.NewTester(func(c *ui.Context) {
			v := Current(c)
			Apply(c, v)
			ui.Column(c).Fill().Padding(20).Children(func() {
				ui.PrimaryButton(c, "Send")
			})
		}, 240, 120)
		tt.SetDark(dark)
		tt.Frame()
		rect, ok := tt.Find("Send")
		if !ok {
			t.Fatalf("dark=%v: the button did not paint", dark)
		}
		img := tt.Image()
		// The button pads around its label, so sample left of the text run.
		got := img.RGBAAt(int(rect.X-4), int(rect.Y+rect.H/2))
		if got.R != uint8(tk.Primary.R) || got.G != uint8(tk.Primary.G) || got.B != uint8(tk.Primary.B) {
			t.Fatalf("dark=%v: button pixel = #%02x%02x%02x, want primary #%02x%02x%02x",
				dark, got.R, got.G, got.B, tk.Primary.R, tk.Primary.G, tk.Primary.B)
		}
	}
}

// TestBorderComesFromTokens checks the divider's 1px line, which ui draws with
// the theme border color, so Apply's border wiring shows up in pixels.
func TestBorderComesFromTokens(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tk := Light()
		if dark {
			tk = Dark()
		}
		tt := ui.NewTester(func(c *ui.Context) {
			v := Current(c)
			Apply(c, v)
			ui.Row(c).Fill().Children(func() {
				ui.Divider(c)
				ui.Text(c, "after")
			})
		}, 120, 120)
		tt.SetDark(dark)
		tt.Frame()
		got := tt.Image().RGBAAt(0, 60)
		// Translucent borders (dark mode) composite over the background.
		want := composite(tk.Border, tk.Background)
		if !near(got.R, want.R) || !near(got.G, want.G) || !near(got.B, want.B) {
			t.Fatalf("dark=%v: divider pixel = #%02x%02x%02x, want border #%02x%02x%02x over background",
				dark, got.R, got.G, got.B, want.R, want.G, want.B)
		}
	}
}

// composite blends a with alpha over an opaque background.
func composite(a, bg ui.Color) ui.Color {
	mix := func(f, b uint8) uint8 {
		alpha := uint32(a.A)
		return uint8((uint32(f)*alpha + uint32(b)*(255-alpha)) / 255)
	}
	return ui.RGBA(mix(a.R, bg.R), mix(a.G, bg.G), mix(a.B, bg.B), 255)
}

// near tolerates the 1-2 value rounding of a blend done in the renderer.
func near(a, b uint8) bool {
	d := int(a) - int(b)
	return d >= -2 && d <= 2
}
