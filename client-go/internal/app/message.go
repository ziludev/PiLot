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
			a.messageAction(c, t, i18n.T("editResend"), func() { a.editMessage(row) })
			a.messageAction(c, t, i18n.T("copy"), func() { a.copyMessage(c, row) })
			return
		}
		a.messageAction(c, t, i18n.T("copy"), func() { a.copyMessage(c, row) })
		if row.turn != 0 && strings.TrimSpace(row.text) != "" {
			a.messageAction(c, t, i18n.T("regenerate"), func() { a.retryTurn(row.turn) })
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

func (a *App) messageAction(c *ui.Context, t theme.Tokens, label string, onClick func()) {
	btn := ui.ButtonBase(c).Height(20).Radius(4).Padding(0, 6).Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Text(c, label).FontSize(10).TextColor(t.MutedFg).MaxLines(1)
	})
	btn.OnClick(onClick)
}

// copyMessage puts the words on the clipboard and says so: without the toast a
// copy is indistinguishable from a click that did nothing.
func (a *App) copyMessage(c *ui.Context, row threadRow) {
	text := row.text
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
