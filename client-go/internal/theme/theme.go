// Package theme maps the earlier web client's zinc token set
// (the old client/index.css) onto what mygo's ui package can color.
package theme

import "github.com/egoist/mygo/ui"

// Tokens holds the client's palette. Widget colors go into ui.Theme;
// the chrome colors (sidebar, toolbar, status bar) have no theme slot and
// stay here, in the shell's own palette.
type Tokens struct {
	Background ui.Color
	Foreground ui.Color
	Card       ui.Color
	Popover    ui.Color
	PopoverFg  ui.Color

	Primary   ui.Color
	PrimaryFg ui.Color
	Secondary ui.Color
	SecondFg  ui.Color

	Muted    ui.Color
	MutedFg  ui.Color
	Accent   ui.Color
	AccentFg ui.Color

	// Warning is amber-500, the color the approval bar and running tasks use;
	// that token set has no equivalent.
	Warning ui.Color
	// Selection is the highlight behind selected text, which mygo draws
	// itself; a browser paints it, so these are mygo's own
	// defaults, kept explicit so a theme change cannot make it invisible.
	Selection   ui.Color
	Destructive ui.Color
	Border      ui.Color
	Input       ui.Color
	Ring        ui.Color

	Sidebar       ui.Color
	SidebarFg     ui.Color
	SidebarAccent ui.Color
	SidebarBorder ui.Color
}

// Light is :root from the old client/index.css, oklch converted to sRGB.
func Light() Tokens {
	return Tokens{
		Background: ui.Hex("#ffffff"),
		Foreground: ui.Hex("#0a0a0a"),
		Card:       ui.Hex("#ffffff"),
		Popover:    ui.Hex("#ffffff"),
		PopoverFg:  ui.Hex("#0a0a0a"),

		Primary:   ui.Hex("#171717"),
		PrimaryFg: ui.Hex("#fafafa"),
		Secondary: ui.Hex("#f5f5f5"),
		SecondFg:  ui.Hex("#171717"),

		Muted:    ui.Hex("#f5f5f5"),
		MutedFg:  ui.Hex("#737373"),
		Accent:   ui.Hex("#f5f5f5"),
		AccentFg: ui.Hex("#171717"),

		Warning:     ui.Hex("#f59e0b"),
		Selection:   ui.RGBA(37, 99, 235, 0.25),
		Destructive: ui.Hex("#e7000b"),
		Border:      ui.Hex("#e5e5e5"),
		Input:       ui.Hex("#e5e5e5"),
		Ring:        ui.Hex("#a1a1a1"),

		Sidebar:       ui.Hex("#fafafa"),
		SidebarFg:     ui.Hex("#0a0a0a"),
		SidebarAccent: ui.Hex("#f5f5f5"),
		SidebarBorder: ui.Hex("#e5e5e5"),
	}
}

// Dark is .dark from the old client/index.css. The borders are white at 10% and
// 15% there, so they stay translucent here.
func Dark() Tokens {
	return Tokens{
		Background: ui.Hex("#0a0a0a"),
		Foreground: ui.Hex("#fafafa"),
		Card:       ui.Hex("#171717"),
		Popover:    ui.Hex("#171717"),
		PopoverFg:  ui.Hex("#fafafa"),

		Primary:   ui.Hex("#e5e5e5"),
		PrimaryFg: ui.Hex("#171717"),
		Secondary: ui.Hex("#262626"),
		SecondFg:  ui.Hex("#fafafa"),

		Muted:    ui.Hex("#262626"),
		MutedFg:  ui.Hex("#a1a1a1"),
		Accent:   ui.Hex("#262626"),
		AccentFg: ui.Hex("#fafafa"),

		Warning:     ui.Hex("#fbbf24"),
		Selection:   ui.RGBA(59, 130, 246, 0.40),
		Destructive: ui.Hex("#ff6467"),
		Border:      ui.RGBA(255, 255, 255, 0.10),
		Input:       ui.RGBA(255, 255, 255, 0.15),
		Ring:        ui.Hex("#737373"),

		Sidebar:       ui.Hex("#171717"),
		SidebarFg:     ui.Hex("#fafafa"),
		SidebarAccent: ui.Hex("#262626"),
		SidebarBorder: ui.RGBA(255, 255, 255, 0.10),
	}
}

// Current reads the appearance from the frame's base theme, which follows the
// system. It must be called every frame: caching it freezes the app in the
// mode it launched in.
func Current(c *ui.Context) Tokens {
	if c.Theme().Dark {
		return Dark()
	}
	return Light()
}

// Radius is --radius: 0.625rem from the client's stylesheet, in DIPs.
const Radius float32 = 10

// Apply sets the widget theme for this frame from t.
func Apply(c *ui.Context, t Tokens) {
	base := *c.Theme()
	base.Background = t.Background
	base.Surface = t.Muted
	base.SurfaceHover = t.Accent
	base.SurfacePressed = t.Border
	base.Border = t.Border
	base.Text = t.Foreground
	base.TextMuted = t.MutedFg
	// --primary there is ink, not a hue: buttons read as black on light.
	base.Accent = t.Primary
	base.AccentHover = t.Primary
	base.AccentPressed = t.Primary
	base.AccentText = t.PrimaryFg
	base.Danger = t.Destructive
	base.Selection = t.Selection
	base.Focus = t.Ring
	base.Radius = Radius
	if family := FontForWindow(); family != "" {
		base.Font = family
	}
	c.SetTheme(&base)
}

// Hover is a color nudged ten percent toward its own text color, which is the
// the --primary/90 hover mix.
func Hover(base, text ui.Color) ui.Color {
	return Composite(ui.RGBA(text.R, text.G, text.B, 26), base)
}

// Composite blends a over an opaque bg, which is what a translucent highlight
// looks like on screen and what a pixel test can compare against.
func Composite(a, bg ui.Color) ui.Color {
	mix := func(f, b uint8) uint8 {
		alpha := uint32(a.A)
		return uint8((uint32(f)*alpha + uint32(b)*(255-alpha)) / 255)
	}
	return ui.RGBA(mix(a.R, bg.R), mix(a.G, bg.G), mix(a.B, bg.B), 255)
}
