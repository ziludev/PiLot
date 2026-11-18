package app

import "time"

// waitedSuffix words how long the oldest pending approval has been waiting, the
// number the approval bar shows and the title bar repeats where a hidden
// window can still be read.
func (a *App) waitedSuffix() string {
	if len(a.approvals) == 0 {
		return ""
	}
	oldest := a.approvals[0].CreatedAt
	for _, approval := range a.approvals[1:] {
		if approval.CreatedAt < oldest {
			oldest = approval.CreatedAt
		}
	}
	seconds := time.Now().UnixMilli() - oldest
	if seconds < 1000 {
		return ""
	}
	return itoa(int(seconds/1000)) + "s"
}

// unreadCount is how many chats hold an answer nobody has looked at yet.
func (a *App) unreadCount() int {
	n := 0
	for _, unread := range a.unread {
		if unread {
			n++
		}
	}
	return n
}

// tickTitle keeps a waiting approval's age moving in the title. Approvals are
// polled every five seconds, which would let the number sit still for that
// long; a second is what the drawer of a title expects.
func (a *App) tickTitle() {
	for range time.Tick(time.Second) {
		a.apply(func() {
			if len(a.approvals) == 0 {
				return
			}
			a.syncTitle()
		})
	}
}

// titleText is the window title: the chat, the approval wait, or the count of
// chats holding an unseen answer. A wait past five minutes blinks, because a
// title that has said "300s" for minutes stops being read.
func (a *App) titleText() string {
	title := "PiLot"
	for _, surface := range a.surfaces {
		if surface.ConversationID == a.currentValue() {
			title = "PiLot · " + surfaceTitle(surface)
			break
		}
	}
	waited := a.waitedSuffix()
	switch {
	case waited != "":
		marker := "⚠ "
		if a.waitSeconds() >= longWaitSeconds && a.titleTick%2 == 1 {
			marker = "  "
		}
		return marker + waited + " · " + title
	case a.unreadCount() > 0:
		return "(" + itoa(a.unreadCount()) + ") " + title
	}
	return title
}

// longWaitSeconds is when a wait stops looking like progress and starts looking
// like a stall.
const longWaitSeconds = 300

// waitSeconds is the age of the oldest approval in seconds.
func (a *App) waitSeconds() int {
	if len(a.approvals) == 0 {
		return 0
	}
	oldest := a.approvals[0].CreatedAt
	for _, approval := range a.approvals[1:] {
		if approval.CreatedAt < oldest {
			oldest = approval.CreatedAt
		}
	}
	return int((time.Now().UnixMilli() - oldest) / 1000)
}
