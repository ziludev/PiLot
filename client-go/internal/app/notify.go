package app

import (
	"strings"

	"pitta/internal/notify"
	"pitta/internal/protocol"
	"pitta/internal/turns"
)

// maybeNotify posts a desktop notification when a new answer finished while
// the window is in the background. It reads the same last assistant entry the
// React transcript watched.
func (a *App) maybeNotify() {
	a.apply(func() {
		if a.focused {
			return
		}
		answer := lastAssistant(a.entries)
		if answer == nil || a.notifiedID == itoa(answer.ID) {
			return
		}
		text := strings.TrimSpace(turns.Text(*answer))
		if text == "" {
			return
		}
		a.notifiedID = itoa(answer.ID)
		notify.Answer(text)
	})
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
