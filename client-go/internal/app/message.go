package app

// The action bar under a message: what every chat client grew once the reader
// started wanting to do something with an answer. It follows assistant-ui's
// shape — copy and regenerate on the answer, copy and edit on the question —
// with one difference: 编辑重发 puts the words back in the composer instead of
// rewriting history, because these transcripts are the agent's record, and a
// message edited in place would leave the answer talking to something that was
// never said.

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/theme"
)

// messageActions draws the bar under one message. It is always built and hidden
// with opacity rather than skipped, so the row keeps its height and the thread
// does not jump as the pointer moves.
func (a *App) messageActions(c *ui.Context, t theme.Tokens, row threadRow, hovered, fromUser bool) {
	bar := ui.Row(c).FillWidth().Height(20).Gap(2).AlignItems(ui.Center)
	if fromUser {
		bar = bar.Justify(ui.End)
	}
	bar.Opacity(hoverOpacity(hovered)).IgnoreInput(!hovered)
	bar.Children(func() {
		if fromUser {
			a.messageAction(c, t, icons.Pencil, i18n.T("editResend"), func() { a.editMessage(row) })
			a.messageAction(c, t, icons.Copy, i18n.T("copy"), func() { a.copyMessage(c, row) })
			return
		}
		a.messageAction(c, t, icons.Copy, i18n.T("copy"), func() { a.copyMessage(c, row) })
		if row.turn != 0 && strings.TrimSpace(row.text) != "" {
			a.messageAction(c, t, icons.RefreshCw, i18n.T("regenerate"), func() { a.retryTurn(row.turn) })
		}
	})
}

// hoverOpacity is how visible an idle action bar is: present enough to hint
// that the message has actions, quiet enough not to read as content.
func hoverOpacity(hovered bool) float32 {
	if hovered {
		return 1
	}
	return 0
}

// messageAction is one icon in that bar. The word is in the tooltip rather than
// on the button: three verbs spelled out under every message is more text than
// the message has, and the marks (copy, pencil, arrow round a circle) are the
// ones every editor and mail client already uses for them.
func (a *App) messageAction(c *ui.Context, t theme.Tokens, icon *ui.SVG, label string, onClick func()) {
	btn := ui.ButtonBase(c).Height(20).Width(20).Radius(4).
		Cursor(ui.CursorPointer).Tooltip(label).Label(label)
	btn.Children(func() {
		ui.Icon(c, icon).Size(11.5, 11.5).TextColor(t.MutedFg)
	})
	btn.OnClick(onClick)
}

// copyMessage puts the words on the clipboard and says so: without the toast a
// copy is indistinguishable from a click that did nothing.
func (a *App) copyMessage(c *ui.Context, row threadRow) {
	a.copyText(c, row.text)
}

// copyText puts words on the clipboard and says so. Tool output copies through
// the same door, so one toast means one thing.
func (a *App) copyText(c *ui.Context, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	c.WriteClipboard(text)
	c.AddToast(ui.Toast{Title: i18n.T("copied"), Type: "success", Timeout: 2000})
}

// editMessage puts a question back in the composer for another try.
func (a *App) editMessage(row threadRow) {
	if row.text == "" && len(row.images) == 0 {
		return
	}
	a.draft = row.text
	a.draftImage = append([]imageRef(nil), row.images...)
	a.focusComposer = true
	a.invalidate()
}
