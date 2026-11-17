package app

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// maxImages is how many pictures one turn carries; the earlier client composer clips at
// three as well.
const maxImages = 3

// imagePart is one attached picture: the bytes the bridge wants, plus the
// decoded bitmap so the thumbnail does not decode again each frame.
type imagePart struct {
	Name   string
	Data   string
	Mime   string
	bitmap *ui.Bitmap
}

// imageStrip draws thumbnails for the composer, each with a remove handle.
func (a *App) imageStrip(c *ui.Context, t theme.Tokens) {
	if len(a.pendingImages) == 0 {
		return
	}
	ui.Row(c).FillWidth().Gap(6).Padding(0, 0).Children(func() {
		for index, image := range a.pendingImages {
			index := index
			ui.Box(c).Width(56).Height(56).Radius(6).Border(1, t.Border).
				Background(t.Muted).OnClick(func() {
				a.apply(func() { a.dropImage(index) })
			}).Children(func() {
				if image.bitmap != nil {
					ui.Image(c, image.bitmap).Fill()
				} else {
					ui.Icon(c, icons.FileIcon).Size(16, 16).TextColor(t.MutedFg)
				}
			})
		}
	})
}

// dropImage removes one pending picture.
func (a *App) dropImage(index int) {
	if index < 0 || index >= len(a.pendingImages) {
		return
	}
	a.pendingImages = append(a.pendingImages[:index], a.pendingImages[index+1:]...)
	a.invalidate()
}

// attachImage reads one picture from disk, either from the system picker or
// from the path field, which is the way in for a build without a window. mygo's
// clipboard is text only, so paste cannot carry a screenshot.
func (a *App) attachImage(path string) {
	path = strings.TrimSpace(strings.Trim(path, `"'`))
	if path == "" {
		return
	}
	if expanded := expandHome(path); expanded != path {
		path = expanded
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.fail("attach image: %v", err)
		return
	}
	mime := mimeOf(path)
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		a.fail("attach image %s: %v", filepath.Base(path), err)
		return
	}
	a.apply(func() {
		if len(a.pendingImages) >= maxImages {
			return
		}
		a.pendingImages = append(a.pendingImages, imagePart{
			Name:   filepath.Base(path),
			Data:   base64.StdEncoding.EncodeToString(data),
			Mime:   mime,
			bitmap: bitmap,
		})
		a.imagePath = ""
		a.imageOpen = false
		a.invalidate()
	})
}

// expandHome turns a leading ~ into the user's home so a path copied from a
// shell works.
func expandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~"))
}

// mimeOf names the type the bridge forwards to the model, by extension.
func mimeOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	case ".bmp":
		return "image/bmp"
	}
	return "image/png"
}

// imageDialog asks for a path, since there is no native file picker.
func (a *App) imageDialog(c *ui.Context, t theme.Tokens) {
	open := a.imageOpen
	ui.Modal(c, &open, func() {
		ui.Text(c, i18n.T("attachImage")).FontSize(13).FontWeight(600).TextColor(t.Foreground)
		ui.Text(c, i18n.T("attachImageHint")).FontSize(10).TextColor(t.MutedFg)
		ui.TextInput(c, &a.imagePath).FillWidth().FontSize(12).
			Placeholder("~/Desktop/shot.png").
			OnSubmit(func() { a.attachImage(a.imagePath) })
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			ui.Row(c).Grow(1)
			ui.Button(c, i18n.T("cancel")).FontSize(11).OnClick(func() {
				a.apply(func() { a.imageOpen = false; a.invalidate() })
			})
			ui.PrimaryButton(c, i18n.T("attach")).FontSize(11).OnClick(func() {
				a.attachImage(a.imagePath)
			})
		})
	})
	if a.imageOpen != open {
		a.apply(func() { a.imageOpen = open; a.invalidate() })
	}
}

// imageCacheLimit bounds how many entries keep their decoded pictures: one
// screenshot is a few MB of pixels once decoded, and a long chat can carry many.
const imageCacheLimit = 6

// historyImages returns the pictures a stored entry carries, decoded on first
// draw and then kept. View runs every frame, and imagesOf decodes a whole PNG
// every time it is called (~15ms for a 1720x1116 screenshot), so calling it
// from the draw path starves the main thread.
func (a *App) historyImages(entry protocol.Entry) []imagePart {
	// Entries without an id (a turn still streaming) are drawn from the live
	// state and change as it does, so they are never cached.
	if entry.ID == 0 {
		return imagesOf(entry)
	}
	if images, ok := a.imageCache[entry.ID]; ok {
		return images
	}
	images := imagesOf(entry)
	if a.imageCache == nil {
		a.imageCache = make(map[int][]imagePart, imageCacheLimit)
	}
	a.imageCache[entry.ID] = images
	a.imageOrder = append(a.imageOrder, entry.ID)
	for len(a.imageOrder) > imageCacheLimit {
		oldest := a.imageOrder[0]
		a.imageOrder = a.imageOrder[1:]
		delete(a.imageCache, oldest)
	}
	return images
}

// imagesOf returns the pictures a user entry carries, for the transcript.
func imagesOf(entry protocol.Entry) []imagePart {
	parts := contentParts(entry)
	if len(parts) == 0 {
		return nil
	}
	var images []imagePart
	for _, part := range parts {
		if stringOf(part["type"]) != "image" {
			continue
		}
		data, mime := stringOf(part["data"]), stringOf(part["mimeType"])
		if data == "" || mime == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			log.Printf("app: decode image part: %v", err)
			continue
		}
		bitmap, err := ui.DecodeBitmap(raw)
		if err != nil {
			log.Printf("app: decode image bitmap: %v", err)
			continue
		}
		images = append(images, imagePart{Data: data, Mime: mime, bitmap: bitmap})
	}
	return images
}

// imagePreviews draws conversation pictures, each opening at full size.
func (a *App) imagePreviews(c *ui.Context, t theme.Tokens, images []imagePart) {
	ui.Row(c).Gap(6).Children(func() {
		for index, image := range images {
			index, images := index, images
			ui.Box(c).Height(72).Radius(6).Border(1, t.Border).
				OnClick(func() {
					a.apply(func() {
						a.previewSet = images
						a.previewImage = index
						a.invalidate()
					})
				}).Children(func() {
				if image.bitmap != nil {
					ui.Image(c, image.bitmap).Height(72)
				}
			})
		}
	})
}

// previewDialog shows one conversation picture full size, with the zoom a
// picture needs when the answer is about a detail: clicking it doubles the
// size, and ± steps in halves.
func (a *App) previewDialog(c *ui.Context, t theme.Tokens) {
	if a.previewImage < 0 || a.previewImage >= len(a.previewSet) {
		return
	}
	tokens := t
	open := true
	ui.Modal(c, &open, func() {
		image := a.previewSet[a.previewImage]
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, image.Name).FontSize(11).TextColor(tokens.MutedFg).SingleLine()
			ui.Row(c).Grow(1)
			ui.Button(c, "−").FontSize(11).Disabled(a.previewScale <= 0.5).OnClick(func() {
				a.zoomPreview(-0.5)
			})
			ui.Text(c, zoomPercent(a.previewScale)).FontSize(10).Width(34).AlignSelf(ui.Center).TextColor(tokens.MutedFg)
			ui.Button(c, "+").FontSize(11).Disabled(a.previewScale >= 4).OnClick(func() {
				a.zoomPreview(0.5)
			})
			ui.Button(c, "1:1").FontSize(11).OnClick(func() {
				a.apply(func() { a.previewScale = 1; a.invalidate() })
			})
		})
		ui.ScrollBoth(c).Grow(1).FillWidth().Children(func() {
			if image.bitmap == nil {
				return
			}
			picture := ui.Image(c, image.bitmap).MaxHeight(640 * a.previewScale).MaxWidth(900 * a.previewScale)
			picture.Scale(a.previewScale, a.previewScale)
			picture.OnClick(func() { a.zoomPreview(1) })
		})
	})
	if !open {
		a.apply(func() {
			a.previewImage, a.previewScale = -1, 1
			a.previewSet = nil
			a.invalidate()
		})
	}
}

// zoomPreview moves the scale by delta, kept inside the range the buttons
// offer so the picture never shrinks into its border or past the panel.
func (a *App) zoomPreview(delta float32) {
	a.apply(func() {
		scale := a.previewScale + delta
		if scale < 0.5 {
			scale = 0.5
		}
		if scale > 4 {
			scale = 4
		}
		a.previewScale = scale
		a.invalidate()
	})
}

// zoomPercent words the scale the way a photo viewer does.
func zoomPercent(scale float32) string {
	return itoa(int(scale*100+0.5)) + "%"
}

// submitImages converts the pending pictures to what the bridge takes, once.
func submitImages(images []imagePart) []api.Image {
	if len(images) == 0 {
		return nil
	}
	out := make([]api.Image, 0, len(images))
	for _, image := range images {
		out = append(out, api.Image{Data: image.Data, MimeType: image.Mime})
	}
	return out
}

// contentParts returns the content blocks of an entry's messages, one level
// down from the wire shape the bridge sends.
func contentParts(entry protocol.Entry) []map[string]any {
	if len(entry.Model) == 0 {
		return nil
	}
	var messages []map[string]any
	if err := json.Unmarshal(entry.Model, &messages); err != nil {
		return nil
	}
	var parts []map[string]any
	for _, message := range messages {
		switch content := message["content"].(type) {
		case []any:
			for _, item := range content {
				if part, ok := item.(map[string]any); ok {
					parts = append(parts, part)
				}
			}
		case map[string]any:
			parts = append(parts, content)
		}
	}
	return parts
}

// stringOf reads a JSON value that should hold a string.
func stringOf(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
