package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// railCollapsedWidth is the rail folded to its icons, the way Beautiful UI's
// sidebar collapses: 52 there, plus the seam's room here.
const railCollapsedWidth = 56

// railView is the left column, drawn as Beautiful UI's SidebarNav: a workspace
// control over the list, the primary destinations as icon rows, the search
// folded behind its icon, and one full-width button at the foot.
func (a *App) railView(c *ui.Context, t theme.Tokens) {
	// The edge sits over the seam between the two columns, so the pointer finds
	// it before the list underneath does.
	ui.Column(c).Width(a.railCurrentWidth()).FillHeight().Background(t.Canvas).Children(func() {
		edge := ui.Box(c).Absolute().Right(-3).Top(0).Width(6).FillHeight().Cursor(ui.CursorResizeEW)
		if dx, _, held := edge.Dragged(); dx != 0 || a.dragging {
			a.resizeRail(dx, held)
		}
		// The window hides its title bar, so this strip is the title bar: it is
		// what the traffic lights sit over and what a press drags the window by.
		ui.Box(c).Height(titleBar).DragWindow()
		a.sideHeader(c, t)
		a.sideNav(c, t)
		if a.railCollapsed {
			return
		}
		a.sideSearch(c, t)
		a.railList(c, t)
		a.sideFooter(c, t)
	})
}

// railCurrentWidth is what the column is laid out at: the dragged width, or the
// folded one while the rail is collapsed.
func (a *App) railCurrentWidth() float32 {
	if a.railCollapsed {
		return railCollapsedWidth
	}
	return a.railW
}

// toggleRailCollapsed folds the rail to icons and puts it back to the width it
// had, so a collapsed rail is a place the reader returns from.
func (a *App) toggleRailCollapsed() {
	if a.railCollapsed {
		if a.railWide >= railMin {
			a.railW = a.railWide
		}
		a.railCollapsed = false
	} else {
		a.railWide = a.railW
		a.railCollapsed = true
		a.railSearch = false
	}
	a.invalidate()
}

// sideHeader is the workspace control: logo, name, chevron — the row that says
// which workspace this window belongs to. Collapsed, it is the logo alone, and
// pressing it unfolds the rail.
func (a *App) sideHeader(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(40).Padding(0, 8, 10, 8).AlignItems(ui.Start).Gap(4).Children(func() {
		if a.railCollapsed {
			// Full width, centred like a nav row: a 28-point button sat six points
			// left of the icons, so the logo was off the rail's axis.
			open := ui.ButtonBase(c).FillWidth().Height(32).Radius(theme.RadiusControl).Justify(ui.Center).
				AlignItems(ui.Center).Cursor(ui.CursorPointer)
			sideTip(c, t, open, i18n.T("expandRail"))
			if open.Hovered() {
				open = open.Background(t.Hover2)
			}
			open.Children(func() { a.avatarView(c, 20) })
			open.OnClick(a.toggleRailCollapsed)
			return
		}
		ws := ui.ButtonBase(c).Grow(1).Height(32).Radius(theme.RadiusControl).Padding(0, 8).Gap(6).
			AlignItems(ui.Center).Cursor(ui.CursorPointer)
		sideTip(c, t, ws, i18n.T("projects"))
		if ws.Hovered() {
			ws = ws.Background(t.Hover2)
		}
		ws.Children(func() {
			a.avatarView(c, 20)
			ui.Text(c, "PiLot").FontSize(theme.TextTitle).FontWeight(500).TextColor(t.Ink2).MaxLines(1)
			if n := len(a.pending); n > 0 {
				badge := ui.Box(c).Radius(theme.RadiusChip).Padding(1, 6).Background(t.Red)
				badge.Children(func() {
					ui.Text(c, itoa(n)).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Surface)
				})
			}
			ui.Spacer(c)
			ui.Icon(c, icons.ChevronDown).Size(14, 14).TextColor(t.Ink3)
		})
		ws.OnClick(a.toggleProjects)

		fold := ui.ButtonBase(c).Size(32, 32).Radius(theme.RadiusControl).Justify(ui.Center).
			AlignItems(ui.Center).Cursor(ui.CursorPointer)
		sideTip(c, t, fold, i18n.T("collapseRail"))
		if fold.Hovered() {
			fold = fold.Background(t.Hover2)
		}
		fold.Children(func() { ui.Icon(c, icons.Panel).Size(15, 15).TextColor(t.Ink3) })
		fold.OnClick(a.toggleRailCollapsed)
	})
}

// sideNav is the primary navigation: the destinations, not the content. 新会话
// is first because it is what the rail is for on a quiet afternoon; 会话 and
// 任务 are the two lists, and the drawer of projects is a place too.
func (a *App) sideNav(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Gap(1).Padding(0, 8, 0, 8).Children(func() {
		a.sideRow(c, t, icons.Pencil, i18n.T("newChat"), 0, false, a.newChat)
		a.sideRow(c, t, icons.MessagePlus, i18n.T("chats"), len(a.surfaceSnapshot()),
			a.rail == railChats, func() { a.setRail(railChats) })
		a.sideRow(c, t, icons.Checklist, i18n.T("tasks"), len(a.tasks),
			a.rail == railTasks, func() { a.setRail(railTasks) })
		// 项目 opens a drawer rather than switching the list, so it does not take
		// the row highlight: with it lit, the drawer and the section the rail is
		// showing were both marked, and two lit rows say nothing about where you
		// are. The drawer is its own cue; folded, the logo carries it.
		a.sideRow(c, t, icons.Folder, i18n.T("projects"), 0, false, a.toggleProjects)
		enabled := 0
		for _, row := range a.ext.items {
			if row.Enabled {
				enabled++
			}
		}
		a.sideRow(c, t, icons.PlugZap, i18n.T("extensions"), enabled,
			a.rail == railExtensions, func() { a.setRail(railExtensions) })
	})
}

// sideTip shows a tip beside a control in the top strip. The default placement is
// above the control, which is where the window's traffic lights are: a tip there
// covers them and reads as a stray label over the window's own controls.
func sideTip(c *ui.Context, t theme.Tokens, anchor ui.Element, label string) {
	ui.TooltipBase(c, anchor, func(tip ui.Element) {
		tip.AttachTo(anchor, ui.AnchorRight, ui.AnchorLeft).MarginStart(8).
			Padding(4, 8).Radius(theme.RadiusChip).Background(t.TipBg).TextColor(t.TipFg)
		tip.Children(func() {
			ui.Text(c, label).FontSize(theme.TextSmall).FontWeight(500).TextColor(t.TipFg).
				MaxLines(1)
		})
	})
}

// sideRow is one destination, the shape Beautiful UI's RailButton has: a 20px
// icon box, the label at 14 medium, and a count at the right edge.
func (a *App) sideRow(c *ui.Context, t theme.Tokens, icon *ui.SVG, label string, count int, active bool, onClick func()) {
	btn := ui.ButtonBase(c).FillWidth().Height(32).Radius(theme.RadiusControl).Padding(0, 8).
		Gap(6).AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(label)
	if a.railCollapsed {
		btn = btn.Justify(ui.Center).Padding(0, 4)
	}
	if active || btn.Hovered() {
		btn = btn.Background(t.Hover2)
	}
	btn.Children(func() {
		color := t.Ink2
		if active {
			color = t.Ink
		}
		ui.Box(c).Size(20, 20).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icon).Size(16, 16).TextColor(color)
		})
		if a.railCollapsed {
			return
		}
		ui.Text(c, label).FontSize(theme.TextTitle).FontWeight(500).TextColor(color).
			MaxLines(1).Grow(1)
		if count > 0 {
			ui.Text(c, itoa(count)).FontSize(theme.TextRow).FontWeight(500).TextColor(t.Ink3)
		}
	})
	btn.OnClick(onClick)
}

// setRail switches which list the rail shows. The list's scroll and selection
// belong to the tab that had them, so they are dropped when it changes.
func (a *App) setRail(section railSection) {
	if a.rail == section {
		return
	}
	a.rail = section
	if section == railExtensions {
		a.loadExtensions()
	}
	a.stamp++
	a.vm.railList.Selected = nil
	a.invalidate()
}

// sideSearch is the list header: the list's own name with the search folded
// behind its icon, exactly as Beautiful UI keeps "Chats" until you ask for the
// field. Expanded, the field takes the whole row.
func (a *App) sideSearch(c *ui.Context, t theme.Tokens) {
	name := i18n.T("chats")
	switch a.rail {
	case railTasks:
		name = i18n.T("tasks")
	case railExtensions:
		name = i18n.T("extensions")
	}
	ui.Row(c).FillWidth().Height(32).Padding(0, 8, 8, 8).AlignItems(ui.Center).Gap(4).Children(func() {
		if a.railSearch {
			ui.Row(c).Grow(1).Height(32).Radius(theme.RadiusControl).Padding(0, 8).Gap(6).
				Background(t.Field).Border(1, t.LineSoft).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.Search).Size(14, 14).TextColor(t.Ink3)
				ui.TextInputBase(c, &a.filter).FontSize(theme.TextBody).
					Placeholder(i18n.T("filter")).Height(22).Grow(1)
				if a.filter != "" {
					clear := ui.ButtonBase(c).Size(20, 20).Radius(theme.RadiusChip).
						Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					clear.Children(func() { ui.Icon(c, icons.X).Size(12, 12).TextColor(t.Ink3) })
					clear.OnClick(func() {
						// The cross closes the search, as it does in the design:
						// it is the same control that folded the field out.
						a.filter = ""
						a.railSearch = false
						a.invalidate()
					})
				}
			})
			return
		}
		ui.Text(c, name).FontSize(theme.TextRow).FontWeight(500).TextColor(t.Ink3).
			MaxLines(1).Grow(1).Padding(7, 0)
		// The calendar belongs to the task tab: that is the tab whose rows are
		// the work, and the work is what the calendar counts.
		if a.rail == railTasks {
			a.usageButton(c, t)
		}
		find := ui.ButtonBase(c).Size(28, 28).Radius(theme.RadiusControl).Justify(ui.Center).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("filter"))
		if find.Hovered() {
			find = find.Background(t.Hover2)
		}
		find.Children(func() { ui.Icon(c, icons.Search).Size(14, 14).TextColor(t.Ink3) })
		find.OnClick(func() {
			a.railSearch = true
			a.invalidate()
		})
	})
	// What was typed is local filtering first; the bridge is asked a beat
	// later, so a word typed at speed is one search rather than eight.
	a.watchSearch()
}

// sideFooter is the foot of the sidebar: a hairline and one full-width button,
// the way Beautiful UI ends its nav. The button is 设置 because that panel is
// where this window's own choices live.
func (a *App) sideFooter(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Padding(8, 8, 10, 8).Gap(8).Children(func() {
		ui.Box(c).FillWidth().Height(1).Background(t.Line)
		btn := ui.ButtonBase(c).FillWidth().Height(32).Radius(theme.RadiusControl).Gap(6).
			Justify(ui.Center).AlignItems(ui.Center).Background(t.Hover2).Cursor(ui.CursorPointer)
		if btn.Hovered() {
			btn = btn.Background(t.Hover2).Border(1, t.LineStrong)
		}
		btn.Children(func() {
			ui.Icon(c, icons.Gear).Size(14, 14).TextColor(t.Ink)
			ui.Text(c, i18n.T("settings")).FontSize(theme.TextRow).FontWeight(500).TextColor(t.Ink)
		})
		btn.OnClick(a.toggleSettings)
	})
}

// projectsButton opens the drawer of repositories a chat can live in. It sits
// in the rail because a project is not a property of the chat that happens to be
// open: it is where the next chat could go.
func (a *App) projectsButton(c *ui.Context, t theme.Tokens) {
	color := t.MutedFg
	btn := ui.ButtonBase(c).Size(22, 22).Radius(theme.RadiusChip).Justify(ui.Center).AlignItems(ui.Center).
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
	btn := ui.ButtonBase(c).Size(22, 22).Radius(theme.RadiusChip).Justify(ui.Center).AlignItems(ui.Center).
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
	ui.Column(c).FillWidth().Grow(1).Padding(0, 8, 10, 8).Gap(6).Children(func() {
		if a.rail == railExtensions {
			a.extensionsBody(c, t)
			return
		}
		if a.rail == railTasks {
			a.taskFilterRow(c, t)
			a.taskTableHead(c, t)
		}
		ui.List(c, &a.vm.railList, len(rows), func(row int) {
			a.railRow(c, t, rows[row])
		}).Grow(1).Width(a.railW - 16)
		if a.rail == railTasks {
			a.taskTableFoot(c, t)
		}
	})
}

// railRowsCached recomputes the rail only when its inputs changed. The filter
// is one of them: it moves without a snapshot arriving, and a cached list would
// keep the search box's results forever out of date.
func (a *App) railRowsCached() []railRow {
	if a.vm.railCache == nil || a.vm.railStamp != a.stamp || a.vm.railFilter != a.filter ||
		a.vm.railState != a.taskFilter {
		a.vm.railCache = a.railRows()
		a.vm.railStamp = a.stamp
		a.vm.railFilter = a.filter
		a.vm.railState = a.taskFilter
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
		ui.Row(c).FillWidth().Height(28).Padding(4, 8, 2, 8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, row.label).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Ink3)
			if row.count > 0 {
				// The count is what makes the group a table's heading rather
				// than a label: it says how many rows are under it.
				ui.Spacer(c)
				ui.Text(c, itoa(row.count)).FontSize(theme.TextMicro).TextColor(t.Ink3).Opacity(0.8)
			}
		})
	case railHint:
		ui.Row(c).FillWidth().Height(32).Padding(0, 8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, row.label).FontSize(theme.TextSmall).TextColor(t.Ink3).MaxLines(1)
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
	item := ui.ButtonBase(c).FillWidth().Height(28).Radius(theme.RadiusChip).Padding(0, 8).
		Gap(4).AlignItems(ui.Center).Cursor(ui.CursorPointer).
		OnClick(func() { a.toggleSection(row.section) })
	item.Children(func() {
		icon := icons.ChevronRight
		if row.open {
			icon = icons.ChevronDown
		}
		ui.Icon(c, icon).Size(12, 12).TextColor(t.Ink3)
		ui.Text(c, row.label).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Ink3)
		ui.Spacer(c)
		ui.Text(c, itoa(row.count)).FontSize(theme.TextMicro).TextColor(t.Ink3).Opacity(0.8)
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
	// The open chat is a floating row, not a tinted band: a surface plus a
	// hairline, the same way Beautiful UI lifts its selected record.
	item := ui.ButtonBase(c).FillWidth().Height(32).Radius(theme.RadiusControl).Padding(0, 8).
		Cursor(ui.CursorPointer).Gap(8).AlignItems(ui.Center)
	if active {
		item = item.Background(t.Surface).Border(1, t.Line)
	}
	// A row that is not open reads dimmer, so the open one carries the eye.
	titleColor := t.Ink2
	if active {
		titleColor = t.Ink
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
		ui.Text(c, title).Grow(1).Shrink(1).MinWidth(0).FontSize(theme.TextRow).
			FontWeight(500).TextColor(titleColor).MaxLines(1)
		// The queued count is not repeated here: the chat's own status strip
		// spells out what is waiting, and a rail row has one line to give.
		if row.meta != "" {
			ui.Text(c, row.meta).FontSize(theme.TextMicro).TextColor(t.Ink3)
		}
		if when := relTime(a.now, row.at); when != "" {
			ui.Text(c, when).FontSize(theme.TextMicro).TextColor(t.Ink3)
		}
		// One status per row, where the unread dot lives: a spinner while a turn
		// is in flight, the dot once it has finished and nobody has read it.
		// The bridge's health names every chat at work; the open chat is also
		// known here from its own live document, which covers a bridge that
		// predates the field.
		switch {
		case a.runningConvs[row.conv.ConversationID] ||
			(row.conv.ConversationID == a.active && a.running()):
			// The library's inline spinner is a 12px ring in ink, not a warning
			// glyph: work in flight is a state, and an amber ring in a list reads
			// as a problem.
			ui.Spinner(c).Size(12, 12).TextColor(t.Ink2).Label(i18n.T("runningNow"))
		case row.unread:
			ui.Box(c).Size(5, 5).Radius(3).Background(t.Brand)
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
		ui.Text(c, "⋯").FontSize(theme.TextSmall).FontWeight(600).TextColor(t.MutedFg)
	})
	archived := a.autoArchived(conv, at)
	pinned := a.prefs.pinnedChat(conv.ConversationID)
	button.Menu(func(m *ui.Menu) {
		pinLabel := i18n.T("pin")
		if pinned {
			pinLabel = i18n.T("unpin")
		}
		if m.Item(pinLabel).Chosen() {
			a.post(event{kind: evPinChat, convID: conv.ConversationID, archived: !pinned})
		}
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
	item := ui.ButtonBase(c).FillWidth().Height(42).Radius(theme.RadiusControl).Padding(4, 8).
		Cursor(ui.CursorPointer).AlignItems(ui.Center)
	item.Children(func() {
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, row.label).FontSize(theme.TextRow).FontWeight(500).TextColor(t.Ink).MaxLines(1)
			ui.Text(c, row.snippet).FontSize(theme.TextSmall).TextColor(t.Ink3).MaxLines(1)
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
