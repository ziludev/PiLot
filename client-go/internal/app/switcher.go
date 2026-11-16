package app

import (
	"encoding/base64"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// switcherHint is the palette's placeholder. It is a constant rather than an
// i18n key because the shared dictionary holds exactly what i18n.ts holds.
const switcherHint = "Jump to a chat…"

// switcher is the Cmd+K jump list: every chat, newest first, narrowed as you
// type. It exists because a native window can bind a shortcut that the React
// client only reached through the rail's search field.
type switcher struct {
	open  bool
	query string
}

// openSwitcher shows the palette with an empty query.
func (a *App) openSwitcher() {
	a.switcher.open = true
	a.switcher.query = ""
	a.invalidate()
}

// switcherMatches is what the palette lists, newest first, query narrowed.
func (a *App) switcherMatches() []protocol.Surface {
	query := strings.ToLower(strings.TrimSpace(a.switcher.query))
	rows := a.sortedSurfaces()
	if query == "" {
		return rows
	}
	matched := make([]protocol.Surface, 0, len(rows))
	for _, row := range rows {
		haystack := strings.ToLower(surfaceTitle(row) + " " + surfaceKind(row) + " " + itoa(row.ConversationID))
		if strings.Contains(haystack, query) {
			matched = append(matched, row)
		}
	}
	return matched
}

// switcherDialog draws the palette while it is open.
func (a *App) switcherDialog(c *ui.Context, t theme.Tokens) {
	if !a.switcher.open {
		return
	}
	rows := a.switcherMatches()
	open := func(row protocol.Surface) {
		a.selectConversation(row.ConversationID)
		a.apply(func() { a.switcher.open = false })
	}
	ui.DialogBase(c, &a.switcher.open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.25))
		panel.Width(460).Padding(10).Gap(6).Radius(12).Material(panelGlass())
		panel.Children(func() {
			ui.TextInput(c, &a.switcher.query).FillWidth().FontSize(12).
				Placeholder(switcherHint).Label(switcherHint).
				OnSubmit(func() {
					if len(rows) > 0 {
						open(rows[0])
					}
				}).
				OnTextKey(func(k ui.TextKey) bool {
					if k.Key != ui.KeyEnter || len(rows) == 0 {
						return false
					}
					open(rows[0])
					return true
				})
			if len(rows) == 0 {
				ui.Text(c, i18n.T("noHits")).FontSize(11).TextColor(t.MutedFg).Padding(6, 4)
				return
			}
			shown := rows
			if len(shown) > 8 {
				shown = shown[:8]
			}
			for _, row := range shown {
				surface := row
				ui.Row(c).FillWidth().Gap(6).Padding(5, 6).Radius(6).AlignItems(ui.Center).
					OnClick(func() { open(surface) }).Children(func() {
					switcherIcon(c, t, surface)
					ui.Text(c, surfaceTitle(surface)).FontSize(12).TextColor(t.Foreground).Grow(1).SingleLine()
					ui.Text(c, "#"+itoa(surface.ConversationID)).FontSize(10).TextColor(t.MutedFg)
				})
			}
		})
	})
}

// switcherIcon is the row's transport icon, matching its rail row.
func switcherIcon(c *ui.Context, t theme.Tokens, s protocol.Surface) {
	switch surfaceKind(s) {
	case "client":
		ui.Icon(c, icons.Monitor).Size(11, 11).TextColor(t.Primary)
	case "feishu":
		ui.Icon(c, icons.MessagesSquare).Size(11, 11).TextColor(t.MutedFg)
	default:
		ui.Icon(c, icons.CornerDownRight).Size(11, 11).TextColor(t.MutedFg)
	}
}

// dropPaths handles files dropped on the window: images become attachments,
// other files open in the viewer. The React client could only offer a path
// field.
func (a *App) dropPaths(paths []string) {
	for _, path := range paths {
		switch strings.ToLower(filepath.Ext(path)) {
		case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp":
			a.attachImage(path)
		default:
			a.openFile(path)
		}
	}
}

// attachClipboardPNG adds an image the clipboard carries, as Cmd+V does.
func (a *App) attachClipboardPNG(data []byte) {
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		a.fail("attach clipboard image: %v", err)
		return
	}
	a.apply(func() {
		if len(a.pendingImages) >= maxImages {
			a.notice = i18n.T("attachImageHint")
			return
		}
		a.pendingImages = append(a.pendingImages, imagePart{
			Name:   "clipboard.png",
			Data:   base64.StdEncoding.EncodeToString(data),
			Mime:   "image/png",
			bitmap: bitmap,
		})
	})
}

// pasteImage turns a clipboard image into an attachment, reporting whether the
// clipboard held one so text paste can be left alone.
func (a *App) pasteImage(c *ui.Context) bool {
	return a.pasteImageFrom(c.Services())
}

// pasteImageFrom is pasteImage without a Context, so the rule can be tested
// headless where the clipboard is reachable through the tester only.
func (a *App) pasteImageFrom(services ui.Services) bool {
	for _, format := range services.ClipboardFormats() {
		if format != transfer.PNG {
			continue
		}
		data, err := services.ReadClipboardData(transfer.PNG).Read(transfer.PNG)
		if err != nil {
			return false
		}
		a.attachClipboardPNG(data)
		return true
	}
	return false
}
