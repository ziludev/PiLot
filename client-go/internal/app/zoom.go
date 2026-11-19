package app

// Looking at a picture.
//
// A message can carry screenshots, and a 180-point thumbnail is not why they
// were sent: the numbers are too small to read and the diagram is too small to
// follow. The click opens the same bytes at the size the window allows, so the
// answer stays here instead of going through Finder.

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// zoomEdge is the longest side a picture is decoded to for the overlay: enough
// for a retina screenshot on a laptop screen, not so much that a 6K capture
// costs a gigabyte of pixels to look at.
const zoomEdge = 1600

type zoomState struct {
	open    bool
	bitmap  *ui.Bitmap
	caption string
	failed  bool
}

// zoomImage opens one attachment. The decode happens off the frame thread:
// a tall screenshot is a few megabytes, and a click should not cost a paint.
func (a *App) zoomImage(img imageRef) {
	data := img.data
	size := len(data)
	a.zoom = zoomState{open: true, caption: img.caption(size)}
	a.invalidate()
	go func() {
		bitmap, err := ui.DecodeBitmapScaled(data, zoomEdge)
		if err != nil {
			a.post(event{kind: evZoom, err: err})
			return
		}
		a.post(event{kind: evZoom, zoom: bitmap})
	}()
}

// applyZoom fills the overlay, unless it was closed while the picture decoded.
func (a *App) applyZoom(bitmap *ui.Bitmap, err error) {
	if !a.zoom.open {
		return
	}
	if err != nil {
		a.zoom.failed = true
		a.invalidate()
		return
	}
	a.zoom.bitmap = bitmap
	a.invalidate()
}

// zoomSize is the picture at its own ratio, shrunk to fit the overlay's box and
// never blown up past the pixels it has.
func (a *App) zoomSize() (float32, float32) {
	if a.zoom.bitmap == nil {
		return 280, 120
	}
	w, h := a.zoom.bitmap.Size()
	bw, bh := float32(w), float32(h)
	scale := math.Min(920/float64(bw), 640/float64(bh))
	if scale > 1 {
		scale = 1
	}
	return float32(math.Round(float64(bw) * scale)), float32(math.Round(float64(bh) * scale))
}

// zoomDialog draws the picture over everything else while it is up.
func (a *App) zoomDialog(c *ui.Context, t theme.Tokens) {
	if !a.zoom.open {
		return
	}
	ui.Modal(c, &a.zoom.open, func() {
		w, h := a.zoomSize()
		ui.Column(c).Width(w+20).Padding(10).Gap(8).Radius(10).
			Background(t.Popover).Border(1, t.Border).Children(func() {
			switch {
			case a.zoom.failed:
				ui.Text(c, i18n.T("imageBroken")).FontSize(11).TextColor(t.MutedFg)
			case a.zoom.bitmap == nil:
				ui.Row(c).Width(w).Height(h).Justify(ui.Center).AlignItems(ui.Center).
					Children(func() {
						ui.Text(c, i18n.T("loading")).FontSize(11).TextColor(t.MutedFg)
					})
			default:
				ui.Image(c, a.zoom.bitmap).Size(w, h).Radius(6).Clip()
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.zoom.caption).FontSize(9.5).TextColor(t.MutedFg).Grow(1)
				a.dialogButton(c, t, i18n.T("close"), true, func() { a.zoom.open = false })
			})
		})
	})
}

// humanBytes is the size a caption shows: one decimal below ten, because "1.2
// MB" and "12 MB" are the same length to read.
func humanBytes(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// caption is what the thumbnail said about the file, for the line under it.
func (img imageRef) caption(size int) string {
	name := img.mime
	if name == "" {
		name = "image"
	}
	return name + " · " + humanBytes(size)
}
