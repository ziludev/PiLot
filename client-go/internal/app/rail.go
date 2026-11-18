package app

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/brand"
	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/motion"
	"pitta/internal/protocol"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// railRowHeight is one chat's box: a single line of 11.5 px text, with the room
// the rail's rows have.
const railRowHeight float32 = 28

// othersOpen is the fold state of the "other projects" group. It lives on the
// App, not in a package variable, so that it survives a restart through prefs
// the way the earlier web client's localStorage did.
// The rail has no per-window field for it, and a value rebuilt every frame
// would fold the block again the moment the pointer left its header.

// rail is the left pane: the app's name and project, the way into the chats,
// the chats themselves, the tasks running behind them, and the buttons that
// drive the agent.
func (a *App) rail(c *ui.Context) {
	t := theme.Current(c)
	rows := a.railRows()
	a.rows = rows
	ui.Column(c).Fill().Background(t.Sidebar).Border(1, t.SidebarBorder).Children(func() {
		a.railHeader(c, t)
		if a.conn == ConnDown {
			a.railDown(c, t)
		}
		a.railApproval(c, t)
		a.projectBar(c, t)
		a.railSearch(c, t)
		a.railFilters(c, t)
		a.railFeishu(c, t)
		a.railList(c, t, rows)
		a.railTasks(c, t)
		a.railFooter(c, t)
	})
}

// railHeader names the app and the project it works in, and holds the two
// switches the rail owns: the language and the theme.
// railHeaderLabel names the header row for the tests that measure it.
const railHeaderLabel = "rail-header"

func (a *App) railHeader(c *ui.Context, t theme.Tokens) {
	// px-3 py-2: avatar, then the name over the status. The theme and language
	// switches live in the settings panel with the rest of the preferences; the
	// header keeps the two things that say what this window is.
	// The window draws its own title bar (mygo.WindowOptions.TitleBarStyle), so
	// this row starts below the traffic lights rather than to their right: a
	// sidebar's header lines up with its rows, and a name pushed past the
	// controls floats between two gaps, which reads as a centred title.
	bar := c.TitleBar()
	// Padding takes (top, right, bottom, left) here: the title bar's room is
	// added to the top only, so the row keeps its 12 px sides and the name
	// column keeps the width the split gave it (see
	// TestRailHeaderStaysInTheCorner).
	ui.Row(c).FillWidth().Padding(8+bar.Bottom, 12, 8, 12).DragWindow().Gap(8).AlignItems(ui.Center).Label(railHeaderLabel).
		BorderWidth(0, 0, 1, 0).BorderColor(t.SidebarBorder).Children(func() {
		mood, delegated := a.markLook()
		brand.Draw(c, 24, mood, brand.Ink(a.markInk(t, delegated)))
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, i18n.T("appName")).FontSize(14).FontWeight(600).
				TextColor(t.SidebarFg).MaxLines(1)
			status := ui.Text(c, a.railStatus()).FontSize(11).MaxLines(1)
			if a.busy {
				status.TextColor(t.Primary)
			} else {
				status.TextColor(t.MutedFg)
			}
		})
	})
}

// railDown is the rail's one red line: the socket to the agent is down, so a
// list showing nothing is the socket's fault and not the chats'.
func (a *App) railDown(c *ui.Context, t theme.Tokens) {
	hint := i18n.T("connecting")
	if a.retry > 0 {
		hint = i18n.T("bridgeRetrying") + " " + itoa(a.retry)
	}
	ui.Column(c).FillWidth().Padding(4, 10).Gap(1).Background(t.Destructive).Children(func() {
		ui.Text(c, i18n.T("disconnected")+" · "+hint).FontSize(10).FontWeight(600).
			TextColor(t.Sidebar).MaxLines(1)
		if a.downReason != "" {
			ui.Text(c, a.downReason).FontSize(9).TextColor(t.Sidebar).MaxLines(2)
		}
	})
}

// railFeishu warns that the Feishu transport has been down for a while, which
// silences the chats that arrive through it (the rail view shows it past a minute).
func (a *App) railFeishu(c *ui.Context, t theme.Tokens) {
	if a.feishu == nil || a.feishu.Ready || a.feishu.DownMs <= 60_000 {
		return
	}
	ui.Row(c).FillWidth().Padding(6, 10).Gap(4).AlignItems(ui.Center).
		Background(ui.RGBA(244, 63, 94, 0.10)).Border(1, ui.RGBA(244, 63, 94, 0.40)).Radius(6).
		Children(func() {
			ui.Icon(c, icons.PlugZap).Size(11, 11).TextColor(t.Destructive)
			ui.Text(c, i18n.T("feishuDown")+" "+itoa(int(a.feishu.DownMs/60_000))+"m").
				FontSize(10).TextColor(t.Destructive).Grow(1).MaxLines(1)
		})
}

// railApprovalLabel names the chip for the test that clicks it.
const railApprovalLabel = "rail-approval"

// railApproval is the standing way into a call that waits. The approval bar
// sits in the composer, so a window showing another chat would hide a call that
// is holding the agent still, and the tray's count would point at nothing.
// It is under the header rather than in it: the header drags the window, and a
// click meant for the chip would move the window instead.
func (a *App) railApproval(c *ui.Context, t theme.Tokens) {
	waiting := a.trayApprovals()
	if len(waiting) == 0 {
		return
	}
	target := 0
	for _, approval := range waiting {
		if approval.ConversationID != 0 {
			target = approval.ConversationID
			break
		}
	}
	ui.Row(c).FillWidth().Padding(4, 10, 2, 10).Children(func() {
		chip := ui.Row(c).Gap(5).AlignItems(ui.Center).Padding(2, 7).Radius(6).
			Background(ui.RGBA(245, 158, 11, 0.12)).Border(1, ui.RGBA(245, 158, 11, 0.45)).
			Label(railApprovalLabel).Transition(motion.Hover())
		chip.Cursor(ui.CursorPointer)
		if chip.Hovered() {
			chip.Background(ui.RGBA(245, 158, 11, 0.22))
		}
		chip.OnClick(func() {
			if target == 0 {
				return
			}
			// The bar lists every pending call, so opening the chat that owns
			// this one is enough to put the buttons on screen.
			a.apply(func() { a.selectConversation(target) })
		})
		chip.Children(func() {
			ui.Icon(c, icons.AlertTriangle).Size(10, 10).TextColor(t.Warning)
			ui.Text(c, itoa(len(waiting))+" "+i18n.T("needsApproval")).
				FontSize(10).FontWeight(600).TextColor(t.Warning).MaxLines(1)
		})
	})
}

// railSearch filters the list as the user types. The rule lives in
// sortedSurfaces, which every view of the list goes through.
func (a *App) railSearch(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Padding(6, 10, 4, 10).Children(func() {
		ui.TextInput(c, &a.query).Fill().FontSize(11.5).PaddingY(4).PaddingX(8).
			Placeholder(i18n.T("searchShort")).
			OnSubmit(func() {
				a.apply(func() {
					a.filter = a.query
					a.invalidate()
				})
				a.runSearch()
			})
	})
	a.searchHitsPanel(c, t)
}

// searchHitsPanel lists what the bridge found inside transcripts. A hit opens
// the conversation and marks the turn, the same as the rail's search field.
func (a *App) searchHitsPanel(c *ui.Context, t theme.Tokens) {
	if a.searchPending {
		ui.Row(c).FillWidth().Padding(0, 12, 4, 12).Children(func() {
			ui.Text(c, "…").FontSize(10).TextColor(t.MutedFg)
		})
		return
	}
	if a.searchHits == nil {
		return
	}
	if len(a.searchHits) == 0 {
		ui.Row(c).FillWidth().Padding(0, 12, 4, 12).Children(func() {
			ui.Text(c, i18n.T("noHits")).FontSize(10).TextColor(t.MutedFg).MaxLines(1)
		})
		return
	}
	ui.Column(c).FillWidth().Padding(0, 10, 4, 10).Children(func() {
		// A few hits hug their content; a long list stops at 140 and scrolls,
		// rather than leaving dead rail between the last hit and the views.
		ui.Scroll(c).MaxHeight(searchHitsHeight).Label(searchHitsLabel).Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for _, hit := range a.searchHits {
					hit := hit
					ui.Column(c).FillWidth().Padding(3, 6).Gap(1).
						Background(t.Card).Border(1, t.Border).Radius(4).
						OnClick(func() { a.jumpToHit(hit) }).
						Children(func() {
							ui.Text(c, "#"+itoa(hit.ConversationID)+" · "+strings.TrimPrefix(hit.Kind, "pi.")).
								FontSize(9).TextColor(t.MutedFg).MaxLines(1)
							ui.Text(c, hit.Snippet).FontSize(10).TextColor(t.Foreground).MaxLines(2)
						})
				}
			})
		})
	})
}

// searchHitsHeight is the room a long list of search hits may take; a short
// list takes only what it needs.
const searchHitsHeight = 140

// searchHitsLabel names the box the hits scroll in, which is how the tests that
// measure it find it.
const searchHitsLabel = "search-hits"

// railFilters is the line between the search field and the list: what the list
// shows, how many chats that is, and whether it keeps the archived ones.
//
// The views are laid flat rather than behind a select. A closed dropdown hides
// the very thing the row is asked for — how many chats are running, how many
// answers arrived unseen — and the six views fold onto two lines at the rail's
// narrowest width, so there is nothing to open.
func (a *App) railFilters(c *ui.Context, t theme.Tokens) {
	picked := railFilterIndex(a.prefs.SurfaceFilter)
	counts := a.railFilterCounts()
	ui.Row(c).FillWidth().Padding(6, 10, 0, 10).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, strings.ToUpper(i18n.T("sessions"))).FontSize(10).FontWeight(600).
			TextColor(t.MutedFg).MaxLines(1)
		ui.Row(c).Grow(1).Children(func() {})
		a.railArchiveToggle(c, t)
		railIconAction(c, i18n.T("newChat"), icons.Plus, func() { go a.newConversation() })
	})
	ui.Row(c).FillWidth().Wrap().Padding(4, 10, 4, 10).Gap(4).Children(func() {
		for index, key := range railFilterKeys {
			index := index
			label := i18n.T(key)
			if railFilterCounted(railFilters[index]) {
				label += " " + itoa(counts[index])
			}
			chip(c, t, label, index == picked, func() {
				a.apply(func() {
					a.prefs.SurfaceFilter = railFilters[index]
					a.savePrefsLater()
					a.invalidate()
				})
			})
		}
	})
}

// railArchiveToggle folds the archived chats in and out. It is drawn always,
// tinted while they show, because a control that appears only under the pointer
// is a control the user has to find.
func (a *App) railArchiveToggle(c *ui.Context, t theme.Tokens) {
	button := ui.ButtonBase(c).Size(22, 22).Radius(5).Cursor(ui.CursorPointer)
	button.Label(i18n.T("archived"))
	color := t.MutedFg
	switch {
	case a.showArchived:
		button.Background(t.SidebarAccent)
		color = t.SidebarFg
	case button.Hovered():
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() {
		ui.Icon(c, icons.Archive).Size(11, 11).TextColor(color)
	})
	button.OnClick(func() {
		a.apply(func() {
			a.showArchived = !a.showArchived
			a.invalidate()
		})
	})
}

// railList draws the chats. A virtual list keeps a thousand chats as cheap as
// the twenty on screen.
func (a *App) railList(c *ui.Context, t theme.Tokens, rows []railRow) {
	if len(rows) == 0 {
		a.railEmpty(c, t)
		return
	}
	a.railListState.Key = func(row int) any {
		if row < 0 || row >= len(rows) {
			return row
		}
		if rows[row].kind != railChatRow {
			return "header"
		}
		return rows[row].surface.ConversationID
	}
	a.railListState.Reorder = func(moved []int, to int) { a.reorderChats(moved, to) }
	ui.List(c, &a.railListState, len(rows), func(i int) {
		a.railRow(c, t, rows[i])
	}).Grow(1).FillWidth()
}

// railEmpty says why the list is blank: a first run, a filter that matches
// nothing, or an agent that is not answering.
func (a *App) railEmpty(c *ui.Context, t theme.Tokens) {
	title, hint := i18n.T("noSessions"), i18n.T("noSessionsHint")
	switch {
	// A filter that matches nothing is the user's own doing: saying the agent
	// is connecting would send them looking for a fault that is not there.
	case a.query != "" || railFilterIndex(a.prefs.SurfaceFilter) != 0:
		title, hint = i18n.T("noHits"), a.query
	case a.conn == ConnDown:
		title = i18n.T("bridgeDownTitle")
		hint = i18n.T("bridgeRetrying") + " " + itoa(a.retry)
	case a.conn == ConnConnecting:
		title, hint = i18n.T("connecting"), ""
		if a.retry > 0 {
			hint = i18n.T("bridgeRetrying") + " " + itoa(a.retry)
		}
	}
	ui.Column(c).Grow(1).FillWidth().Padding(16, 12).Gap(3).Transition(motion.FadeIn()).Children(func() {
		ui.Text(c, title).FontSize(11).TextColor(t.MutedFg).Wrap()
		if hint != "" {
			ui.Text(c, hint).FontSize(10).TextColor(t.MutedFg).Wrap()
		}
	})
}

// railRow draws one line of the list: a chat, or the header that folds another
// project's chats away. A chat is a single line — icon, title, cost — with its
// #id, menu and pin appearing over the right end under the pointer, as
// the rail's SessionRow does.
func (a *App) railRow(c *ui.Context, t theme.Tokens, row railRow) {
	if row.kind == railOtherProjects {
		a.railOthersHeader(c, t, row.count)
		return
	}
	s := row.surface
	selected := s.ConversationID == a.current
	pinned := a.isPinned(s.ConversationID)
	// Keyed by chat, not by row: the list recycles rows, and a lift that
	// belonged to one chat would otherwise stay on whichever row took its
	// place after a scroll.
	line := ui.Row(c).FillWidth().Height(railRowHeight).PaddingX(6).Gap(4).Radius(6).
		Key(s.ConversationID).Cursor(ui.CursorPointer)
	if row.topic {
		// A fork sits under the chat it forked from, as the rail indents
		// it, so the nesting reads without opening anything.
		line.PaddingStart(14)
	}
	hovered := line.Hovered()
	switch {
	case selected:
		line.Background(t.SidebarAccent)
	case hovered:
		line.Background(c.Theme().SurfaceHover)
	}
	// The hover lift of the rail view: the row rises 2 px and casts a soft shadow,
	// both over 300 ms, so a row under the pointer reads as picked up.
	target := float32(0)
	if hovered {
		target = -motion.HoverRise
		line.Shadow(0, 6, 14, -6, ui.RGBA(0, 0, 0, 0.45))
	}
	// The lift moves what the row shows, never its box: a box that rose under a
	// still pointer would give the hover to the row below at the seam, which
	// would give it back, frame after frame, and the two would flicker.
	lift := line.AnimateTo("lift", target, motion.Lift)
	line.OnClick(func() {
		// A chat already open in a window of its own comes forward instead of
		// being loaded here a second time.
		if focusOwnWindow(a, s.ConversationID) {
			return
		}
		a.selectConversation(s.ConversationID)
	})
	// A right-click gives the row what the hover cluster offers, plus the
	// destructive actions that a native menu can carry without a dialog.
	line.ContextMenu(func(m *ui.Menu) {
		if m.Item(i18n.T("pinned")).Checked(a.isPinned(s.ConversationID)).Chosen() {
			a.apply(func() { a.togglePin(s.ConversationID) })
		}
		if m.Item(i18n.T("rename")).Chosen() {
			a.apply(func() { a.openChatMenuFor(s.ConversationID) })
		}
		// A second window on one chat is what a native client adds, so the item
		// is absent where no window factory was handed in, as in the tests.
		if windowOpener != nil && m.Item(newWindowLabel).Chosen() {
			a.openInNewWindow(s.ConversationID)
		}
		m.Separator()
		if m.Item(i18n.T("archive")).Chosen() {
			a.apply(func() {
				a.menu.id = s.ConversationID
				a.archiveChat(true)
			})
		}
		if s.Archived && m.Item(i18n.T("unarchive")).Chosen() {
			a.apply(func() {
				a.menu.id = s.ConversationID
				a.archiveChat(false)
			})
		}
		if m.Item(i18n.T("cleanupWorktree")).Danger().Chosen() {
			a.apply(func() {
				a.menu.id = s.ConversationID
				a.removeWorktree()
			})
		}
	})
	line.Children(func() {
		// Every part of the row rises with the lift, so the row reads as picked
		// up while the pointer keeps the box that found it.
		rise := func(e ui.Element) ui.Element {
			if lift != 0 {
				e.Translate(0, lift)
			}
			return e
		}
		// The bar is drawn for every row so the names stay in one column.
		bar := rise(ui.Box(c).Width(2).Height(14).Radius(1).AlignSelf(ui.Center))
		if selected {
			bar.Background(t.Primary)
		}
		switch {
		case row.topic:
			rise(ui.Icon(c, icons.CornerDownRight).Size(10, 10).TextColor(t.MutedFg))
		case surfaceKind(s) == "client":
			rise(ui.Icon(c, icons.Monitor).Size(10, 10).TextColor(t.Primary))
		case surfaceKind(s) == "feishu":
			rise(ui.Icon(c, icons.MessagesSquare).Size(10, 10).TextColor(t.MutedFg))
		}
		title := rise(ui.Text(c, surfaceTitle(s)).FontSize(11.5).TextColor(t.SidebarFg).
			Grow(1).MaxLines(1))
		if selected {
			title.FontWeight(500)
		}
		if s.Archived {
			title.Strikethrough().TextColor(t.MutedFg)
		}
		drawRailMark(c, t, a.railMarkFor(s.ConversationID, selected), rise)
		if s.Cost >= 0.005 {
			// Under half a cent the number is noise, as in the rail view.
			rise(ui.Text(c, railCost(s.Cost)).FontSize(10).TextColor(t.MutedFg).MaxLines(1))
		}
		pinTint := t.MutedFg
		if pinned {
			pinTint = t.Primary
		}
		// A pinned row keeps its pin in the flow; the rest of the cluster is an
		// overlay, so the title keeps every pixel it has at rest instead of
		// shortening under the pointer.
		if pinned {
			rise(railRowButton(c, icons.Pin, i18n.T("pinned"), pinTint, func() {
				a.togglePin(s.ConversationID)
			}))
		}
		if hovered {
			a.railHoverCluster(c, t, s, pinned, pinTint, rise)
		}
	})
}

// railHoverCluster draws what a chat's row offers under the pointer: its number,
// the rename that opens the chat menu, and the pin. It is placed over the row's
// right end rather than laid out in it, so hovering cannot shorten the title.
func (a *App) railHoverCluster(c *ui.Context, t theme.Tokens, s protocol.Surface, pinned bool, pinTint ui.Color, rise func(ui.Element) ui.Element) {
	// The cluster paints over the row's own numbers, so it carries the row's
	// hovered colour rather than letting them show through it.
	cluster := ui.Row(c).Absolute().Right(2).Top(2).Gap(2).Padding(2).Radius(4).
		AlignItems(ui.Center).Background(c.Theme().SurfaceHover).Label(railHoverLabel)
	rise(cluster)
	cluster.Children(func() {
		ui.Text(c, "#"+itoa(s.ConversationID)).FontSize(9.5).TextColor(t.MutedFg).MaxLines(1)
		railRowButton(c, icons.MoreHorizontal, i18n.T("rename"), t.MutedFg, func() {
			a.openChatMenuFor(s.ConversationID)
		})
		railRowButton(c, icons.Pin, i18n.T("pinned"), pinTint, func() {
			a.togglePin(s.ConversationID)
		})
	})
}

// railHoverLabel names the cluster, which is how the tests that measure the
// title under a hover find it.
const railHoverLabel = "rail-hover"

// railRowButton is one of the small buttons a chat's row keeps on its right: the
// pin, and the archive that puts the chat away.
func railRowButton(c *ui.Context, icon *ui.SVG, label string, tint ui.Color, onClick func()) ui.Element {
	button := ui.ButtonBase(c).Size(20, 20).Radius(4).Cursor(ui.CursorPointer)
	button.Label(label)
	button.Tooltip(label)
	if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() {
		ui.Icon(c, icon).Size(10, 10).TextColor(tint)
	})
	button.OnClick(onClick)
	return button
}

// railOthersHeader folds the chats of the projects this rail is not scoped to
// away or out. They stay one click from the rail without crowding it.
func (a *App) railOthersHeader(c *ui.Context, t theme.Tokens, count int) {
	button := ui.ButtonBase(c).FillWidth().Height(26).PaddingX(6).Gap(5).
		AlignItems(ui.Center).Cursor(ui.CursorPointer)
	button.Label(i18n.T("otherProjects"))
	button.Tooltip(i18n.T("otherProjects"))
	if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() {
		chevron := icons.ChevronRight
		if a.othersOpen {
			chevron = icons.ChevronDown
		}
		ui.Icon(c, chevron).Size(10, 10).TextColor(t.MutedFg)
		ui.Text(c, i18n.T("otherProjects")).FontSize(10).FontWeight(600).
			TextColor(t.MutedFg).Grow(1).MaxLines(1)
		ui.Text(c, itoa(count)).FontSize(9).TextColor(t.MutedFg)
	})
	button.OnClick(func() {
		a.othersOpen = !a.othersOpen
		a.prefs.OthersCollapsed = !a.othersOpen
		a.savePrefsLater()
		a.invalidate()
	})
}

// railTasks lists the tasks the owner started, above the rail's buttons. It
// stops at 40 points: a rail shows what is running, not the whole queue.
func (a *App) railTasks(c *ui.Context, t theme.Tokens) {
	tasks := railLiveTasks(a.tasks)
	if len(tasks) == 0 {
		return
	}
	ui.Divider(c)
	ui.Row(c).FillWidth().Padding(5, 10).Children(func() {
		ui.Text(c, i18n.T("running")).FontSize(10).FontWeight(600).TextColor(t.MutedFg)
	})
	ui.Scroll(c).FillWidth().Height(40).Padding(0, 6).Children(func() {
		ui.Column(c).FillWidth().Children(func() {
			for _, task := range tasks {
				task := task
				ui.Row(c).FillWidth().Height(18).PaddingX(4).Gap(5).
					AlignItems(ui.Center).OnClick(func() { a.openTask(task.ID) }).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).AlignSelf(ui.Center).
						Background(railTaskColor(t, task.State))
					ui.Text(c, railTaskTitle(task)).FontSize(10).TextColor(t.SidebarFg).
						Grow(1).MaxLines(1)
					ui.Text(c, railTaskState(task.State)).FontSize(9).TextColor(t.MutedFg)
				})
			}
		})
	})
}

// railFooter is the rail's last line: the settings panel's button, the log, and
// where the agent's home is.
func (a *App) railFooter(c *ui.Context, t theme.Tokens) {
	// One row, as the rail's footer: the log, then the workspace path at the
	// right end. Restart and backup act on the daemon rather than on a chat, so
	// they live in the settings panel.
	ui.Row(c).FillWidth().Padding(6, 10).Gap(2).AlignItems(ui.Center).
		BorderWidth(1, 0, 0, 0).BorderColor(t.SidebarBorder).Children(func() {
		// Settings sits beside the log rather than only on ⌘, : a preference
		// behind a shortcut nobody was told about is a preference nobody finds.
		railIconAction(c, settingsTitle, icons.Settings, func() {
			a.apply(func() { a.settingsOpen = true; a.invalidate() })
		})
		railIconAction(c, i18n.T("logs"), icons.FileIcon, func() { a.apply(func() { a.toggleLogs(); a.invalidate() }) })
		ui.Row(c).Grow(1).Children(func() {})
		a.healthChip(c, t)
		ui.Row(c).Width(5).Children(func() {})
		ui.Text(c, shortPath(a.cfg.Home)).FontSize(9.5).TextColor(t.MutedFg).
			MaxLines(1).TruncateMiddle()
	})
}

// healthChip is the bridge's own state, beside the workspace path.
//
// The bridge reconnects on its own, so a dead agent used to look exactly like
// an idle one; the dot and the queue are what tell them apart, and the tooltip
// carries the pid and version needed to tell which bridge is answering.
func (a *App) healthChip(c *ui.Context, t theme.Tokens) {
	if a.health == nil && a.healthErr == nil {
		return // nothing read yet: a dot about nothing is noise
	}
	// The chip keeps its width; the workspace path is the one that gives way.
	chip := ui.Row(c).Gap(3).AlignItems(ui.Center).Padding(0, 3).Radius(3).Shrink(0)
	if a.healthErr != nil || a.health == nil {
		chip.Tooltip(a.healthErrText())
		chip.Children(func() {
			ui.Text(c, "●").FontSize(8).TextColor(t.Destructive)
			ui.Text(c, i18n.T("agentUnreachable")).FontSize(9.5).TextColor(t.Destructive).MaxLines(1)
		})
		return
	}
	health := a.health
	chip.Tooltip(fmt.Sprintf("pid %d · v%s", health.PID, health.Version))
	chip.Children(func() {
		ui.Text(c, "●").FontSize(8).TextColor(t.Primary)
		ui.Text(c, uptime(health)+" · "+i18n.T("queueDepth")+" "+itoa(health.QueueDepth)).
			FontSize(9.5).TextColor(t.MutedFg).MaxLines(1)
	})
}

func (a *App) healthErrText() string {
	if a.healthErr == nil {
		return i18n.T("agentUnreachable")
	}
	return a.healthErr.Error()
}

// uptime is worked out from the bridge's start time on every frame, so a window
// left open overnight still counts; a bridge that restarted drops back to zero.
func uptime(health *protocol.Health) string {
	started, err := time.Parse(time.RFC3339, health.StartedAt)
	if err != nil {
		return turns.Clock(health.UptimeMS)
	}
	elapsed := time.Since(started).Milliseconds()
	if elapsed < int64(time.Hour/time.Millisecond) {
		return turns.Clock(elapsed)
	}
	hours := elapsed / int64(time.Hour/time.Millisecond)
	minutes := (elapsed % int64(time.Hour/time.Millisecond)) / int64(time.Minute/time.Millisecond)
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

// railAction is one of the footer's ghost buttons: an icon and the name of what
// it does.
func railAction(c *ui.Context, t theme.Tokens, label string, icon *ui.SVG, onClick func()) {
	button := ui.ButtonBase(c).Height(24).PaddingX(5).Gap(4).Radius(5).
		Cursor(ui.CursorPointer)
	button.Label(label)
	button.Tooltip(label)
	if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() {
		ui.Icon(c, icon).Size(11, 11).TextColor(t.MutedFg)
		ui.Text(c, label).FontSize(10).TextColor(t.SidebarFg).MaxLines(1)
	})
	button.OnClick(onClick)
}

// railIconAction is the footer's icon-only button: the label lives in the
// tooltip, because four labelled buttons do not fit the rail's width.
func railIconAction(c *ui.Context, label string, icon *ui.SVG, onClick func()) {
	button := ui.ButtonBase(c).Height(24).Width(24).Radius(5).Cursor(ui.CursorPointer)
	button.Label(label)
	button.Tooltip(label)
	if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() {
		ui.Icon(c, icon).Size(11, 11).TextColor(c.Theme().TextMuted)
	})
	button.OnClick(onClick)
}

// projectLabel names the project the rail is scoped to, as ProjectBar does.
func (a *App) projectLabel() string {
	for _, p := range a.projects {
		if p.ID == a.project {
			return p.Name
		}
	}
	if len(a.projects) > 0 {
		return a.projects[0].Name
	}
	return a.cfg.Home
}

// sortedSurfaces is the rail's order: pinned first, then newest first, as the
// rail sorts them. The search field and the archived toggle filter here,
// so every view of the list sees the same chats in the same order.
func (a *App) sortedSurfaces() []protocol.Surface {
	query := strings.ToLower(a.query)
	out := make([]protocol.Surface, 0, len(a.surfaces))
	for _, s := range a.surfaces {
		if s.Archived && !a.showArchived {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(surfaceTitle(s)), query) {
			continue
		}
		out = append(out, s)
	}
	// Pinned chats keep their own order, then the drag order the user saved,
	// then newest first — the comparison the rail view:227-239 uses.
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := a.isPinned(out[i].ConversationID), a.isPinned(out[j].ConversationID)
		if pi != pj {
			return pi
		}
		oi, oj := a.orderIndex(out[i].ConversationID), a.orderIndex(out[j].ConversationID)
		if oi >= 0 && oj >= 0 {
			return oi < oj
		}
		return out[i].ConversationID > out[j].ConversationID
	})
	return out
}

// isPinned reports whether the rail keeps the chat at the top.
func (a *App) isPinned(conversationID int) bool {
	for _, id := range a.prefs.Pinned {
		if id == conversationID {
			return true
		}
	}
	return false
}

// togglePin moves the chat between the pinned block and the rest.
func (a *App) togglePin(conversationID int) {
	a.apply(func() {
		kept := a.prefs.Pinned[:0]
		found := false
		for _, id := range a.prefs.Pinned {
			if id == conversationID {
				found = true
				continue
			}
			kept = append(kept, id)
		}
		if !found {
			kept = append(kept, conversationID)
		}
		a.prefs.Pinned = kept
		a.savePrefsLater()
		a.invalidate()
	})
}

// archiveSurface asks the bridge to archive the chat, or to bring it back, and
// paints the answer: the rail keeps its own copy of the surfaces.
func (a *App) archiveSurface(s protocol.Surface) {
	archived := !s.Archived
	go func() {
		err := a.api.SetSurfaceMeta(contextBG(), api.SurfaceMeta{
			ConversationID: s.ConversationID,
			Archived:       &archived,
		})
		if err != nil {
			a.fail("archive chat %d: %v", s.ConversationID, err)
			return
		}
		a.apply(func() {
			for i := range a.surfaces {
				if a.surfaces[i].ConversationID == s.ConversationID {
					a.surfaces[i].Archived = archived
				}
			}
			a.invalidate()
		})
	}()
}

// newConversation asks the bridge for a fresh chat and opens it.
func (a *App) newConversation() {
	ctx := contextBG()
	conversationID, _, err := a.api.NewConversation(ctx, a.project)
	if err != nil {
		a.fail("new conversation: %v", err)
		return
	}
	if err := a.loadState(ctx); err != nil {
		a.fail("refresh after new chat: %v", err)
	}
	a.selectConversation(conversationID)
}

// railStatus is the one line under the app name: what the agent is doing right
// now, not what it did (the rail view:320-328).
func (a *App) railStatus() string {
	switch {
	case a.busy:
		return i18n.T("busy")
	case a.conn == ConnOpen || a.reachable:
		return i18n.T("idle")
	case a.conn == ConnConnecting:
		return i18n.T("connecting")
	case a.downReason == "error":
		return i18n.T("bridgeFailed")
	}
	return i18n.T("disconnected")
}

// avatarLook picks the face and colour: a running chat makes the bot think, the
// two and a half seconds after a run finishes it smiles, and the colour says
// who is working — a delegated task is not the same as this chat answering.
// markLook is the app's mark's mood: it breathes when the window is
// quiet, flickers while a turn or a task runs, and beats once when an answer lands.
// delegated reports that the work is a background task rather than the open chat,
// which tints the mark so the two are told apart at a glance.
func (a *App) markInk(t theme.Tokens, delegated bool) ui.Color {
	if delegated {
		return t.Primary
	}
	return t.SidebarFg
}

func (a *App) markLook() (brand.Mood, bool) {
	running := a.live != nil && len(a.live.Run) > 0
	now := time.Now()
	if running {
		a.wasLive, a.mood, a.moodUntil = true, brand.MoodThinking, time.Time{}
	} else {
		if a.wasLive {
			a.wasLive, a.mood, a.moodUntil = false, brand.MoodHappy, now.Add(2500*time.Millisecond)
		}
		if a.mood == brand.MoodHappy && now.After(a.moodUntil) {
			a.mood = brand.MoodIdle
		}
	}
	return a.mood, a.runningTasks() > 0
}

// runningTasks counts the background tasks the rail is already listing.
func (a *App) runningTasks() int {
	n := 0
	for _, task := range a.tasks {
		if task.State == "running" {
			n++
		}
	}
	return n
}

// orderIndex is where the user last dragged this chat, or -1 when they never
// have: prefs.Order holds conversation ids front to back.
func (a *App) orderIndex(conversationID int) int {
	for i, id := range a.prefs.Order {
		if id == conversationID {
			return i
		}
	}
	return -1
}

// reorderChats saves the drag the list reported: the rows moved, and the row
// they now sit above. Rows include the header of a folded project, so the
// mapping goes through the row list drawn last frame.
func (a *App) reorderChats(moved []int, to int) {
	if len(moved) == 0 {
		return
	}
	order := make([]int, 0, len(a.prefs.Order)+len(moved))
	for _, surface := range a.sortedSurfaces() {
		order = append(order, surface.ConversationID)
	}
	if len(order) == 0 {
		return
	}
	// The drag reported row indices of the drawn list; translate them to ids.
	ids := make([]int, 0, len(moved))
	for _, row := range moved {
		if id, ok := a.chatIDAtRow(row); ok {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	next := make([]int, 0, len(order))
	for _, id := range order {
		if !containsInt(ids, id) {
			next = append(next, id)
		}
	}
	at := len(next)
	if id, ok := a.chatIDAtRow(to); ok {
		at = indexOfInt(next, id)
	} else if to >= len(next) {
		at = len(next)
	}
	if at < 0 {
		at = 0
	}
	next = append(next[:at], append(ids, next[at:]...)...)
	a.apply(func() {
		a.prefs.Order = next
		// Dragging to the very top pins, as the rail view:302-316 does.
		if at == 0 {
			for _, id := range ids {
				if !a.isPinned(id) {
					a.prefs.Pinned = append(a.prefs.Pinned, id)
				}
			}
		}
	})
	a.savePrefsLater()
	a.invalidate()
}

func containsInt(list []int, want int) bool { return indexOfInt(list, want) >= 0 }

func indexOfInt(list []int, want int) int {
	for i, v := range list {
		if v == want {
			return i
		}
	}
	return -1
}

// chatIDAtRow maps a drawn row index to the chat it shows; project headers and
// task rows have no chat.
func (a *App) chatIDAtRow(row int) (int, bool) {
	if row < 0 || row >= len(a.rows) {
		return 0, false
	}
	if a.rows[row].kind != railChatRow {
		return 0, false
	}
	return a.rows[row].surface.ConversationID, true
}
