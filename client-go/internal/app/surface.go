package app

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
	"pilot/internal/protocol"
	"pilot/internal/theme"
)

// The rail's two writes on a chat and the cross-chat search behind its filter
// box. Both writes have the same shape: the mirror is patched first, so the
// frame after the click already shows the new title or the archive, and the
// bridge is told afterwards. A write that fails is undone.
const (
	// searchDebounce is how long the filter box waits before the bridge is
	// asked: a word typed at speed is one request, not eight.
	searchDebounce = 250 * time.Millisecond
	// searchMinRunes keeps one character local. Below two there is nothing to
	// ask about, and the rail's own filter is faster than a round trip.
	searchMinRunes = 2
	// searchTimeout bounds one search; the bridge is local, so a slow reply is
	// a bridge that is busy rather than a search worth waiting on.
	searchTimeout = 6 * time.Second
	// metaTimeout bounds one surface write.
	metaTimeout = 8 * time.Second
)

// searchState is the cross-chat search: the hits, the query they answer (in the
// rail's own normalized form), the text the last request was scheduled for, and
// the generation that retires a reply the user has typed past.
type searchState struct {
	seq   int
	seen  string
	query string
	hits  []protocol.SearchHit
}

// renameState is the rename dialog: whether it is up, which chat it edits, and
// the text in it, held here because the dialog is rebuilt every frame.
type renameState struct {
	open bool
	conv int
	text string
}

// surfaceUndo is a write's footprint, kept until the bridge answers so a failed
// write can put the mirror back the way the snapshot had it.
type surfaceUndo struct {
	convID   int
	title    string
	archived bool
}

// watchSearch notices the filter box moving and asks the bridge a quarter
// second later. It is called from the frame that drew the box, so typing costs
// one comparison per keystroke and one request per word.
func (a *App) watchSearch() {
	query := strings.TrimSpace(a.filter)
	if query == a.search.seen {
		return
	}
	a.search.seen = query
	// Every new text retires the replies still in flight: an answer to the
	// query before this one must not be drawn under this one.
	a.search.seq++
	seq := a.search.seq
	if utf8.RuneCountInString(query) < searchMinRunes {
		// Back under two characters the local filter is all there is; drop the
		// block rather than leave the last query's hits on screen.
		if a.search.query != "" || a.search.hits != nil {
			a.search.query, a.search.hits = "", nil
			a.stamp++
		}
		return
	}
	time.AfterFunc(searchDebounce, func() {
		a.post(event{kind: evSearch, seq: seq, text: query})
	})
}

// searchBlock reports whether the hit list is on screen for the filter as it
// stands: two characters, and hits answering this very query. The rail's local
// filter and the block are drawn from the same test, so they cannot disagree.
func (a *App) searchBlock(filter string) bool {
	return utf8.RuneCountInString(filter) >= searchMinRunes &&
		a.search.query == filter && len(a.search.hits) > 0
}

// startSearch runs one query on a goroutine, as GET /api/search. Failures and
// timeouts are silent: the rail falls back to the local filter it kept running.
func (a *App) startSearch(seq int, query string) {
	if seq != a.search.seq || strings.TrimSpace(a.filter) != query {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, searchTimeout)
		defer cancel()
		hits, err := a.api.Search(ctx, query)
		if err != nil {
			hits = nil
		}
		a.post(event{kind: evSearchDone, seq: seq, text: query, hits: hits})
	}()
}

// applySearch takes a reply only if it answers the query on screen now.
func (a *App) applySearch(seq int, query string, hits []protocol.SearchHit) {
	if seq != a.search.seq || strings.TrimSpace(a.filter) != query {
		return
	}
	a.search.query = lowerTrim(query)
	a.search.hits = hits
	a.stamp++
}

// openRename puts the dialog up on the chat the menu belonged to, seeded with
// the title it has now.
func (a *App) openRename(convID int) {
	title := ""
	for _, s := range a.surfaceSnapshot() {
		if s.ConversationID == convID {
			title = s.Title
			break
		}
	}
	a.rename = renameState{open: true, conv: convID, text: title}
}

// commitRename closes the dialog and writes the title: the mirror first, so the
// row shows the new name on this frame, then the bridge.
func (a *App) commitRename() {
	convID, title := a.rename.conv, strings.TrimSpace(a.rename.text)
	a.rename.open = false
	if convID == 0 {
		return
	}
	undo := a.patchSurface(convID, func(s *protocol.Surface) { s.Title = title })
	patch := api.SurfaceMeta{ConversationID: convID, Title: &title}
	a.writeSurface(patch, undo)
}

// setArchived files a chat away, or brings it back: the same patched-then-post
// order as a rename, since an archived row must leave the sections at once.
func (a *App) setArchived(convID int, archived bool) {
	if !archived && !a.archivedOnBridge(convID) {
		// The rail filed this chat away by itself, so the bridge never heard of
		// it: bringing it back is a note in this window's file, not a write.
		a.prefs.keepChat(convID)
		savePrefs(a.prefs)
		a.stamp++
		a.invalidate()
		return
	}
	undo := a.patchSurface(convID, func(s *protocol.Surface) { s.Archived = archived })
	if archived && convID == a.active {
		// Filing away the chat on screen would hide it under a closed shelf:
		// open the shelf, so the move is something the user sees.
		a.revealChatSection(convID)
		a.stamp++
		a.invalidate()
	}
	patch := api.SurfaceMeta{ConversationID: convID, Archived: &archived}
	a.writeSurface(patch, undo)
}

// archivedOnBridge is whether the bridge itself has this chat archived, as
// opposed to the rail having filed it away for being a week old.
func (a *App) archivedOnBridge(convID int) bool {
	for _, s := range a.surfaceSnapshot() {
		if s.ConversationID == convID {
			return s.Archived
		}
	}
	return false
}

// patchSurface rewrites one chat in the mirror and asks for a frame. It returns
// what the row was, so the write can be undone if the bridge refuses it.
func (a *App) patchSurface(convID int, patch func(*protocol.Surface)) *surfaceUndo {
	surfaces := a.surfaceSnapshot()
	undo := &surfaceUndo{convID: convID}
	found := false
	for i := range surfaces {
		if surfaces[i].ConversationID == convID {
			if !found {
				undo.title, undo.archived = surfaces[i].Title, surfaces[i].Archived
				found = true
			}
			patch(&surfaces[i])
		}
	}
	a.setSurfaces(surfaces)
	a.stamp++
	a.invalidate()
	return undo
}

// writeSurface tells the bridge what the mirror already says.
func (a *App) writeSurface(patch api.SurfaceMeta, undo *surfaceUndo) {
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, metaTimeout)
		defer cancel()
		if err := a.api.PatchSurface(ctx, patch); err != nil {
			a.post(event{kind: evMetaFailed, undo: undo})
		}
	}()
}

// undoSurface puts a row back after a write the bridge refused. Silent: the
// next snapshot is the truth, and a notice would outlive the mistake.
func (a *App) undoSurface(undo *surfaceUndo) {
	if undo == nil {
		return
	}
	a.patchSurface(undo.convID, func(s *protocol.Surface) {
		s.Title, s.Archived = undo.title, undo.archived
	})
}

// renameDialog draws the rename dialog while it is up: the title box, and the
// two answers a dialog has.
func (a *App) renameDialog(c *ui.Context, t theme.Tokens) {
	if !a.rename.open {
		return
	}
	ui.Modal(c, &a.rename.open, func() {
		ui.Column(c).Width(300).Padding(16).Gap(10).Radius(theme.Radius).
			Background(t.Popover).Border(1, t.Border).Children(func() {
			ui.Text(c, i18n.T("rename")).FontSize(theme.TextRow).FontWeight(600).TextColor(t.Foreground)
			ui.TextInputBase(c, &a.rename.text).FontSize(theme.TextRow).Height(30).
				Padding(0, 8).Radius(theme.RadiusChip).Background(t.SidebarAccent)
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				a.dialogButton(c, t, i18n.T("cancel"), false, func() {
					a.rename.open = false
				})
				a.dialogButton(c, t, i18n.T("save"), true, a.commitRename)
			})
		})
	})
}

// dialogButton is one answer in the rename dialog: a filled button for the one
// that commits, a quiet one for the one that does not.
func (a *App) dialogButton(c *ui.Context, t theme.Tokens, label string, primary bool, onClick func()) {
	button := ui.ButtonBase(c).Height(28).Radius(theme.RadiusControl).Padding(0, 12).
		Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if primary {
		// The one blue thing in a dialog is the answer it wants: ink reads as
		// another piece of text at button size.
		button = button.Background(t.Brand)
	} else {
		button = button.Background(t.Field).Border(1, t.LineSoft)
	}
	button.Children(func() {
		colour, weight := t.Ink2, 500
		if primary {
			colour, weight = ui.RGBA(255, 255, 255, 1), 600
		}
		ui.Text(c, label).FontSize(theme.TextSmall).FontWeight(weight).TextColor(colour)
	})
	button.OnClick(onClick)
}
