package app

import (
	"sync"

	"pitta/internal/protocol"
)

// surfaceHub shares the rail between windows. A chat archived in one window,
// or a new chat started in another, has to show up everywhere: the windows
// share one client and one set of surfaces, so they must share one list.
var surfaceHub = struct {
	sync.Mutex
	subs map[*App]bool
}{subs: map[*App]bool{}}

// subscribeSurfaces makes a window hear the surface list of the others.
func subscribeSurfaces(a *App) {
	surfaceHub.Lock()
	defer surfaceHub.Unlock()
	surfaceHub.subs[a] = true
}

// unsubscribeSurfaces stops the hearing, for a window that closed.
func unsubscribeSurfaces(a *App) {
	surfaceHub.Lock()
	defer surfaceHub.Unlock()
	delete(surfaceHub.subs, a)
}

// broadcastSurfaces hands the list a window just loaded to the others. It runs
// on the goroutine that fetched it, so it only posts to each window's loop.
func broadcastSurfaces(from *App, surfaces []protocol.Surface) {
	surfaceHub.Lock()
	listeners := make([]*App, 0, len(surfaceHub.subs))
	for other := range surfaceHub.subs {
		if other != from {
			listeners = append(listeners, other)
		}
	}
	surfaceHub.Unlock()
	for _, other := range listeners {
		other := other
		other.post(func() {
			other.surfaces = surfaces
			// The chat this window shows may have been archived elsewhere;
			// the rail drops it, and so must the window.
			other.syncTitle()
			other.invalidate()
		})
	}
}

// broadcastApprovals hands a freshly polled approval list to the other
// windows. Two windows polling five seconds apart would otherwise disagree
// about what waits for the user, and the one that resolved an approval would
// keep showing it while the other had already dropped it.
func broadcastApprovals(from *App, approvals []protocol.PendingApproval) {
	surfaceHub.Lock()
	listeners := make([]*App, 0, len(surfaceHub.subs))
	for other := range surfaceHub.subs {
		if other != from {
			listeners = append(listeners, other)
		}
	}
	surfaceHub.Unlock()
	for _, other := range listeners {
		other := other
		other.post(func() {
			other.approvals = approvals
			other.syncBadge()
			other.syncTitle()
			other.invalidate()
		})
	}
}

// openWindows records which chats have a window of their own, so the rail can
// say so and a click can bring that window forward instead of loading the chat
// a second time in this one.
var openWindows = struct {
	sync.Mutex
	byChat map[int]ownWindow
}{byChat: map[int]ownWindow{}}

// ownWindow is a window opened for one chat, and who owns it, so the owner's
// own clicks do not send it forward.
type ownWindow struct {
	owner *App
	focus func()
}

// noteOwnWindow registers a window opened for one chat.
func noteOwnWindow(owner *App, chat int, focus func()) {
	if chat == 0 || focus == nil {
		return
	}
	openWindows.Lock()
	defer openWindows.Unlock()
	openWindows.byChat[chat] = ownWindow{owner: owner, focus: focus}
}

// forgetOwnWindow drops the registration when that window closes.
func forgetOwnWindow(chat int) {
	openWindows.Lock()
	defer openWindows.Unlock()
	delete(openWindows.byChat, chat)
}

// ownWindowFocus returns the focus function of the window showing chat, if one
// is open.
// focusOwnWindow brings forward the window showing chat, unless it is the one
// asking. It reports whether a window was raised.
func focusOwnWindow(asker *App, chat int) bool {
	openWindows.Lock()
	window, ok := openWindows.byChat[chat]
	openWindows.Unlock()
	if !ok || window.owner == asker {
		return false
	}
	window.focus()
	return true
}
