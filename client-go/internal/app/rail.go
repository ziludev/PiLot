package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
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
	ui.Row(c).FillWidth().Height(36).Padding(0, 10, 8, 10).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Row(c).Grow(1).Height(28).Radius(7).Padding(0, 8).Gap(6).
			Background(t.SidebarAccent).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Search).Size(11, 11).TextColor(t.MutedFg)
			ui.TextInputBase(c, &a.filter).FontSize(11.5).
				Placeholder(i18n.T("filter")).Height(20)
		})
		// The key, shown next to the box it stands in for: a palette nobody
		// knows about is a palette nobody uses.
		keys := ui.ButtonBase(c).Size(24, 20).Radius(4).Justify(ui.Center).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("palette"))
		keys.Children(func() {
			ui.Text(c, "⌘K").FontSize(9.5).TextColor(t.MutedFg)
		})
		if keys.Clicked() {
			a.openPalette()
		}
	})
	// What was typed is local filtering first; the bridge is asked a beat
	// later, so a word typed at speed is one search rather than eight.
	a.watchSearch()
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

// railFooter is where the settings live now: one gear, because the panel behind
// it is about to hold more than a language and a theme.
func (a *App) railFooter(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(30).Padding(0, 10).Gap(2).AlignItems(ui.Center).Children(func() {
		ui.Spacer(c)
		// The calendar belongs to the task tab: that is the tab whose rows are
		// the work, and the work is what the calendar counts.
		if a.rail == railTasks {
			a.usageButton(c, t)
		}
		a.projectsButton(c, t)
		a.settingsButton(c, t)
	})
}

// projectsButton opens the drawer of repositories a chat can live in. It sits
// in the rail because a project is not a property of the chat that happens to be
// open: it is where the next chat could go.
func (a *App) projectsButton(c *ui.Context, t theme.Tokens) {
	color := t.MutedFg
	btn := ui.ButtonBase(c).Size(22, 22).Radius(5).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(i18n.T("projects"))
	if a.projects.open {
		color = t.Foreground
		btn = btn.Background(t.Secondary)
	}
	btn.Children(func() { ui.Icon(c, icons.Folder).Size(12, 12).TextColor(color) })
	btn.OnClick(a.toggleProjects)
}

// usageButton opens the calendar. It exists only on the task tab, because the
// calendar counts the work that tab lists.
func (a *App) usageButton(c *ui.Context, t theme.Tokens) {
	color := t.MutedFg
	btn := ui.ButtonBase(c).Size(22, 22).Radius(5).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(i18n.T("usage"))
	if a.usage.open {
		color = t.Foreground
		btn = btn.Background(t.Secondary)
	}
	btn.Children(func() {
		ui.Icon(c, icons.Calendar).Size(12, 12).TextColor(color)
	})
	btn.OnClick(a.toggleUsage)
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
	// Grow, not FillHeight: 100% of the rail's height would put the list's bottom
	// below the window (the footer's already spent 30 of it), so the list would
	// think all its rows were on screen and refuse to scroll.
	ui.Column(c).FillWidth().Grow(1).Padding(0, 8, 10, 8).Children(func() {
		ui.List(c, &a.vm.railList, len(rows), func(row int) {
			a.railRow(c, t, rows[row])
		}).Grow(1).Width(a.railW - 16)
	})
}

// railRowsCached recomputes the rail only when its inputs changed. The filter
// is one of them: it moves without a snapshot arriving, and a cached list would
// keep the search box's results forever out of date.
func (a *App) railRowsCached() []railRow {
	if a.vm.railCache == nil || a.vm.railStamp != a.stamp || a.vm.railFilter != a.filter {
		a.vm.railCache = a.railRows()
		a.vm.railStamp = a.stamp
		a.vm.railFilter = a.filter
	}
	return a.vm.railCache
}

func railKey(row railRow) any {
	switch row.kind {
	case railChat:
		return "c" + itoa(row.conv.ConversationID)
	case railTask:
		return "t" + itoa(row.task.ID)
	case railGroup:
		if row.section != "" {
			return "s" + row.section
		}
		return "h" + row.label
	case railSearchHit:
		// Two hits can sit in the same chat, so the entry is part of the key.
		return "r" + itoa(row.convID) + "." + itoa(row.entryID)
	}
	return "h" + row.label
}

func (a *App) railRow(c *ui.Context, t theme.Tokens, row railRow) {
	switch row.kind {
	case railGroup:
		if row.section != "" {
			a.railSectionRow(c, t, row)
			return
		}
		ui.Row(c).FillWidth().Height(24).Padding(6, 7, 2, 7).AlignItems(ui.Center).Children(func() {
			ui.Text(c, row.label).FontSize(9.5).FontWeight(600).TextColor(t.MutedFg)
		})
	case railHint:
		ui.Row(c).FillWidth().Height(30).Padding(0, 7).AlignItems(ui.Center).Children(func() {
			ui.Text(c, row.label).FontSize(11).TextColor(t.MutedFg).MaxLines(1)
		})
	case railTask:
		a.railTaskRow(c, t, row)
	case railSearchHit:
		a.railSearchRow(c, t, row)
	default:
		a.railChatRow(c, t, row)
	}
}

func (a *App) railSectionRow(c *ui.Context, t theme.Tokens, row railRow) {
	item := ui.ButtonBase(c).FillWidth().Height(24).Radius(4).Padding(0, 7).
		Gap(4).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		OnClick(func() { a.toggleSection(row.section) })
	item.Children(func() {
		icon := icons.ChevronRight
		if row.open {
			icon = icons.ChevronDown
		}
		ui.Icon(c, icon).Size(11, 11).TextColor(t.MutedFg)
		ui.Text(c, row.label).FontSize(9.5).FontWeight(600).TextColor(t.MutedFg)
		ui.Spacer(c)
		ui.Text(c, itoa(row.count)).FontSize(9.5).TextColor(t.MutedFg).Opacity(0.8)
	})
}

// toggleSection opens or folds a chat section and remembers it across launches.
func (a *App) toggleSection(section string) {
	if a.prefs.Sections == nil {
		a.prefs.Sections = map[string]bool{}
	}
	a.prefs.Sections[section] = a.prefs.sectionClosed(section)
	savePrefs(a.prefs)
	// The rows are cached against the stamp; folding a section changes them
	// without touching what the bridge said, so the cache has to be retired by
	// hand or the chevron turns and the list stays as it was.
	a.stamp++
	// The rows under a heading land below it, and a heading near the bottom of
	// the rail would put them under the window edge: opening it scrolls the
	// heading to the top so what was asked for is what shows up.
	if !a.prefs.Sections[section] {
		for i, row := range a.railRowsCached() {
			if row.kind == railGroup && row.section == section {
				a.vm.railList.ScrollTo(i, ui.Start)
				break
			}
		}
	}
	a.invalidate()
}

func (a *App) railChatRow(c *ui.Context, t theme.Tokens, row railRow) {
	active := row.conv.ConversationID == a.active
	item := ui.ButtonBase(c).FillWidth().Height(30).Radius(6).Padding(0, 7).
		Cursor(ui.CursorPointer).Gap(6).AlignItems(ui.Center)
	if active {
		item = item.Background(t.SidebarAccent)
	}
	// The ⋯ belongs to the chat on screen and to the row under the pointer; its
	// space is kept either way, so a title never jumps as the pointer crosses.
	menuShown := active || item.Hovered()
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
		ui.Box(c).Size(14, 14).Children(func() {
			if menuShown {
				a.railChatMenu(c, t, row.conv, row.at)
			}
		})
	})
	item.OnClick(func() { a.selectConversation(row.conv.ConversationID) })
}

// railChatMenu is the row's ⋯: the two things the rail can write on a chat.
// The menu is built by the window, so the items take the theme's type and
// colors; both choices only queue an event, since a menu build must not write.
func (a *App) railChatMenu(c *ui.Context, t theme.Tokens, conv protocol.Surface, at time.Time) {
	button := ui.ButtonBase(c).Size(14, 14).Radius(3).Justify(ui.Center).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("chatMenu"))
	button.Children(func() {
		ui.Text(c, "⋯").FontSize(11).FontWeight(600).TextColor(t.MutedFg)
	})
	archived := a.autoArchived(conv, at)
	button.Menu(func(m *ui.Menu) {
		if m.Item(i18n.T("rename")).Chosen() {
			a.post(event{kind: evRenameChat, convID: conv.ConversationID})
		}
		label := i18n.T("archive")
		if archived {
			label = i18n.T("unarchive")
		}
		if m.Item(label).Chosen() {
			a.post(event{kind: evArchiveChat, convID: conv.ConversationID, archived: !archived})
		}
	})
}

// railSearchRow is one hit from the bridge: the chat it sits in on the first
// line, the matched text on the second. A press opens that chat; finding the
// entry inside it is the chat's own job.
func (a *App) railSearchRow(c *ui.Context, t theme.Tokens, row railRow) {
	item := ui.ButtonBase(c).FillWidth().Height(38).Radius(6).Padding(3, 7).
		Cursor(ui.CursorPointer).AlignItems(ui.Center)
	item.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, row.label).FontSize(11).FontWeight(500).TextColor(t.Foreground).MaxLines(1)
			ui.Text(c, row.snippet).FontSize(9.5).TextColor(t.MutedFg).MaxLines(1)
		})
	})
	item.OnClick(func() { a.selectConversation(row.convID) })
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
