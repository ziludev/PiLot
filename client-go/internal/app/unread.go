package app

import (
	"sort"

	"pitta/internal/protocol"
)

// Unread marks
//
// A chat that finished an answer while the window was behind another app keeps
// a dot in the rail until it is opened. The React client had nothing like this:
// its notification vanished with the banner, and the rail looked the same
// either way.

// markUnread records that a chat has news the reader has not seen. UI thread.
func (a *App) markUnread(conversationID int) {
	if conversationID == 0 || conversationID == a.currentValue() && a.focused {
		return
	}
	if a.unread == nil {
		a.unread = map[int]bool{}
	}
	a.unread[conversationID] = true
}

// clearUnread forgets a chat's dot, which opening it does.
func (a *App) clearUnread(conversationID int) {
	if a.unread == nil || !a.unread[conversationID] {
		return
	}
	delete(a.unread, conversationID)
}

// isUnread reports the dot for a rail row.
func (a *App) isUnread(conversationID int) bool { return a.unread[conversationID] }

// unreadIDs lists the dots for the preference file, sorted so the file is
// stable across saves.
func (a *App) unreadIDs() []int {
	if len(a.unread) == 0 {
		return nil
	}
	ids := make([]int, 0, len(a.unread))
	for id := range a.unread {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// noteTaskFinish dots a chat whose task stopped while the reader was elsewhere:
// the transcript socket only carries the open chat, so the task list is how a
// background chat's completion becomes visible.
func (a *App) noteTaskFinish(previous, current []protocol.Task) {
	running := map[int]bool{}
	for _, task := range previous {
		if task.State == "running" || task.State == "queued" || task.State == "pending" {
			running[task.ConversationID] = true
		}
	}
	still := map[int]bool{}
	for _, task := range current {
		if task.State == "running" || task.State == "queued" || task.State == "pending" {
			still[task.ConversationID] = true
		}
	}
	for id := range running {
		if still[id] || id == a.currentValue() && a.focused {
			continue
		}
		a.markUnread(id)
	}
}

// unreadFromPrefs restores the dots a previous run left behind.
func unreadFromPrefs(p prefs) map[int]bool {
	if len(p.Unread) == 0 {
		return map[int]bool{}
	}
	out := make(map[int]bool, len(p.Unread))
	for _, id := range p.Unread {
		out[id] = true
	}
	return out
}
