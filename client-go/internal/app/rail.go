package app

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/theme"
)

// railView is the left column: the brand line with the approval count, the
// filter, the two segments, and the list they switch between.
func (a *App) railView(c *ui.Context, t theme.Tokens) {
	// The edge sits over the seam between the two columns, so the pointer finds
	// it before the list underneath does.
	ui.Column(c).Width(a.railW).FillHeight().Background(t.Sidebar).Children(func() {
		edge := ui.Box(c).Absolute().Right(-3).Top(0).Width(6).FillHeight().Cursor(ui.CursorResizeEW)
		if dx, _, held := edge.Dragged(); dx != 0 || a.dragging {
			a.resizeRail(dx, held)
		}
		// The window hides its title bar, so the traffic lights sit over this.
		ui.Box(c).Height(titleBar)
		a.railBrand(c, t)
		a.railFilter(c, t)
		a.railSegments(c, t)
		a.railList(c, t)
		a.railFooter(c, t)
	})
}

func (a *App) railBrand(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(26).Padding(0, 12).AlignItems(ui.Center).Gap(6).Children(func() {
		a.avatarView(c, 24)
		ui.Text(c, "PiLot").FontSize(12).FontWeight(600).TextColor(t.Foreground)
		ui.Spacer(c)
		compose := ui.ButtonBase(c).Size(20, 20).Radius(4).Justify(ui.Center).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("newChat"))
		compose.Children(func() {
			ui.Icon(c, icons.Plus).Size(12, 12).TextColor(t.MutedFg)
		})
		if compose.Clicked() {
			a.newChat()
		}
		if n := len(a.pending); n > 0 {
			badge := ui.Box(c).Radius(9).Padding(1, 6).Background(t.Destructive)
			badge.Children(func() {
				ui.Text(c, itoa(n)).FontSize(9).FontWeight(600).TextColor(t.Background)
			})
		}
	})
}

func (a *App) railFilter(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(36).Padding(0, 10, 8, 10).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Grow(1).Height(28).Radius(7).Padding(0, 8).Gap(6).
			Background(t.SidebarAccent).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Search).Size(11, 11).TextColor(t.MutedFg)
			ui.TextInputBase(c, &a.filter).FontSize(11.5).
				Placeholder(i18n.T("filter")).Height(20)
		})
	})
}

// railSegments is the 会话 | 任务 switch; it keeps its own state, so the list
// below it is a pure function of the segment and the filter.
func (a *App) railSegments(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(34).Padding(0, 10, 8, 10).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Grow(1).Height(26).Radius(7).Padding(2).Gap(2).
			Background(t.SidebarAccent).Children(func() {
			a.railSegment(c, t, i18n.T("chats"), railChats)
			a.railSegment(c, t, i18n.T("tasks"), railTasks)
		})
	})
}

func (a *App) railSegment(c *ui.Context, t theme.Tokens, label string, section railSection) {
	active := a.rail == section
	button := ui.ButtonBase(c).Grow(1).Height(22).Radius(5).Justify(ui.Center).Cursor(ui.CursorPointer)
	if active {
		button = button.Background(t.Background)
	}
	button.Children(func() {
		color := t.MutedFg
		weight := 500
		if active {
			color, weight = t.Foreground, 600
		}
		ui.Text(c, label).FontSize(11).FontWeight(weight).TextColor(color)
	})
	button.OnClick(func() {
		if a.rail == section {
			return
		}
		a.rail = section
		a.stamp++
		a.vm.railList.Selected = nil
		a.invalidate()
	})
}

// railFooter is the two switches this window has no dialog for: the language and
// the theme. They sit where a settings panel would have started.
func (a *App) railFooter(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(30).Padding(0, 10).Gap(2).AlignItems(ui.Center).Children(func() {
		label, tip := "中文", "English"
		if i18n.Current() == i18n.EN {
			label, tip = "EN", "中文"
		}
		a.footerText(c, t, label, tip, a.toggleLang)
		ui.Spacer(c)
		dark := a.prefs.dark(c.Theme().Dark)
		icon, tip := icons.Moon, i18n.T("themeDark")
		if dark {
			icon, tip = icons.Sun, i18n.T("themeLight")
		}
		a.footerIcon(c, t, icon, tip, a.toggleTheme)
	})
}

func (a *App) footerText(c *ui.Context, t theme.Tokens, label, tip string, onClick func()) {
	button := ui.ButtonBase(c).Height(22).Radius(5).Padding(0, 7).Justify(ui.Center).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(tip)
	button.Children(func() {
		ui.Text(c, label).FontSize(10.5).FontWeight(500).TextColor(t.MutedFg)
	})
	button.OnClick(onClick)
}

func (a *App) footerIcon(c *ui.Context, t theme.Tokens, icon *ui.SVG, tip string, onClick func()) {
	button := ui.ButtonBase(c).Size(22, 22).Radius(5).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(tip)
	button.Children(func() {
		ui.Icon(c, icon).Size(12, 12).TextColor(t.MutedFg)
	})
	button.OnClick(onClick)
}

func (a *App) railList(c *ui.Context, t theme.Tokens) {
	rows := a.railRowsCached()
	a.vm.railList.Key = func(row int) any {
		if row < 0 || row >= len(rows) {
			return row
		}
		return railKey(rows[row])
	}
	// The list's width is spelled out: its items are laid out at the viewport's
	// width, and a row that measured itself against the window would push its
	// time column under the transcript.
	// FillHeight matters: without it the column sizes to its content, the list
	// draws everything and gets clipped, and the wheel has nothing to scroll.
	ui.Column(c).FillWidth().FillHeight().Padding(0, 8, 10, 8).Children(func() {
		ui.List(c, &a.vm.railList, len(rows), func(row int) {
			a.railRow(c, t, rows[row])
		}).Grow(1).Width(a.railW - 16)
	})
}

// railRowsCached recomputes the rail only when its inputs changed.
func (a *App) railRowsCached() []railRow {
	if a.vm.railCache == nil || a.vm.railStamp != a.stamp {
		a.vm.railCache = a.railRows()
		a.vm.railStamp = a.stamp
	}
	return a.vm.railCache
}

func railKey(row railRow) any {
	switch row.kind {
	case railChat:
		return "c" + itoa(row.conv.ConversationID)
	case railTask:
		return "t" + itoa(row.task.ID)
	default:
		return "h" + row.label
	}
}

func (a *App) railRow(c *ui.Context, t theme.Tokens, row railRow) {
	switch row.kind {
	case railGroup:
		ui.Row(c).FillWidth().Height(24).Padding(6, 7, 2, 7).AlignItems(ui.Center).Children(func() {
			ui.Text(c, row.label).FontSize(9.5).FontWeight(600).TextColor(t.MutedFg)
		})
	case railHint:
		ui.Row(c).FillWidth().Height(30).Padding(0, 7).AlignItems(ui.Center).Children(func() {
			ui.Text(c, row.label).FontSize(11).TextColor(t.MutedFg).MaxLines(1)
		})
	case railTask:
		a.railTaskRow(c, t, row)
	default:
		a.railChatRow(c, t, row)
	}
}

func (a *App) railChatRow(c *ui.Context, t theme.Tokens, row railRow) {
	active := row.conv.ConversationID == a.active
	item := ui.ButtonBase(c).FillWidth().Height(30).Radius(6).Padding(0, 7).
		Cursor(ui.CursorPointer).Gap(6).AlignItems(ui.Center)
	if active {
		item = item.Background(t.SidebarAccent)
	}
	item.Children(func() {
		title := row.conv.Title
		if title == "" {
			title = "#" + itoa(row.conv.ConversationID)
		}
		// The title gives up width first: the time and the source badge are
		// short and fixed, and a long title must not push them off the rail.
		ui.Text(c, title).Grow(1).Shrink(1).MinWidth(0).FontSize(11.5).
			TextColor(t.Foreground).MaxLines(1)
		if waiting := a.queued[row.conv.ConversationID]; waiting > 0 {
			// Something is waiting for this chat: clicking the row is how you get
			// to it, since the messages themselves live in that chat's composer.
			ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Clock).Size(9, 9).TextColor(t.MutedFg)
				ui.Text(c, itoa(waiting)).FontSize(9).TextColor(t.MutedFg)
			})
		}
		if row.meta != "" {
			ui.Text(c, row.meta).FontSize(9).TextColor(t.MutedFg)
		}
		if when := relTime(a.now, row.at); when != "" {
			ui.Text(c, when).FontSize(9).TextColor(t.MutedFg)
		}
		if row.unread {
			ui.Box(c).Size(5, 5).Radius(3).Background(t.Primary)
		}
	})
	item.OnClick(func() { a.selectConversation(row.conv.ConversationID) })
}

// resizeRail moves the seam by dx, and writes the width down when the pointer
// lets go: a frame-by-frame file write would be a write per 16ms.
func (a *App) resizeRail(dx float32, held bool) {
	if dx != 0 {
		a.railW = min(max(a.railW+dx, railMin), railMax)
		a.dragging = held
		a.invalidate()
		return
	}
	if a.dragging {
		a.dragging = false
		a.prefs.Rail = int(a.railW)
		savePrefs(a.prefs)
	}
}
