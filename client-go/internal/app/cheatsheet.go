package app

// The shortcuts, written down where they can be read.
//
// Five keys are enough to be useful and few enough to forget; a sheet the reader
// can call up is cheaper than putting them all in the interface.

import (
	"pitta/internal/i18n"
	"pitta/internal/theme"

	"github.com/egoist/mygo/ui"
)

// shortcut is one line of the sheet: the key as it is printed on a Mac keyboard,
// and what it does.
type shortcut struct{ keys, label string }

// shortcuts is the sheet, in the order the keys are worth learning.
func (a *App) shortcuts() []shortcut {
	return []shortcut{
		{"⌘K", i18n.T("palette")},
		{"⌘P", i18n.T("gotoFile")},
		{"⌘J", i18n.T("jumpToLatest")},
		{"⇧⌘E", i18n.T("exportMarkdown")},
		{"⌘⇧/", i18n.T("shortcuts")},
	}
}

// toggleCheatsheet shows or hides the sheet. It is a dialog like any other, so
// opening it closes the palette behind it.
func (a *App) toggleCheatsheet() {
	a.cheatsheet = !a.cheatsheet
	if a.cheatsheet {
		a.palette.open = false
	}
	a.invalidate()
}

// cheatsheetDialog draws the sheet: keys in one column, what they do in the other.
func (a *App) cheatsheetDialog(c *ui.Context, t theme.Tokens) {
	if !a.cheatsheet {
		return
	}
	ui.Modal(c, &a.cheatsheet, func() {
		// The card covers the modal's own frame, the way the palette's does; a
		// narrower card inside it would read as two nested panels.
		ui.Column(c).Width(480).Gap(2).Padding(14).Radius(12).
			Background(t.Popover).Border(1, t.Border).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.35)).
			Children(func() {
				ui.Text(c, i18n.T("shortcuts")).FontSize(13).FontWeight(600).
					TextColor(t.Foreground).Padding(0, 0, 8, 0)
				for _, s := range a.shortcuts() {
					ui.Row(c).Width(452).Padding(2, 0).Children(func() {
						ui.Text(c, s.keys).Width(70).FontSize(12).TextColor(t.Foreground)
						ui.Text(c, s.label).FontSize(12).TextColor(t.MutedFg)
					})
				}
				ui.Text(c, i18n.T("shortcutHint")).FontSize(10).TextColor(t.MutedFg).
					Padding(10, 0, 0, 0)
			})
	})
}
