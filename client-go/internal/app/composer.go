package app

import (
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/motion"
	"pitta/internal/theme"
)

// composer is the input under the transcript: the pending approvals above it,
// the draft, and the row that sends it.
func (a *App) composer(c *ui.Context) {
	t := theme.Current(c)
	ui.Column(c).FillWidth().PaddingY(12).PaddingX(16).Gap(8).Children(func() {
		if len(a.approvals) > 0 {
			a.approvalBar(c, t)
		}
		a.imageStrip(c, t)
		// Enter breaks the line and Cmd+Enter sends, as the React composer
		// does: a Chinese input session confirms candidates with Enter, so
		// sending on it would cut sentences short.
		ui.TextArea(c, &a.draft).Fill().FontSize(13).MinHeight(64).Placeholder(i18n.T("placeholder")).
			// An image on the clipboard becomes an attachment instead of a
			// paste, which the React client could not do at all: its paste
			// handler only ever saw text.
			OnPaste(func(text string) bool { return a.pasteImage(c) }).
			OnTextKey(func(k ui.TextKey) bool {
				if k.Key == ui.KeyUp || k.Key == ui.KeyDown {
					return a.recallKey(k, k.Text())
				}
				if k.Key != ui.KeyEnter || k.Mods&(ui.Super|ui.Ctrl) == 0 {
					return false
				}
				text := k.Text()
				k.SetText("")
				a.sendText(text)
				return true
			})
		// The bottom row of Composer.tsx: picker, a hairline, the steer box on
		// the left; the send button on the right.
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			a.modelPicker(c)
			ui.Box(c).Width(1).Height(14).Background(t.Border)
			ui.Checkbox(c, &a.steer, i18n.T("steer")).FontSize(11)
			iconButton(c, icons.Paperclip, i18n.T("attachImage")).OnClick(func() {
				a.chooseImages()
			})
			ui.Row(c).Grow(1).Children(func() {})
			send := ui.ButtonBase(c).Padding(5, 12).Radius(6).Cursor(ui.CursorPointer).
				Background(t.Primary).Gap(6).AlignItems(ui.Center)
			send.Label(i18n.T("send"))
			send.Tooltip(i18n.T("send"))
			if send.Hovered() {
				send.Background(theme.Hover(t.Primary, t.PrimaryFg))
			}
			send.Children(func() {
				ui.Icon(c, icons.Send).Size(14, 14).TextColor(t.PrimaryFg)
				ui.Text(c, i18n.T("send")).FontSize(12).TextColor(t.PrimaryFg)
			})
			send.OnClick(func() { a.submit() })
		})
	})
}

// modelLabel is the model the next turn runs on, as ModelPicker shows it.
func (a *App) modelLabel() string {
	if a.model != nil && a.model.ModelID != "" {
		return a.model.ModelID
	}
	return i18n.T("model")
}

// approvalBar lists the tool calls waiting for a decision. A blocked run burns
// wall-clock time, so each row says how long it waited.
func (a *App) approvalBar(c *ui.Context, t theme.Tokens) {
	// A call that starts waiting slides up into the composer rather than
	// appearing between two frames.
	ui.Column(c).Fill().Padding(6, 8).Gap(6).Radius(6).Background(t.Muted).
		Transition(motion.PopoverEnter()).Children(func() {
		for _, approval := range a.approvals {
			approval := approval
			ui.Row(c).Fill().Gap(8).Children(func() {
				ui.Icon(c, icons.ShieldAlert).Size(12, 12).TextColor(t.Warning)
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					ui.Text(c, i18n.T("needsApproval")+" · "+approval.Tool+" — "+approval.Reason+waitedSuffix(approval.CreatedAt)).
						FontSize(11).TextColor(t.Foreground)
					if summary := strings.TrimSpace(approval.Summary); summary != "" {
						ui.Text(c, truncate(summary, 240)).FontSize(10).TextColor(t.MutedFg)
					}
				})
				ui.Button(c, i18n.T("allow")).OnClick(func() { a.decideApproval(approval.CallID, true, false) })
				if approval.GrantKey != "" {
					ui.Button(c, i18n.T("allowSession")).OnClick(func() { a.decideApproval(approval.CallID, true, true) })
				}
				ui.Button(c, i18n.T("deny")).OnClick(func() { a.decideApproval(approval.CallID, false, false) })
			})
		}
	})
}

// waitedSuffix words how long the call has been blocked.
// waitedSuffix says how long a call has been parked. createdAt is epoch
// milliseconds, as src/approvals.ts stamps it with Date.now().
func waitedSuffix(createdAt int64) string {
	if createdAt <= 0 {
		return ""
	}
	seconds := (time.Now().UnixMilli() - createdAt) / 1000
	if seconds < 0 {
		seconds = 0
	}
	return "  " + i18n.T("waited") + " " + strconv.FormatInt(seconds, 10) + "s"
}

// truncate cuts s to max runes, as the React bar slices summaries to 240.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
