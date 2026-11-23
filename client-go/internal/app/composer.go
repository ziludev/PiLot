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

func (a *App) composerCard(c *ui.Context, t theme.Tokens) {
	card := ui.Column(c).FillWidth().Padding(10, 12, 8, 12).Radius(12).
		Border(1, t.Border).Background(t.Card).Gap(6)
	// Keep the preference mirror current while typing, so leaving the window
	// without switching chats still files what was written.
	if a.active != 0 {
		a.prefs.setDraft(a.active, a.draft)
	}
	a.syncMentions()
	card.Children(func() {
		a.attachmentChips(c, t)
		a.mentionList(c, t)
		input := ui.TextAreaBase(c, &a.draft).FillWidth().Lines(1, 8).MinHeight(22).FontSize(12.5).
			Placeholder(i18n.T("composerPlaceholder"))
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
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
			a.attachButton(c, t)
			a.modelChip(c, t)
			a.usageChip(c, t)
			if cue := a.composerCue(); cue == "" {
				ui.Box(c).Grow(1)
			} else {
				ui.Text(c, cue).Grow(1).FontSize(10).TextColor(t.MutedFg).MaxLines(1)
			}
			a.sendButton(c, t)
		})
	})
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
			chip := ui.Row(c).Gap(4).AlignItems(ui.Center).Padding(2, 2, 2, 2).
				Radius(8).Background(t.Muted)
			chip.Children(func() {
				if bitmap := a.draftImage[index].bitmap(); bitmap != nil {
					ui.Image(c, bitmap).Size(40, 40).Radius(6).Clip()
				}
				remove := ui.ButtonBase(c).Size(16, 16).Radius(8).Justify(ui.Center).
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

func (a *App) attachButton(c *ui.Context, t theme.Tokens) {
	button := ui.ButtonBase(c).Size(24, 24).Radius(6).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(i18n.T("attachImage"))
	button.Children(func() {
		ui.Icon(c, icons.Paperclip).Size(13, 13).TextColor(t.MutedFg)
	})
	button.OnClick(a.attachImages)
}

// sendButton is send while idle and stop while a turn runs: one button, one
// place the eye already is.
func (a *App) sendButton(c *ui.Context, t theme.Tokens) {
	running := a.running()
	button := ui.ButtonBase(c).Size(28, 28).Radius(14).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Background(t.Primary)
	label := i18n.T("send")
	if running {
		label = i18n.T("stop")
	}
	button = button.Tooltip(label)
	button.Children(func() {
		if running {
			ui.Icon(c, icons.Square).Size(11, 11).TextColor(t.PrimaryFg)
		} else {
			ui.Icon(c, icons.Send).Size(13, 13).TextColor(t.PrimaryFg)
		}
	})
	if running {
		button.OnClick(a.abort)
	} else {
		button.OnClick(a.submit)
	}
}

// approvalCard replaces the composer while a tool call waits. It names the
// tool, shows what it wants to run, and offers the two answers.
func (a *App) approvalCard(c *ui.Context, t theme.Tokens, approval protocol.PendingApproval, more int) {
	card := ui.Row(c).FillWidth().Radius(12).Border(1, t.Border).Background(t.Card).
		AlignItems(ui.Stretch).Children(func() {
		ui.Box(c).Width(2).AlignSelf(ui.Stretch).Background(t.Warning)
		ui.Column(c).Grow(1).Padding(10, 12).Gap(6).Children(func() {
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Children(func() {
				ui.Icon(c, icons.ShieldAlert).Size(13, 13).TextColor(t.Warning)
				ui.Text(c, i18n.TF("approvalTitle", map[string]any{"tool": approval.Tool})).
					Grow(1).FontSize(12.5).FontWeight(600).TextColor(t.Foreground).MaxLines(1)
				if more > 0 {
					ui.Text(c, i18n.TF("morePending", map[string]any{"n": more})).
						FontSize(10.5).TextColor(t.MutedFg)
				}
			})
			if summary := approvalSummary(approval); summary != "" {
				ui.Text(c, summary).FontSize(11.5).TextColor(t.MutedFg).MaxLines(3)
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
	})
	_ = card
}

// approvalSessionButton answers "yes, and stop asking for a while": the bridge
// keeps the grant per conversation, either for this risk class or, with `all`,
// for every class that does not destroy data.
func (a *App) approvalSessionButton(c *ui.Context, t theme.Tokens, callID, label string, decide func(string)) {
	button := ui.ButtonBase(c).Height(26).Padding(0, 10).Radius(7).Justify(ui.Center).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Border(1, t.Border).
		Label(label)
	button.Children(func() {
		ui.Text(c, label).FontSize(11.5).TextColor(t.MutedFg)
	})
	button.OnClick(func() { decide(callID) })
}

func (a *App) approvalButton(c *ui.Context, t theme.Tokens, label string, primary bool, callID string, approved bool) {
	button := ui.ButtonBase(c).Height(24).Radius(6).Padding(0, 10).Justify(ui.Center).
		Cursor(ui.CursorPointer)
	if primary {
		button = button.Background(t.Primary)
	} else {
		button = button.Border(1, t.Border).Background(t.Background)
	}
	if a.busy[callID] {
		button = button.Disabled(true)
	}
	button.Children(func() {
		color := t.Foreground
		if primary {
			color = t.PrimaryFg
		}
		ui.Text(c, label).FontSize(11).FontWeight(500).TextColor(color)
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

// usageChip is the chat's running cost and spend, next to the model that is
// spending it: the number a person checks before sending one more long prompt.
func (a *App) usageChip(c *ui.Context, t theme.Tokens) {
	th := a.threads[a.active]
	if th == nil || th.usage == nil || th.usage.Turns == 0 {
		return
	}
	ui.Text(c, compactCost(th.usage.Cost)+" · "+compactTokens(int64(th.usage.Tokens))+" tok").
		FontSize(10.5).TextColor(t.MutedFg).MaxLines(1).
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
