package app

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/theme"
)

const (
	// railWidth is the rail in points before anyone drags its edge. Beautiful
	// UI's own sidebar is 224; a Chinese title needs a little more.
	railWidth = 232
	// railMin and railMax bound the drag: narrower than the first, titles are
	// initials; wider than the second, the transcript has no room.
	railMin = 200
	railMax = 420
	// gutter is the horizontal breathing room of the transcript and composer.
	gutter = 22
	// titleBar is the strip the window's traffic lights sit over.
	titleBar = 38
	// paneInset is how far the conversation card floats off the window edge.
	paneInset = 8
)

// shell is the window: the canvas, the rail on it, and one surface card for the
// conversation. Beautiful UI separates chrome from content by layer, not by a
// 1px divider, so there is no hairline between the two columns.
func (a *App) shell(c *ui.Context, t theme.Tokens) {
	ui.Row(c).Fill().Background(t.Page).Children(func() {
		a.railView(c, t)
		// The inset is padding on a wrapper, not margin on each sheet: a Row
		// drops a child's vertical margin, so a margin here would leave the card
		// touching the window's top and bottom edges.
		ui.Row(c).Grow(1).FillHeight().Padding(paneInset, paneInset).Children(func() {
			pane := ui.Column(c).FillHeight().Grow(1).
				Radius(theme.RadiusWindow).Border(1, t.Line).Background(t.Surface).Clip()
			pane.Children(func() { a.paneView(c, t) })
			if a.mem.open {
				a.drawerSlot(c, t)
				a.memoryView(c, t)
			}
			// One right edge, one drawer: two columns of detail do not fit, and
			// a stale flag must not be able to draw one over the other.
			if a.changes.open && !a.projects.open {
				a.drawerSlot(c, t)
				a.changesView(c, t)
			}
			if a.projects.open && !a.mem.open {
				a.drawerSlot(c, t)
				a.projectsView(c, t)
			}
			if a.tl.open {
				a.drawerSlot(c, t)
				a.timelineView(c, t)
			}
		})
	})
}

// drawerSlot is the gap between the conversation card and a drawer: the drawer
// floats on the same canvas, so the two read as separate sheets.
func (a *App) drawerSlot(c *ui.Context, t theme.Tokens) {
	ui.Box(c).Width(paneInset).FillHeight()
}

// paneView is the right side: a header naming the chat, the transcript, and the
// composer or approval card at the bottom.
func (a *App) paneView(c *ui.Context, t theme.Tokens) {
	ui.Column(c).Grow(1).FillHeight().Children(func() {
		a.paneHeader(c, t)
		a.syncFind()
		a.findBar(c, t)
		a.transcriptView(c, t)
		a.statusStrip(c, t)
		a.composerArea(c, t)
	})
}

func (a *App) paneHeader(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(48).Padding(0, gutter).AlignItems(ui.Center).Gap(8).Children(func() {
		if surface, ok := a.activeConv(); ok {
			ui.Text(c, surface.Title).FontSize(theme.TextTitle).FontWeight(500).
				TextColor(t.Ink).MaxLines(1)
		}
		ui.Spacer(c)
		// What the chat has cost so far sits with its name: the prompt bar is
		// [+ | field | model | send] exactly, and a fourth control in that row
		// pushed the input out of shape.
		a.usageChip(c, t)
		a.memoryToggle(c, t)
		a.changesToggle(c, t)
		a.timelineToggle(c, t)
		if !a.online {
			ui.Icon(c, icons.AlertTriangle).Size(11, 11).TextColor(t.Destructive)
			ui.Text(c, i18n.T("loadFailed")).FontSize(theme.TextSmall).TextColor(t.MutedFg).MaxLines(1)
			retry := ui.ButtonBase(c).Radius(theme.RadiusChip).Padding(2, 8).Cursor(ui.CursorPointer)
			retry.Children(func() {
				ui.Text(c, i18n.T("retry")).FontSize(theme.TextSmall).TextColor(t.Primary)
			})
			retry.OnClick(a.requestRefresh)
		}
	})
}

// memoryToggle is the header's way into the drawer. It sits before the offline
// warning because a warning pushes it aside when it matters.
func (a *App) memoryToggle(c *ui.Context, t theme.Tokens) {
	color := t.MutedFg
	if a.mem.open {
		color = t.Foreground
	}
	btn := ui.ButtonBase(c).Size(22, 22).Radius(theme.RadiusChip).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(i18n.T("memory"))
	if a.mem.open {
		btn.Background(t.Secondary)
	}
	btn.Children(func() {
		ui.Icon(c, icons.Database).Size(12, 12).TextColor(color)
	})
	btn.OnClick(a.toggleMemory)
}

// changesToggle is the header's way into the changes drawer, next to memory
// because both read the chat rather than act on it.
func (a *App) changesToggle(c *ui.Context, t theme.Tokens) {
	color := t.MutedFg
	if a.changes.open {
		color = t.Foreground
	}
	btn := ui.ButtonBase(c).Size(22, 22).Radius(theme.RadiusChip).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(i18n.T("changesTab"))
	if a.changes.open {
		btn.Background(t.Secondary)
	}
	btn.Children(func() {
		ui.Icon(c, icons.GitBranch).Size(12, 12).TextColor(color)
	})
	btn.OnClick(a.toggleChanges)
}

// timelineToggle is the header's way into the timeline, the third drawer that
// reads a chat rather than acting on it.
func (a *App) timelineToggle(c *ui.Context, t theme.Tokens) {
	color := t.Ink3
	if a.tl.open {
		color = t.Ink
	}
	btn := ui.ButtonBase(c).Size(22, 22).Radius(theme.RadiusChip).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(i18n.T("timeline"))
	if a.tl.open {
		btn.Background(t.Field)
	}
	btn.Children(func() {
		ui.Icon(c, icons.Clock).Size(12, 12).TextColor(color)
	})
	btn.OnClick(a.toggleTimeline)
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
	ui.Column(c).FillWidth().Padding(0, gutter, 0, gutter).Gap(3).Children(func() {
		if running {
			ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Clock).Size(10, 10).TextColor(t.MutedFg)
				ui.Text(c, i18n.TF("runElapsed", map[string]any{"t": shortDuration(elapsed)})).
					FontSize(theme.TextMicro).TextColor(t.MutedFg)
			})
		}
		for index, item := range inbox {
			if index == queueRows {
				ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icons.Clock).Size(10, 10).TextColor(t.MutedFg)
					ui.Text(c, i18n.TF("queueMore", map[string]any{"n": len(inbox) - index})).
						FontSize(theme.TextMicro).TextColor(t.MutedFg)
				})
				break
			}
			text := inboxText(item.Content)
			if text == "" {
				text = i18n.T("queueUntitled")
			}
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				// A chip names what the line is; the words after it are the
				// message. Same shape as a tool chip, so a queued turn reads as
				// something that will happen rather than as a passing note.
				label := i18n.T("queued")
				if item.Mode == "steer" {
					label = i18n.T("queueSteer")
				}
				chip := ui.Row(c).Height(20).Radius(theme.RadiusChip).Padding(0, 6).
					AlignItems(ui.Center).Background(t.Field)
				chip.Children(func() {
					ui.Text(c, label).FontSize(theme.TextSmall).FontWeight(500).TextColor(t.Ink2)
				})
				ui.Text(c, text).FontSize(theme.TextSmall).TextColor(t.Ink3).MaxLines(1).Grow(1)
			})
		}
		if a.notice != "" {
			// A notice is either news or a complaint, and the color says which:
			// an export that worked should not look like a file that failed.
			color, icon := t.Destructive, icons.AlertTriangle
			if a.noticeOK {
				color, icon = t.MutedFg, icons.Check
			}
			ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).Size(10, 10).TextColor(color)
				ui.Text(c, a.notice).FontSize(theme.TextMicro).TextColor(color).MaxLines(2)
			})
		}
	})
}
