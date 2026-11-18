package app

import (
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/motion"
	"pitta/internal/notify"
	"pitta/internal/protocol"
)

// Unread marks
//
// A chat that finished an answer while the window was behind another app keeps
// a dot in the rail until it is opened. The earlier web client had nothing like this:
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
	// The same predicate the rail marks rows with, so a state cannot read as
	// "finished" here and "running" there.
	running := map[int]bool{}
	for _, task := range previous {
		if taskUnfinished(task.State) {
			running[task.ConversationID] = true
		}
	}
	still := map[int]bool{}
	for _, task := range current {
		if taskUnfinished(task.State) {
			still[task.ConversationID] = true
		}
	}
	for id := range running {
		if still[id] || id == a.currentValue() && a.focused {
			continue
		}
		a.markUnread(id)
	}
	a.announceFinished(previous, still)
}

// announceFinished says which background jobs are over. A dot on the row is not
// enough for the jobs that run for minutes: the reader has gone to do something
// else by the time one lands, so the toast names it and the notification reaches
// past the window when the window is not the thing being looked at.
func (a *App) announceFinished(previous []protocol.Task, still map[int]bool) {
	done := map[int][]string{}
	for _, task := range previous {
		if !taskUnfinished(task.State) || still[task.ConversationID] || !appJob(task) {
			continue
		}
		if task.ConversationID == a.currentValue() && a.focused {
			continue
		}
		done[task.ConversationID] = append(done[task.ConversationID], railTaskTitle(task))
	}
	ids := make([]int, 0, len(done))
	for id := range done {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		names := done[id]
		a.toast(ui.Toast{
			ID:       "finished-" + itoa(id),
			Title:    strings.Join(names, ", ") + " · " + i18n.T("finished"),
			Action:   i18n.T("open"),
			OnAction: func() { a.selectConversation(id) },
			Timeout:  motion.ToastLife,
		})
		if !a.focused {
			notify.Task(a.surfaceTitleByID(id), strings.Join(names, ", "), id)
		}
	}
}

// appJob reports whether a task is one of the jobs the rail lists. The rail's
// own filter answers it, so a job cannot be announced here and missing there.
func appJob(task protocol.Task) bool {
	return len(railLiveTasks([]protocol.Task{task})) > 0
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
