package app

import (
	_ "embed"
	"os"
	"strings"

	"github.com/egoist/mygo/ui"

	"pilot/internal/theme"
)

// avatarPNG is the fennec the window answers as.
//
//go:embed avatar.png
var avatarPNG []byte

// avatar is decoded once and kept: every frame draws it, and decoding a png per
// frame would show up in the scroll.
var avatar *ui.Bitmap

// avatarSize is the box both faces are drawn in: big enough to read as a face,
// small enough not to compete with the first line of the answer.
const avatarSize = 26

// avatarView draws the bot's face at the given size, square with soft corners so
// it reads as an icon rather than a photo.
func (a *App) avatarView(c *ui.Context, size float32) {
	if avatar == nil {
		bitmap, err := ui.DecodeBitmapScaled(avatarPNG, int(size)*2)
		if err != nil {
			return
		}
		avatar = bitmap
	}
	ui.Image(c, avatar).Size(size, size).Radius(size / 4).Clip()
}

// userAvatarView draws the reader's initial. macOS keeps no account picture
// this process may read, and an initial is honest where a logo would be
// decoration.
func (a *App) userAvatarView(c *ui.Context, t theme.Tokens, size float32) {
	ui.Box(c).Size(size, size).Radius(size / 4).Background(t.Secondary).
		Justify(ui.Center).AlignItems(ui.Center).Children(func() {
		ui.Text(c, userInitial()).FontSize(size * 0.5).FontWeight(600).TextColor(t.SecondFg)
	})
}

// userInitial is the first letter of the account this window runs as.
func userInitial() string {
	name := os.Getenv("USER")
	if name == "" {
		return "?"
	}
	return strings.ToUpper(name[:1])
}
