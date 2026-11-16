package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// searchTurns finds the turns whose text contains the query, newest first, as
// ConvSearch highlights the messages that matched.
func (a *App) searchTurns() []int {
	query := strings.ToLower(strings.TrimSpace(a.convQuery))
	if query == "" {
		return nil
	}
	built := turns.GroupTurns(a.entries, a.waits)
	var matched []int
	for index := range built {
		if strings.Contains(strings.ToLower(turnHaystack(&built[index])), query) {
			matched = append(matched, index)
		}
	}
	return matched
}

// turnHaystack is every text a turn holds, for the find bar to search.
func turnHaystack(turn *turns.Turn) string {
	var parts []string
	if turn.User != nil {
		parts = append(parts, turns.Text(*turn.User))
	}
	for _, entry := range turn.Finals {
		parts = append(parts, turns.Text(entry))
	}
	for _, entry := range turn.Notes {
		parts = append(parts, noteText(entry))
	}
	for _, step := range turn.Steps {
		parts = append(parts, step.Label)
		if step.Result != nil {
			if text, _ := turns.ToolResult(*step.Result); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// stepMatch moves through the matches, wrapping at both ends.
func (a *App) stepMatch(direction int) {
	if len(a.convMatches) == 0 {
		return
	}
	a.apply(func() {
		a.matchIndex += direction
		if a.matchIndex < 0 {
			a.matchIndex = len(a.convMatches) - 1
		}
		if a.matchIndex >= len(a.convMatches) {
			a.matchIndex = 0
		}
		a.invalidate()
	})
}

// focusedTurn is the turn index the find bar is standing on, or -1.
func (a *App) focusedTurn() int {
	if len(a.convMatches) == 0 || a.matchIndex < 0 || a.matchIndex >= len(a.convMatches) {
		return -1
	}
	return a.convMatches[a.matchIndex]
}

// findBar is ConvSearch: a query field, the match counter and the two step
// buttons, shown in the header while the conversation is open.
func (a *App) findBar(c *ui.Context, t theme.Tokens) {
	ui.Row(c).Gap(4).Children(func() {
		ui.TextInput(c, &a.convQuery).FontSize(11).Width(180).Placeholder(i18n.T("findShort")).
			OnChange(func() {
				a.apply(func() {
					a.convMatches = a.searchTurns()
					a.matchIndex = 0
					a.invalidate()
				})
			})
		if strings.TrimSpace(a.convQuery) != "" {
			ui.Text(c, findCount(a.matchIndex, len(a.convMatches))).FontSize(10).TextColor(t.MutedFg)
			ui.Button(c, "").Padding(3, 5).Children(func() {
				ui.Icon(c, icons.ChevronDown).Size(10, 10)
			}).OnClick(func() { a.stepMatch(1) })
			ui.Button(c, "").Padding(3, 5).Children(func() {
				ui.Icon(c, icons.ChevronRight).Size(10, 10)
			}).OnClick(func() { a.stepMatch(-1) })
			ui.Button(c, "").Padding(3, 5).Children(func() {
				ui.Icon(c, icons.X).Size(10, 10)
			}).OnClick(func() {
				a.apply(func() {
					a.convQuery = ""
					a.convMatches = nil
					a.matchIndex = 0
					a.invalidate()
				})
			})
		}
	})
}

// findCount is "3/12", or the empty hint when nothing matched.
func findCount(index, total int) string {
	if total == 0 {
		return "0/0"
	}
	return itoa(index+1) + "/" + itoa(total)
}

// chatMenu is ChatMenu: rename the chat's title, archive it, or drop its
// worktree. Every write goes through the bridge and re-reads the rail.
type chatMenu struct {
	open  bool
	title string
	busy  bool
	note  string
}

// openChatMenu fills the dialog from the selected surface.
func (a *App) openChatMenu() {
	a.apply(func() {
		a.menu.open = true
		a.menu.title = a.surfaceTitleByID(a.current)
		a.menu.note = ""
		a.invalidate()
	})
}

// surfaceTitleByID is the rail's title rule for one chat.
func (a *App) surfaceTitleByID(conversationID int) string {
	for _, surface := range a.surfaces {
		if surface.ConversationID == conversationID {
			return surfaceTitle(surface)
		}
	}
	return ""
}

// chatMenuDialog draws the chat actions.
func (a *App) chatMenuDialog(c *ui.Context, t theme.Tokens) {
	if !a.menu.open {
		return
	}
	ui.Modal(c, &a.menu.open, func() {
		ui.Column(c).Width(360).Padding(14).Gap(10).Radius(10).Background(t.Card).Border(1, t.Border).Children(func() {
			ui.Text(c, i18n.T("renameChat")).FontSize(13).TextColor(t.Foreground)
			ui.TextInput(c, &a.menu.title).FontSize(12).Placeholder(i18n.T("renameHint"))
			if a.menu.note != "" {
				ui.Text(c, a.menu.note).FontSize(10).TextColor(t.Destructive)
			}
			ui.Row(c).Gap(6).Children(func() {
				ui.PrimaryButton(c, i18n.T("save")).FontSize(11).OnClick(func() { a.renameChat() })
				ui.Button(c, i18n.T("archiveChat")).FontSize(11).OnClick(func() { a.archiveChat(true) })
				ui.Button(c, i18n.T("cleanupWorktree")).FontSize(11).OnClick(func() { a.removeWorktree() })
				ui.Row(c).Grow(1)
				ui.Button(c, i18n.T("cancel")).FontSize(11).OnClick(func() {
					a.apply(func() { a.menu.open = false; a.invalidate() })
				})
			})
		})
	})
}

// renameChat saves a new title, then reloads the rail.
func (a *App) renameChat() {
	conversationID := a.currentValue()
	title := a.menu.title
	if conversationID == 0 || strings.TrimSpace(title) == "" {
		return
	}
	a.goLoad(func() error {
		if err := a.api.SetSurfaceMeta(context.Background(), a.apiSurfaceMeta(conversationID, &title, nil)); err != nil {
			return err
		}
		a.apply(func() { a.menu.open = false; a.menu.note = "" })
		return a.loadState(context.Background())
	})
}

// apiSurfaceMeta builds the surface-meta body the bridge expects; nil halves
// leave that field alone. The surface id rides along because the bridge cannot
// resolve every conversation to one, and answers 400 without either.
func (a *App) apiSurfaceMeta(conversationID int, title *string, archived *bool) api.SurfaceMeta {
	meta := api.SurfaceMeta{ConversationID: conversationID, Title: title, Archived: archived}
	for _, surface := range a.surfaces {
		if surface.ConversationID == conversationID {
			meta.SurfaceID = surface.SurfaceID
			break
		}
	}
	return meta
}

// archiveChat hides the chat from the rail, as the row's archive glyph does.
func (a *App) archiveChat(archived bool) {
	conversationID := a.currentValue()
	if conversationID == 0 {
		return
	}
	a.goLoad(func() error {
		if err := a.api.SetSurfaceMeta(context.Background(), a.apiSurfaceMeta(conversationID, nil, &archived)); err != nil {
			return err
		}
		a.apply(func() { a.menu.open = false })
		return a.loadState(context.Background())
	})
}

// removeWorktree drops the chat's worktree; the chat itself stays readable.
func (a *App) removeWorktree() {
	conversationID := a.currentValue()
	if conversationID == 0 {
		return
	}
	a.goLoad(func() error {
		if err := a.api.RemoveWorktree(context.Background(), conversationID); err != nil {
			return err
		}
		a.apply(func() { a.menu.open = false })
		return a.loadState(context.Background())
	})
}
