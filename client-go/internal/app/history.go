package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
)

// historyLimit is how many sent messages the composer can recall. A chat client
// needs the last handful; a scrollback of hundreds just gets in the way of the
// Up arrow.
const historyLimit = 50

// recall remembers what was sent, the way a shell remembers its commands: Up
// walks back through it, Down returns towards the draft that was being typed.
// The stack lives in memory only — a draft from yesterday is not a draft.
func (a *App) recall(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(a.history) > 0 && a.history[len(a.history)-1] == text {
		// Sending the same line twice should not cost two Ups.
		return
	}
	a.history = append(a.history, text)
	if len(a.history) > historyLimit {
		a.history = a.history[len(a.history)-historyLimit:]
	}
	a.historyAt = 0
	a.historyDraft = ""
}

// recallOlder steps back one message and returns the text to put in the editor.
// The first step stashes whatever was being typed, so Down can bring it back.
func (a *App) recallOlder(current string) (string, bool) {
	if len(a.history) == 0 {
		return "", false
	}
	if a.historyAt == 0 {
		a.historyDraft = current
	}
	if a.historyAt >= len(a.history) {
		return "", false
	}
	a.historyAt++
	return a.history[len(a.history)-a.historyAt], true
}

// recallNewer steps back towards the draft, returning it once the walk is done.
func (a *App) recallNewer() (string, bool) {
	if a.historyAt == 0 {
		return "", false
	}
	a.historyAt--
	if a.historyAt == 0 {
		draft := a.historyDraft
		a.historyDraft = ""
		return draft, true
	}
	return a.history[len(a.history)-a.historyAt], true
}

// recallKey handles Up and Down in the composer. A message with line breaks
// keeps the arrows for moving the caret: there, Up means the line above.
func (a *App) recallKey(k ui.TextKey, text string) bool {
	if strings.Contains(text, "\n") {
		return false
	}
	switch k.Key {
	case ui.KeyUp:
		next, ok := a.recallOlder(text)
		if !ok {
			return false
		}
		k.SetText(next)
		return true
	case ui.KeyDown:
		next, ok := a.recallNewer()
		if !ok {
			return false
		}
		k.SetText(next)
		return true
	}
	return false
}
