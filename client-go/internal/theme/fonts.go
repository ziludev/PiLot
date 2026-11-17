package theme

import (
	_ "embed"
	"log"
	"os"

	"github.com/egoist/mygo/ui"
)

// GeistTTF is the Geist latin subset the earlier web client renders with
// (@fontsource-variable/geist), converted from woff2 because ui.RegisterFont
// only takes TTF/OTF.
//
//go:embed fonts/Geist.ttf
var GeistTTF []byte

// FontFamily is the family the app asks for in theme.Font. A registered family
// is drawn from every font registered under it, so the system's CJK font joins
// Geist here: without it, a Chinese title in the rail draws as empty boxes,
// because Geist carries no CJK glyphs and mygo does not fall back across
// families.
const FontFamily = "Pitta Sans"

// cjkFonts are tried in order; the first one present on disk joins the family.
// They are read from the system rather than embedded: they are 20 MB and
// licensed for the machine they ship with.
var cjkFonts = []string{
	"/System/Library/Fonts/Hiragino Sans GB.ttc",
	"/System/Library/Fonts/STHeiti Light.ttc",
	"/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
}

// fontReady reports whether the app's family can draw both Latin and CJK.
var fontReady bool

// RegisterFonts makes the app's family available to the window; call it before
// the first frame, as ui.RegisterFont is not safe to call while frames are
// building. It succeeds when Geist loads even if no CJK font is found, and then
// leaves the window on the system font: a family that cannot draw the UI's own
// strings is worse than a different typeface.
func RegisterFonts() error {
	if err := ui.RegisterFont(GeistTTF, FontFamily); err != nil {
		return err
	}
	for _, path := range cjkFonts {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if err := ui.RegisterFont(data, FontFamily); err != nil {
			log.Printf("theme: %s: %v", path, err)
			continue
		}
		fontReady = true
		return nil
	}
	log.Printf("theme: no CJK font found, keeping the system font")
	return nil
}

// FontForWindow is the family to set on a window, empty when text should stay
// on the system font.
func FontForWindow() string {
	if !fontReady {
		return ""
	}
	return FontFamily
}
