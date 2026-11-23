package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// composerArea is the bottom of the pane. A pending approval takes the whole
// slot: the decision is what the window is for while one waits, and a modal on
// top of a composer would hide the message it is about.
func (a *App) composerArea(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Padding(0, gutter, 14, gutter).Children(func() {
		// The box is as wide as the transcript reads: at a wide window a composer
		// stretched to the pane's edge leaves the text it sends hanging over an
		// empty right margin, and the two measures no longer line up.
		ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
			ui.Column(c).Grow(1).MaxWidth(maxContent).Children(func() {
				if len(a.pending) > 0 {
					a.approvalCard(c, t, a.pending[0], len(a.pending)-1)
					return
				}
				a.composerCard(c, t)
			})
		})
	})
}

// composerCard is Beautiful UI's Prompt Bar at its default size: one row of
// [ + | field | model | send ] inside a 14px card, the card's own hairline
// turning stronger while the field has the keyboard. Larger than a field and
// smaller than a panel — the same measure the transcript reads at.
func (a *App) composerCard(c *ui.Context, t theme.Tokens) {
	// Keep the preference mirror current while typing, so leaving the window
	// without switching chats still files what was written.
	if a.active != 0 {
		a.prefs.setDraft(a.active, a.draft)
	}
	a.syncMentions()
	card := ui.Column(c).FillWidth().Padding(6).Radius(theme.RadiusWindow).
		Border(1, t.Line).Background(t.Surface).Gap(6)
	card.Children(func() {
		a.attachmentChips(c, t)
		a.mentionList(c, t)
		ui.Row(c).FillWidth().AlignItems(ui.End).Gap(4).Children(func() {
			a.attachButton(c, t)
			input := ui.TextAreaBase(c, &a.draft).Grow(1).Lines(1, 8).MinHeight(22).
				FontSize(theme.TextBody).Placeholder(i18n.T("composerPlaceholder"))
			if a.focusComposer {
				input.AutoFocus()
				a.focusComposer = false
			}
			input.OnTextKey(a.composerKey)
			input.OnPaste(func(text string) bool {
				if text != "" {
					return false // text pastes as usual
				}
				return a.pasteImage(c)
			})
			a.modelChip(c, t)
			a.sendButton(c, t)
		})
	})
}

// canSend is the send button's own question: there is something to send, and a
// chat to send it to. The prompt bar greys the button out until there is.
func (a *App) canSend() bool {
	return a.active != 0 && (strings.TrimSpace(a.draft) != "" || len(a.draftImage) > 0)
}

// composerKey takes Enter (send), Shift+Enter (newline, left to the input), and
// the arrows that walk back through what was already said.
func (a *App) composerKey(k ui.TextKey) bool {
	if a.mentionKey(k) {
		return true
	}
	switch {
	case k.Key == ui.KeyEnter && k.Mods&ui.Shift == 0:
		a.submit()
		return true
	case k.Key == ui.KeyUp && k.Mods == 0 && !containsNewline(a.draft):
		return a.historyBack()
	case k.Key == ui.KeyDown && k.Mods == 0 && !containsNewline(a.draft):
		return a.historyForward()
	}
	return false
}

// composerCue is the line under the box. With text in it the placeholder is
// gone, so this is where the Enter rule lives; an empty box already carries it
// in the placeholder, and saying it twice would just be noise.
func (a *App) composerCue() string {
	if strings.TrimSpace(a.draft) == "" {
		return ""
	}
	return i18n.T("composerHint")
}

func containsNewline(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return true
		}
	}
	return false
}

// historyBack walks towards older messages; the composer's own text is lost
// once the walk starts, which is what every shell does.
func (a *App) historyBack() bool {
	if len(a.history) == 0 {
		return false
	}
	if a.histIndex < 0 {
		a.histIndex = len(a.history)
	}
	if a.histIndex == 0 {
		return true
	}
	a.histIndex--
	a.draft = a.history[a.histIndex]
	return true
}

func (a *App) historyForward() bool {
	if a.histIndex < 0 || a.histIndex >= len(a.history)-1 {
		a.histIndex = len(a.history)
		if a.histIndex > 0 {
			a.draft = ""
		}
		return a.histIndex > 0
	}
	a.histIndex++
	a.draft = a.history[a.histIndex]
	return true
}

// pasteImage attaches a screenshot from the clipboard; text pastes through.
func (a *App) pasteImage(c *ui.Context) bool {
	data := c.Services().ReadClipboardData(transfer.PNG)
	if len(data.Items()) == 0 {
		return false
	}
	png, err := data.Read(transfer.PNG)
	if err != nil || len(png) == 0 {
		return false
	}
	a.addImages([]imageRef{{mime: "image/png", data: png}})
	return true
}

func (a *App) attachmentChips(c *ui.Context, t theme.Tokens) {
	if len(a.draftImage) == 0 {
		return
	}
	ui.Row(c).FillWidth().Gap(6).Children(func() {
		for i := range a.draftImage {
			index := i
			chip := ui.Row(c).Gap(4).AlignItems(ui.Center).Height(26).Padding(2).
				Radius(theme.RadiusChip).Background(t.Field)
			chip.Children(func() {
				if bitmap := a.draftImage[index].bitmap(); bitmap != nil {
					ui.Image(c, bitmap).Size(22, 22).Radius(5).Clip()
				}
				remove := ui.ButtonBase(c).Size(20, 20).Radius(5).Justify(ui.Center).
					AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("removeImage"))
				remove.Children(func() {
					ui.Icon(c, icons.X).Size(9, 9).TextColor(t.MutedFg)
				})
				remove.OnClick(func() {
					a.draftImage = append(a.draftImage[:index], a.draftImage[index+1:]...)
					a.invalidate()
				})
			})
		}
	})
}

// attachButton is the prompt bar's leading "+": a 28px square with a hair of
// hover, and the file picker behind it.
func (a *App) attachButton(c *ui.Context, t theme.Tokens) {
	button := ui.ButtonBase(c).Size(28, 28).Radius(theme.RadiusControl).Justify(ui.Center).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("attachImage"))
	if button.Hovered() {
		button = button.Background(t.Hover)
	}
	button.Children(func() {
		ui.Icon(c, icons.Plus).Size(16, 16).TextColor(t.Ink3)
	})
	button.OnClick(a.attachImages)
}

// sendButton is send while idle and stop while a turn runs: one button, one
// place the eye already is.
func (a *App) sendButton(c *ui.Context, t theme.Tokens) {
	running := a.running()
	ready := running || a.canSend()
	back, ink := t.LineStrong, t.Ink2
	if ready {
		back, ink = t.Ink, t.Surface
	}
	button := ui.ButtonBase(c).Size(28, 28).Radius(theme.RadiusControl).Justify(ui.Center).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Background(back)
	label := i18n.T("send")
	if running {
		label = i18n.T("stop")
	}
	button = button.Tooltip(label)
	button.Children(func() {
		if running {
			ui.Icon(c, icons.Square).Size(11, 11).TextColor(ink)
		} else {
			ui.Icon(c, icons.ArrowUp).Size(16, 16).TextColor(ink)
		}
	})
	if running {
		button.OnClick(a.abort)
	} else {
		button.OnClick(a.submit)
	}
}

// approvalCard replaces the composer while a tool call waits. It is Beautiful
// UI's ApprovalCard shell: a shadowed surface card with no border and no spine,
// the question as its heading, the answers as a quiet pair at the foot. The
// click that decides is the only loud thing on it.
func (a *App) approvalCard(c *ui.Context, t theme.Tokens, approval protocol.PendingApproval, more int) {
	card := ui.Column(c).FillWidth().Radius(theme.Radius).Background(t.Surface).
		Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.10)).Padding(14).Gap(10)
	card.Children(func() {
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, i18n.TF("approvalTitle", map[string]any{"tool": approval.Tool})).
				Grow(1).FontSize(theme.TextTitle).FontWeight(500).TextColor(t.Ink).MaxLines(1)
			if more > 0 {
				ui.Text(c, i18n.TF("morePending", map[string]any{"n": more})).
					FontSize(theme.TextMicro).TextColor(t.Ink3)
			}
		})
		if summary := approvalSummary(approval); summary != "" {
			ui.Text(c, summary).FontSize(theme.TextBody).TextColor(t.Ink2).MaxLines(3)
		}
		ui.Row(c).FillWidth().Justify(ui.End).Gap(6).Children(func() {
			a.approvalButton(c, t, i18n.T("deny"), false, approval.CallID, false)
			if approval.GrantKey != "" {
				a.approvalSessionButton(c, t, approval.CallID, i18n.T("allowSession"), a.decideSession)
				a.approvalSessionButton(c, t, approval.CallID, i18n.T("allowSessionAll"), a.decideSessionAll)
			}
			a.approvalButton(c, t, i18n.T("allow"), true, approval.CallID, true)
		})
	})
}

// approvalSessionButton answers "yes, and stop asking for a while": the bridge
// keeps the grant per conversation, either for this risk class or, with `all`,
// for every class that does not destroy data.
func (a *App) approvalSessionButton(c *ui.Context, t theme.Tokens, callID, label string, decide func(string)) {
	button := ui.ButtonBase(c).Height(28).Padding(0, 10).Radius(theme.RadiusControl).
		Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Background(t.Surface)
	if button.Hovered() {
		button = button.Background(t.Hover)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(theme.TextRow).TextColor(t.Ink2)
	})
	button.OnClick(func() { decide(callID) })
}

func (a *App) approvalButton(c *ui.Context, t theme.Tokens, label string, primary bool, callID string, approved bool) {
	button := ui.ButtonBase(c).Height(28).Radius(theme.RadiusControl).Padding(0, 12).
		Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if primary {
		// Allow is the answer the card exists to ask for, so it is the one accent
		// thing on it; deny is a ghost button that only tints on hover.
		button = button.Background(t.Brand)
	} else {
		button = button.Background(t.Surface)
		if button.Hovered() {
			button = button.Background(t.Hover)
		}
	}
	if a.busy[callID] {
		button = button.Disabled(true)
	}
	button.Children(func() {
		color := t.Ink2
		if primary {
			color = ui.RGBA(255, 255, 255, 1)
		}
		ui.Text(c, label).FontSize(theme.TextRow).FontWeight(500).TextColor(color)
	})
	button.OnClick(func() { a.decide(callID, approved) })
}

// approvalSummary is what the bridge says the call wants to do, plus why it
// asked, when it said that separately.
func approvalSummary(approval protocol.PendingApproval) string {
	summary := clip(oneLine(approval.Summary), 160)
	if approval.Reason != "" && approval.Reason != approval.Summary {
		summary = strings.TrimSpace(summary + " · " + clip(oneLine(approval.Reason), 120))
	}
	return summary
}

// usageChip is the chat's running cost and spend, drawn in the header beside the
// chat's name: the number a person checks before sending one more long prompt.
// It used to sit in the prompt bar next to the model, where a fourth control
// left the input too little room to look like an input.
func (a *App) usageChip(c *ui.Context, t theme.Tokens) {
	th := a.threads[a.active]
	if th == nil || th.usage == nil || th.usage.Turns == 0 {
		return
	}
	ui.Text(c, compactCost(th.usage.Cost)+" · "+compactTokens(int64(th.usage.Tokens))+" tok").
		FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1).
		Padding(0, 2).
		Tooltip(i18n.TF("usageTurns", map[string]any{"n": th.usage.Turns}))
}

// compactCost keeps the cents of a normal bill and a third digit while a chat
// is still cheap enough that two decimals would read as zero.
func compactCost(cost float64) string {
	if cost < 0.1 {
		return fmt.Sprintf("$%.3f", cost)
	}
	return fmt.Sprintf("$%.2f", cost)
}
