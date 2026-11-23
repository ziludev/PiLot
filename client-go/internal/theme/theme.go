// Package theme holds the client's palette: the Beautiful UI token set
// (beautifului.dev), not the shadcn zinc set the first web client used.
//
// The shape is five layers from outermost to innermost (Page → Canvas →
// Surface → Inset/Field), three ink steps, two hover washes, and one saturated
// hue (Brand) plus three tinted pairs for ok / warn / bad. The oklch values
// from the site's stylesheet are converted to sRGB here, because mygo colors
// are 8-bit RGBA; docs/ui-beautifului.md records the conversion and the
// mapping onto the old slot names, which the painters still ask for.
package theme

import "github.com/egoist/mygo/ui"

// Tokens holds the client's palette. Widget colors go into ui.Theme;
// the chrome colors (sidebar, toolbar, status bar) have no theme slot and
// stay here, in the shell's own palette.
//
// The first block is Beautiful UI's own vocabulary; the second block keeps the
// old slot names alive so the painters did not all have to change at once.
type Tokens struct {
	// Layers, outermost first. A chat window is Page, the rail and the drawer
	// are Canvas, a bubble or a card is Surface, a well inside one is Inset.
	Page    ui.Color
	Canvas  ui.Color
	Surface ui.Color
	Inset   ui.Color
	Field   ui.Color

	// Hover washes: Hover on a Surface, Hover2 on a Canvas (the rail's rows).
	Hover  ui.Color
	Hover2 ui.Color

	// Three ink steps: Ink for what you read, Ink2 for labels and secondary
	// rows, Ink3 for metadata that must recede without disappearing.
	Ink  ui.Color
	Ink2 ui.Color
	Ink3 ui.Color

	// Hairlines. Line is the default border, LineStrong the edge of a control
	// that has to read as an edge, LineSoft a divider inside a card.
	Line       ui.Color
	LineStrong ui.Color
	LineSoft   ui.Color

	// Brand is --accent: the only saturated hue in the chrome. It marks the
	// selected row, the focus ring, and the one primary action per surface.
	Brand     ui.Color
	BrandInk  ui.Color
	BrandTint ui.Color

	// Tinted pairs: the color for text or a dot, the tint for its background.
	Green      ui.Color
	GreenTint  ui.Color
	Orange     ui.Color
	OrangeTint ui.Color
	Red        ui.Color
	RedTint    ui.Color

	// Tooltip is its own inverse surface, so a hint never has to borrow a
	// layer and land unreadable on it.
	TipBg     ui.Color
	TipFg     ui.Color
	TipMuted  ui.Color
	TipBorder ui.Color

	// Legacy slots, kept so every painter compiles unchanged. Each is filled
	// with the Beautiful UI layer that means the same thing.
	Background ui.Color // Page
	Foreground ui.Color // Ink
	Card       ui.Color // Surface
	Popover    ui.Color // Surface
	PopoverFg  ui.Color // Ink

	Primary   ui.Color // Ink: a primary button is ink, not a hue
	PrimaryFg ui.Color // Page
	Secondary ui.Color // Hover
	SecondFg  ui.Color // Ink

	Muted    ui.Color // Inset
	MutedFg  ui.Color // Ink3
	Accent   ui.Color // Hover
	AccentFg ui.Color // Ink

	Warning     ui.Color // Orange
	Success     ui.Color // Green
	Selection   ui.Color // BrandTint behind selected text
	Destructive ui.Color // Red
	Border      ui.Color // Line
	Input       ui.Color // LineStrong
	Ring        ui.Color // Brand

	Sidebar       ui.Color // Canvas
	SidebarFg     ui.Color // Ink
	SidebarAccent ui.Color // Hover2
	SidebarBorder ui.Color // Line
}

// Light is the site's :root block, oklch converted to sRGB.
func Light() Tokens {
	return Tokens{
		Page:    ui.Hex("#fafafb"),
		Canvas:  ui.Hex("#f1f2f3"),
		Surface: ui.Hex("#ffffff"),
		Inset:   ui.Hex("#f7f8f9"),
		Field:   ui.Hex("#f2f2f3"),

		Hover:  ui.Hex("#f4f5f6"),
		Hover2: ui.Hex("#e7e9eb"),

		Ink:  ui.Hex("#1f2124"),
		Ink2: ui.Hex("#62656b"),
		Ink3: ui.Hex("#9a9da3"),

		Line:       ui.Hex("#ecedef"),
		LineStrong: ui.Hex("#e0e2e5"),
		LineSoft:   ui.Hex("#f3f4f5"),

		Brand:     ui.Hex("#0285ff"),
		BrandInk:  ui.Hex("#0070dd"),
		BrandTint: ui.Hex("#e9f3ff"),

		Green:      ui.Hex("#199a4d"),
		GreenTint:  ui.Hex("#e8f5ed"),
		Orange:     ui.Hex("#ef720d"),
		OrangeTint: ui.Hex("#fdf1e5"),
		Red:        ui.Hex("#e3474c"),
		RedTint:    ui.Hex("#fcecec"),

		TipBg:     ui.Hex("#25272b"),
		TipFg:     ui.Hex("#f6f7f8"),
		TipMuted:  ui.Hex("#a5a8ad"),
		TipBorder: ui.Hex("#3a3c40"),

		Background: ui.Hex("#fafafb"),
		Foreground: ui.Hex("#1f2124"),
		Card:       ui.Hex("#ffffff"),
		Popover:    ui.Hex("#ffffff"),
		PopoverFg:  ui.Hex("#1f2124"),

		Primary:   ui.Hex("#1f2124"),
		PrimaryFg: ui.Hex("#fafafb"),
		Secondary: ui.Hex("#f4f5f6"),
		SecondFg:  ui.Hex("#1f2124"),

		Muted:    ui.Hex("#f7f8f9"),
		MutedFg:  ui.Hex("#9a9da3"),
		Accent:   ui.Hex("#f4f5f6"),
		AccentFg: ui.Hex("#1f2124"),

		Warning:     ui.Hex("#ef720d"),
		Success:     ui.Hex("#199a4d"),
		Selection:   ui.RGBA(2, 133, 255, 0.18),
		Destructive: ui.Hex("#e3474c"),
		Border:      ui.Hex("#ecedef"),
		Input:       ui.Hex("#e0e2e5"),
		Ring:        ui.Hex("#0285ff"),

		Sidebar:       ui.Hex("#f1f2f3"),
		SidebarFg:     ui.Hex("#1f2124"),
		SidebarAccent: ui.Hex("#e7e9eb"),
		SidebarBorder: ui.Hex("#ecedef"),
	}
}

// Dark is the site's .dark block. Its lines are opaque greys, not white at ten
// percent, so the chrome stays crisp over the darker layers.
func Dark() Tokens {
	return Tokens{
		Page:    ui.Hex("#17181a"),
		Canvas:  ui.Hex("#1c1d1f"),
		Surface: ui.Hex("#232427"),
		Inset:   ui.Hex("#1f2022"),
		Field:   ui.Hex("#2b2c2f"),

		Hover:  ui.Hex("#2a2b2e"),
		Hover2: ui.Hex("#313236"),

		Ink:  ui.Hex("#f2f3f4"),
		Ink2: ui.Hex("#a5a8ad"),
		Ink3: ui.Hex("#6c6f75"),

		Line:       ui.Hex("#2e3033"),
		LineStrong: ui.Hex("#3a3c40"),
		LineSoft:   ui.Hex("#27282b"),

		Brand:     ui.Hex("#3d9aff"),
		BrandInk:  ui.Hex("#7ec0ff"),
		BrandTint: ui.RGBA(61, 154, 255, 0.16),

		Green:      ui.Hex("#3cbb72"),
		GreenTint:  ui.RGBA(60, 187, 114, 0.14),
		Orange:     ui.Hex("#f68f3c"),
		OrangeTint: ui.RGBA(246, 143, 60, 0.14),
		Red:        ui.Hex("#ee5c61"),
		RedTint:    ui.RGBA(238, 92, 97, 0.14),

		TipBg:     ui.Hex("#111214"),
		TipFg:     ui.Hex("#f2f3f4"),
		TipMuted:  ui.Hex("#a5a8ad"),
		TipBorder: ui.Hex("#2e3033"),

		Background: ui.Hex("#17181a"),
		Foreground: ui.Hex("#f2f3f4"),
		Card:       ui.Hex("#232427"),
		Popover:    ui.Hex("#232427"),
		PopoverFg:  ui.Hex("#f2f3f4"),

		Primary:   ui.Hex("#f2f3f4"),
		PrimaryFg: ui.Hex("#17181a"),
		Secondary: ui.Hex("#2a2b2e"),
		SecondFg:  ui.Hex("#f2f3f4"),

		Muted:    ui.Hex("#1f2022"),
		MutedFg:  ui.Hex("#6c6f75"),
		Accent:   ui.Hex("#2a2b2e"),
		AccentFg: ui.Hex("#f2f3f4"),

		Warning:     ui.Hex("#f68f3c"),
		Success:     ui.Hex("#3cbb72"),
		Selection:   ui.RGBA(61, 154, 255, 0.35),
		Destructive: ui.Hex("#ee5c61"),
		Border:      ui.Hex("#2e3033"),
		Input:       ui.Hex("#3a3c40"),
		Ring:        ui.Hex("#3d9aff"),

		Sidebar:       ui.Hex("#1c1d1f"),
		SidebarFg:     ui.Hex("#f2f3f4"),
		SidebarAccent: ui.Hex("#2a2b2e"),
		SidebarBorder: ui.Hex("#2e3033"),
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

// Radii, in DIPs, from the site's --radius-* variables.
const (
	// RadiusChip is --radius-chip: a tag or a small badge.
	RadiusChip float32 = 6
	// RadiusControl is --radius-control: a row, a search field, an icon button.
	RadiusControl float32 = 8
	// Radius is --radius-card: a bubble, a card, the composer.
	Radius float32 = 10
	// RadiusWindow is --radius-window: an outer frame.
	RadiusWindow float32 = 14
)

// Type sizes, in DIPs, from the sizes the site actually draws with (it uses
// arbitrary values rather than the default scale).
const (
	// TextTitle is the workspace name in the rail.
	TextTitle float32 = 14
	// TextBody is an answer in the transcript.
	TextBody float32 = 13
	// TextRow is a rail row or a tool chip header.
	TextRow float32 = 12.5
	// TextSmall is a step inside a chip.
	TextSmall float32 = 11.5
	// TextMicro is metadata: timestamps, counters, key hints.
	TextMicro float32 = 11
)

// Use installs the palette for this frame and flips the light/dark flag that
// Current reads. Apply alone only recolours widgets, so a painter that asks
// Current for tokens still got light ones — which is how a dark snapshot ends
// up with white cards.
func Use(c *ui.Context, dark bool) Tokens {
	t := Light()
	if dark {
		t = Dark()
	}
	base := *c.Theme()
	base.Dark = dark
	c.SetTheme(&base)
	Apply(c, t)
	return t
}

// Apply sets the widget theme for this frame from t.
func Apply(c *ui.Context, t Tokens) {
	base := *c.Theme()
	base.Background = t.Page
	base.Surface = t.Surface
	base.SurfaceHover = t.Hover
	base.SurfacePressed = t.Hover2
	base.Border = t.Line
	base.Text = t.Ink
	base.TextMuted = t.Ink3
	// A primary button is ink, not a hue: the site spends its one saturated
	// hue on selection and focus, and black-on-white reads as the action.
	base.Accent = t.Ink
	// A scroll bar that reads as a hint of movement, not as a black rule down
	// the side of a panel: thin, rounded, and quiet until hovered.
	base.ScrollbarWidth = 8
	base.Scrollbar = t.Ink3.Alpha(0.45)
	base.ScrollbarStyle = ui.ScrollbarStyle{
		ThumbWidth: 6,
		Thumb:      t.Ink3.Alpha(0.35),
		Hover:      t.Ink2.Alpha(0.55),
		Active:     t.Ink2.Alpha(0.75),
	}
	base.AccentHover = t.Ink2
	base.AccentPressed = t.Ink2
	base.AccentText = t.Page
	base.Danger = t.Red
	base.Selection = t.Selection
	base.Focus = t.Brand
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
