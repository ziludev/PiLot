package app

import (
	_ "embed"

	"github.com/egoist/mygo/ui"
)

// avatarPNG is the fennec the window answers as.
//
//go:embed avatar.png
var avatarPNG []byte

// avatar is decoded once and kept: every frame draws it, and decoding a png per
// frame would show up in the scroll.
var avatar *ui.Bitmap

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
