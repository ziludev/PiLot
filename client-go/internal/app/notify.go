package app

import (
	"strings"
	"time"

	"pitta/internal/notify"
	"pitta/internal/protocol"
	"pitta/internal/turns"
)

// notifySettle is how still the transcript has to be before an answer is
// announced. A turn streams text, calls a tool, streams more: announcing each
// frame would buzz the owner once per step of one answer.
const notifySettle = 2 * time.Second

// notifyGap is the least time between two notifications for the same chat, so a
// turn that keeps pausing cannot buzz at every pause.
const notifyGap = time.Minute

// maybeNotify pushes the notification one quiet moment further out. Only an
// answer that has fallen still is announced, and only while the window is
// behind another one.
func (a *App) maybeNotify() {
	a.apply(a.armNotify)
}

// armNotify starts the wait again, or drops the pending notification when the
// reader has come back to the window.
func (a *App) armNotify() {
	if a.notifyTimer != nil {
		a.notifyTimer.Stop()
		a.notifyTimer = nil
	}
	// The first window owns the notification, as it owns the Dock badge: a second
	// window on another chat would otherwise announce the same answers twice.
	if a.focused || a.secondary {
		return
	}
	wait := a.notifyQuiet
	if wait <= 0 {
		wait = notifySettle
	}
	a.notifyTimer = time.AfterFunc(wait, a.notifySettled)
}

// notifySettled is the moment the transcript fell still on: the newest answer is
// done now, so it reaches its owner once.
func (a *App) notifySettled() {
	a.apply(func() {
		a.notifyTimer = nil
		if a.focused || a.secondary {
			return
		}
		// A turn that is still working has more to say; its next frame arms the
		// wait again.
		if a.live != nil && len(a.live.Run) > 0 {
			return
		}
		answer := lastAssistant(a.entries)
		if answer == nil {
			return
		}
		chat := a.currentValue()
		if a.notified[chat] == itoa(answer.ID) {
			return
		}
		if time.Since(a.notifyAt[chat]) < notifyGap {
			return
		}
		text := strings.TrimSpace(turns.Text(*answer))
		if text == "" {
			return
		}
		a.notified[chat] = itoa(answer.ID)
		a.notifyAt[chat] = time.Now()
		notify.Answer(a.chatName(), text, chat)
	})
}

// chatName is the open chat's name, which a notification leads with so the owner
// knows which conversation finished without opening the window.
func (a *App) chatName() string {
	for _, surface := range a.surfaces {
		if surface.ConversationID == a.currentValue() {
			return surfaceTitle(surface)
		}
	}
	return ""
}

// lastAssistant is the newest assistant entry, the one an owner waiting on a
// long turn wants to read.
func lastAssistant(entries []protocol.Entry) *protocol.Entry {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind == "pi.assistant" {
			return &entries[i]
		}
	}
	return nil
}

// OpenFromNotification shows a chat a notification click asked for: the window
// comes forward and the chat opens, on the UI thread.
func (a *App) OpenFromNotification(chatID int) {
	if chatID <= 0 {
		return
	}
	a.apply(func() {
		if chatID == a.current {
			return
		}
		a.selectConversation(chatID)
		a.invalidate()
	})
}
