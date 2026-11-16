package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
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

// runSearch asks the bridge to look inside transcripts. The filter box already
// narrows the list by title as the user types; this only runs on submit, as
// SearchBox.tsx does, because the bridge scans up to 60 chats per query.
func (a *App) runSearch() {
	query := strings.TrimSpace(a.filter)
	if len([]rune(query)) < 2 {
		a.apply(func() {
			a.searchHits = nil
			a.searchPending = false
			a.invalidate()
		})
		return
	}
	var seq int
	a.apply(func() {
		a.searchSeq++
		seq = a.searchSeq
		a.searchPending = true
		a.invalidate()
	})
	// Without a window there is no event loop to return to, so tests run the
	// request inline instead of leaving it to a goroutine.
	if a.win == nil {
		a.searchOnce(query, seq)
		return
	}
	go a.searchOnce(query, seq)
}

// searchOnce asks for the hits and applies the answer, guarded by seq so a
// slower earlier query cannot overwrite a newer one.
func (a *App) searchOnce(query string, seq int) {
	{
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		hits, err := a.api.Search(ctx, query)
		a.apply(func() {
			// A newer query already started: its result wins.
			if seq != a.searchSeq {
				return
			}
			a.searchPending = false
			if err != nil {
				a.fail("search: %v", err)
			} else {
				a.searchHits = hits
			}
			a.invalidate()
		})
	}
}

// jumpToHit opens the conversation a search hit belongs to and marks the turn
// that holds the entry. The window moves to that turn in transcript().
func (a *App) jumpToHit(hit protocol.SearchHit) {
	a.apply(func() {
		a.searchHits = nil
		a.invalidate()
	})
	a.selectConversation(hit.ConversationID)
	a.apply(func() {
		a.focusEntry = hit.EntryID
		a.invalidate()
	})
}

// turnOfEntry is the turn index holding entryID, or -1. A hit can point at a
// tool result or a note, so every entry of the turn is checked.
func turnOfEntry(built []turns.Turn, entryID int) int {
	for i, turn := range built {
		if turn.User != nil && turn.User.ID == entryID {
			return i
		}
		for _, group := range [][]protocol.Entry{turn.Finals, turn.Reports, turn.Notes, turn.Internals} {
			for _, entry := range group {
				if entry.ID == entryID {
					return i
				}
			}
		}
		for _, step := range turn.Steps {
			if step.Entry.ID == entryID || (step.Result != nil && step.Result.ID == entryID) {
				return i
			}
		}
	}
	return -1
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
	// No transition here: an entering row moves the input while it takes the
	// focus, and the first keys land nowhere.
	ui.Row(c).Gap(4).Children(func() {
		ui.TextInput(c, &a.convQuery).FontSize(11).Width(180).Label(i18n.T("findShort")).
			Placeholder(i18n.T("findShort")).
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
	id    int
	title string
	busy  bool
	note  string
}

// openChatMenu fills the dialog from the selected surface.
func (a *App) openChatMenu() {
	a.openChatMenuFor(a.currentValue())
}

// openChatMenuFor opens the actions of one chat, which the rail's row menu and
// the header both do.
func (a *App) openChatMenuFor(conversationID int) {
	a.apply(func() {
		a.menu.open = true
		a.menu.id = conversationID
		a.menu.title = a.surfaceTitleByID(conversationID)
		a.menu.note = ""
		a.invalidate()
	})
}

// menuChatID is the chat the open menu acts on, falling back to the selected
// one when the menu was opened without naming a row.
func (a *App) menuChatID() int {
	if a.menu.id != 0 {
		return a.menu.id
	}
	return a.currentValue()
}

// surfaceArchived reports whether the chat is archived, which flips the label
// and the action of the chat menu's archive entry.
func (a *App) surfaceArchived(conversationID int) bool {
	for _, surface := range a.surfaces {
		if surface.ConversationID == conversationID {
			return surface.Archived
		}
	}
	return false
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
	ui.DialogBase(c, &a.menu.open, func(back, panel ui.Element) {
		back.Background(ui.RGBA(0, 0, 0, 0.2))
		// The chat menu is a glass pane, as ChatMenu.tsx's `glass` dropdown is:
		// a form of four controls over a small panel.
		panel.Width(360).Padding(14).Gap(10).Radius(12).Material(panelGlass()).Children(func() {
			ui.Text(c, i18n.T("rename")).FontSize(13).TextColor(t.Foreground)
			ui.TextInput(c, &a.menu.title).FontSize(12).Placeholder(i18n.T("renameHint"))
			if a.menu.note != "" {
				ui.Text(c, a.menu.note).FontSize(10).TextColor(t.Destructive)
			}
			ui.Row(c).Gap(6).Children(func() {
				ui.PrimaryButton(c, i18n.T("save")).FontSize(11).OnClick(func() { a.renameChat() })
				archived := a.surfaceArchived(a.menuChatID())
				label := i18n.T("archive")
				if archived {
					label = i18n.T("unarchive")
				}
				ui.Button(c, label).FontSize(11).OnClick(func() { a.archiveChat(!archived) })
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
	conversationID := a.menuChatID()
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
	conversationID := a.menuChatID()
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
	conversationID := a.menuChatID()
	if conversationID == 0 {
		return
	}
	a.goLoad(func() error {
		result, err := a.api.RemoveWorktree(context.Background(), conversationID)
		if err != nil {
			return err
		}
		// An unmerged branch is kept, and the chat menu has to say so: the
		// user would otherwise think the checkout is gone (ChatMenu.tsx:124-140).
		if result != nil && result.Status == "unmerged" {
			a.apply(func() {
				a.notice = i18n.T("unmergedWork")
				a.invalidate()
			})
			return nil
		}
		a.apply(func() { a.menu.open = false })
		return a.loadState(context.Background())
	})
}
