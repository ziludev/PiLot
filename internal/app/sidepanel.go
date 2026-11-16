package app

import (
	"context"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/ops"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// sidePanel is the right-hand column: the four tabs of SidePanel.tsx, each
// reading data the app already loaded and every action sent back through a
// callback.
func (a *App) sidePanel(c *ui.Context) {
	t := theme.Current(c)
	ui.Column(c).Fill().Background(t.Sidebar).Children(func() {
		a.sideTabs(c, t)
		ui.Divider(c).Background(t.SidebarBorder)
		ui.Scroll(c).Fill().Children(func() {
			switch a.sideTab {
			case SideMemory:
				ops.MemoryPanel(c, ops.MemoryProps{
					Memory:      a.memory,
					Query:       a.memoryQuery,
					Curating:    a.curating,
					Suggestions: a.suggestions,
					Selected:    a.memoryUnchecked,
					OnQuery: func(query string) {
						a.apply(func() { a.memoryQuery = query; a.invalidate() })
					},
					OnCurate: func() { a.curateMemory() },
					OnToggle: func(index int) {
						a.apply(func() {
							if a.memoryUnchecked == nil {
								a.memoryUnchecked = map[int]bool{}
							}
							a.memoryUnchecked[index] = !a.memoryUnchecked[index]
							a.invalidate()
						})
					},
					OnApply: func(indices []int) { a.applyMemory(indices) },
					OnDrop:  func(index int) { a.dropSuggestion(index) },
				})
			case SideAccess:
				ops.AccessPanel(c, ops.AccessProps{
					View:     a.access,
					Audit:    a.audit,
					OnReload: func() { a.goLoad(func() error { return a.loadAccess(context.Background()) }) },
				})
			case SideUsage:
				ops.UsageCalendar(c, ops.UsageProps{Days: a.usageDays})
			default:
				a.sideChanges(c, t)
			}
		})
	})
}

// sideTabs is the tab strip; the active tab keeps the app state so a restart
// reopens the same panel.
func (a *App) sideTabs(c *ui.Context, t theme.Tokens) {
	tabs := []struct {
		tab   SideTab
		label string
		icon  *ui.SVG
	}{
		{SideChanges, i18n.T("changes"), icons.GitCompare},
		{SideMemory, i18n.T("memory"), icons.Brain},
		{SideAccess, i18n.T("access"), icons.ShieldCheck},
		{SideUsage, i18n.T("usage"), icons.Clock},
	}
	ui.Row(c).FillWidth().Padding(4, 6).Gap(2).Background(t.Sidebar).Children(func() {
		for _, entry := range tabs {
			entry := entry
			active := a.sideTab == entry.tab
			ui.Button(c, entry.label).FontSize(10).Padding(4, 8).
				Background(map[bool]ui.Color{true: t.SidebarAccent, false: t.Sidebar}[active]).
				TextColor(map[bool]ui.Color{true: t.SidebarFg, false: t.MutedFg}[active]).
				OnClick(func() { a.openSideTab(entry.tab) })
		}
		ui.Row(c).Grow(1)
		ui.Button(c, "").Padding(3, 5).Children(func() {
			ui.Icon(c, icons.X).Size(10, 10)
		}).OnClick(func() {
			a.apply(func() { a.sideOpen = false; a.invalidate() })
		})
	})
}

// openSideTab switches the panel and loads what that tab needs.
func (a *App) openSideTab(tab SideTab) {
	a.apply(func() {
		a.sideTab = tab
		a.sideOpen = true
		a.invalidate()
	})
	a.reloadSideTab()
}

// toggleSide opens and closes the column from the header button.
func (a *App) toggleSide() {
	a.apply(func() {
		a.sideOpen = !a.sideOpen
		a.invalidate()
	})
	if a.sideOpen {
		a.reloadSideTab()
	}
}

// reloadSideTab fetches the data the active tab draws, off the UI thread.
func (a *App) reloadSideTab() {
	conversationID := a.currentValue()
	ctx := context.Background()
	switch a.sideTab {
	case SideMemory:
		a.goLoad(func() error { return a.loadMemory(ctx) })
	case SideAccess:
		a.goLoad(func() error { return a.loadAccess(ctx) })
	case SideUsage:
		a.goLoad(func() error { return a.loadUsage(ctx, 120) })
	case SideChanges:
		if conversationID != 0 {
			a.goLoad(func() error { return a.loadChanges(ctx) })
		}
	}
}

// loadAccess reads who may write where, plus the audit tail.
func (a *App) loadAccess(ctx context.Context) error {
	view, err := a.api.Access(ctx)
	if err != nil {
		return err
	}
	audit, err := a.api.Audit(ctx, 40)
	if err != nil {
		return err
	}
	a.apply(func() {
		a.access = view
		a.audit = audit
		a.invalidate()
	})
	return nil
}

// curateMemory asks the agent to propose note changes.
func (a *App) curateMemory() {
	a.apply(func() { a.curating = true; a.invalidate() })
	a.goLoad(func() error {
		curation, err := a.api.CurateMemory(context.Background(), 24)
		a.apply(func() {
			a.curating = false
			if err == nil && curation != nil {
				a.suggestions = curation.Suggestions
			}
			a.invalidate()
		})
		return err
	})
}

// applyMemory writes the suggestions the reader left checked.
func (a *App) applyMemory(indices []int) {
	suggestions := make([]protocol.MemorySuggestion, 0, len(indices))
	for _, index := range indices {
		if index >= 0 && index < len(a.suggestions) {
			suggestions = append(suggestions, a.suggestions[index])
		}
	}
	if len(suggestions) == 0 {
		return
	}
	a.goLoad(func() error {
		if err := a.api.ApplyMemory(context.Background(), suggestions); err != nil {
			return err
		}
		a.apply(func() {
			a.suggestions = nil
			a.memoryUnchecked = nil
			a.invalidate()
		})
		return a.loadMemory(context.Background())
	})
}

// dropSuggestion removes one proposal the user does not want.
func (a *App) dropSuggestion(index int) {
	a.apply(func() {
		if index >= 0 && index < len(a.suggestions) {
			a.suggestions = append(a.suggestions[:index], a.suggestions[index+1:]...)
		}
		a.invalidate()
	})
}

// openTask loads the detail of a running task and shows it.
func (a *App) openTask(id int) {
	a.goLoad(func() error {
		detail, err := a.api.Task(context.Background(), id)
		if err != nil {
			return err
		}
		a.apply(func() {
			a.taskOpen = detail
			a.invalidate()
		})
		return nil
	})
}

// closeTask hides the detail dialog.
func (a *App) closeTask() {
	a.apply(func() {
		a.taskOpen = nil
		a.invalidate()
	})
}

// cancelTask asks the bridge to stop a running task.
func (a *App) cancelTask(id int) {
	a.goLoad(func() error {
		if err := a.api.CancelTask(context.Background(), id); err != nil {
			return err
		}
		return a.loadState(context.Background())
	})
}

// taskDialog is the task detail dialog; it draws nothing without a task.
func (a *App) taskDialog(c *ui.Context) {
	if a.taskOpen == nil {
		return
	}
	task := a.taskOpen
	ops.TaskDialog(c, ops.TaskProps{
		Task:     task,
		Open:     true,
		OnCancel: func() { a.cancelTask(task.ID) },
		OnClose:  func() { a.closeTask() },
	})
}

// sideWidthOf is the column width the split uses, clamped so a stored value
// from another window size cannot squeeze the chat away.
func (a *App) sideWidthOf() float32 {
	if a.sideWidth < 220 {
		a.sideWidth = 300
	}
	return a.sideWidth
}
