package theme

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// GeistTTF is the Geist latin subset the React client renders with
// (@fontsource-variable/geist), converted from woff2 because ui.RegisterFont
// only takes TTF/OTF.
//
//go:embed fonts/Geist.ttf
var GeistTTF []byte

// FontFamily is the name the app asks for in theme.Font.
const FontFamily = "Geist"

// RegisterFonts makes Geist available to the window; call it before the first
// frame, as ui.RegisterFont is not safe to call while frames are building.
func RegisterFonts() error {
	return ui.RegisterFont(GeistTTF, FontFamily)
}
