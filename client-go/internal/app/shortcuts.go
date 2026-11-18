package app

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/theme"
)

// A native client can offer a键盘 cheat sheet; the earlier web client had none, so
// these labels are literals rather than dictionary entries — the Go dictionary
// must stay identical to i18n.ts.

type shortcut struct{ Keys, Action string }

func shortcuts() []shortcut {
	cmd := "⌘"
	if modKey() != "⌘" {
		cmd = modKey()
	}
	return []shortcut{
		{cmd + "K", "Jump to a chat"},
		{cmd + "N", "New chat"},
		{cmd + "⇧N", "Open this chat in a window"},
		{cmd + "F", "Find in this chat"},
		{cmd + "B", "Show or hide the side panel"},
		{"⌥↑ ⌥↓", "Previous / next chat"},
		{cmd + "⇧P", "Pin or unpin this chat"},
		{cmd + "⇧⌫", "Archive this chat"},
		{"↑ ↓", "Recall a sent message"},
		{cmd + ",", "Settings"},
		{cmd + "Q", "Quit"},
		{cmd + "W", "Close this window"},
		{cmd + "1…" + cmd + "9", "Jump to one of the first nine chats"},
		{cmd + "↑ " + cmd + "↓", "Page the transcript"},
		{"↵", "New line (⌘↵ sends)"},
		{"⌘⇧Space", "Bring the window back, from any app"},
		{cmd + "/", "This list"},
	}
}

// shortcutsDialog lists the keys the window answers to.
func (a *App) shortcutsDialog(c *ui.Context, t theme.Tokens) {
	open := a.shortcutsOpen
	ui.DialogBase(c, &open, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.2)).OnClick(func() {
			a.apply(func() { a.shortcutsOpen = false; a.invalidate() })
		})
		panel.Column().Width(360).Padding(14).Gap(8).Radius(12).Material(panelGlass())
		ui.Text(c, "Keyboard shortcuts").FontSize(13).FontWeight(600).TextColor(t.Foreground)
		for _, s := range shortcuts() {
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, s.Keys).FontSize(11).Font("monospace").Width(92).TextColor(t.Foreground)
				ui.Text(c, s.Action).FontSize(11).TextColor(t.MutedFg).Grow(1)
			})
		}
	})
	if a.shortcutsOpen != open {
		a.apply(func() { a.shortcutsOpen = open; a.invalidate() })
	}
}
