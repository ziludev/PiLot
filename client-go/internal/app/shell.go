package app

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/theme"
)

const (
	// railWidth is the rail in points: wide enough for a title and a time, and
	// fixed, so the transcript never reflows under the cursor.
	railWidth = 260
	// gutter is the horizontal breathing room of the transcript and composer.
	gutter = 20
	// titleBar is the strip the window's traffic lights sit over.
	titleBar = 38
)

// shell is the window: rail, hairline, transcript column.
func (a *App) shell(c *ui.Context, t theme.Tokens) {
	ui.Row(c).Fill().Background(t.Background).Children(func() {
		a.railView(c, t)
		ui.Box(c).Width(1).FillHeight().Background(t.SidebarBorder)
		a.paneView(c, t)
	})
}

// paneView is the right side: a header naming the chat, the transcript, and the
// composer or approval card at the bottom.
func (a *App) paneView(c *ui.Context, t theme.Tokens) {
	ui.Column(c).Grow(1).FillHeight().Children(func() {
		a.paneHeader(c, t)
		a.transcriptView(c, t)
		a.statusStrip(c, t)
		a.composerArea(c, t)
	})
}

func (a *App) paneHeader(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(44).Padding(0, gutter).AlignItems(ui.Center).Gap(8).Children(func() {
		if surface, ok := a.activeConv(); ok {
			ui.Text(c, surface.Title).FontSize(13).FontWeight(600).TextColor(t.Foreground).MaxLines(1)
		}
		ui.Spacer(c)
		if !a.online {
			ui.Icon(c, icons.AlertTriangle).Size(11, 11).TextColor(t.Destructive)
			ui.Text(c, i18n.T("loadFailed")).FontSize(11).TextColor(t.MutedFg).MaxLines(1)
			retry := ui.ButtonBase(c).Radius(6).Padding(2, 8).Cursor(ui.CursorPointer)
			retry.Children(func() {
				ui.Text(c, i18n.T("retry")).FontSize(11).TextColor(t.Primary)
			})
			retry.OnClick(a.requestRefresh)
		}
	})
}

// queueRows is how many waiting messages the strip spells out; past that it
// counts them.
const queueRows = 3

// statusStrip draws what the composer cannot say while it is an approval card:
// the messages waiting behind the running turn, and the last failed action.
func (a *App) statusStrip(c *ui.Context, t theme.Tokens) {
	inbox := a.inboxItems()
	elapsed, running := a.runSoFar()
	if len(inbox) == 0 && a.notice == "" && !running {
		return
	}
	ui.Column(c).FillWidth().Padding(0, gutter, 0, gutter).Gap(2).Children(func() {
		if running {
			ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Clock).Size(10, 10).TextColor(t.MutedFg)
				ui.Text(c, i18n.TF("runElapsed", map[string]any{"t": shortDuration(elapsed)})).
					FontSize(10.5).TextColor(t.MutedFg)
			})
		}
		for index, item := range inbox {
			if index == queueRows {
				ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icons.Clock).Size(10, 10).TextColor(t.MutedFg)
					ui.Text(c, i18n.TF("queueMore", map[string]any{"n": len(inbox) - index})).
						FontSize(10.5).TextColor(t.MutedFg)
				})
				break
			}
			text := inboxText(item.Content)
			if text == "" {
				text = i18n.T("queueUntitled")
			}
			ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Clock).Size(10, 10).TextColor(t.MutedFg)
				label := i18n.T("queued")
				if item.Mode == "steer" {
					label = i18n.T("queueSteer")
				}
				ui.Text(c, label).FontSize(10.5).TextColor(t.MutedFg)
				ui.Text(c, text).FontSize(10.5).TextColor(t.MutedFg).MaxLines(1).Grow(1)
			})
		}
		if a.notice != "" {
			ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.AlertTriangle).Size(10, 10).TextColor(t.Destructive)
				ui.Text(c, a.notice).FontSize(10.5).TextColor(t.Destructive).MaxLines(2)
			})
		}
	})
}
