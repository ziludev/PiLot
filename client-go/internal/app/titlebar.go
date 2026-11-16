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
