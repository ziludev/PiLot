package notify

import (
	"log"
	"os/exec"
	"strings"
)

// Title is the notification heading the React client used for a finished
// answer.
const Title = "Agent · 回答完成"

// Answer posts a macOS notification with the answer's opening words, the way
// the client notifies its owner when the window is in the background.
//
// mygo v0.6.4 has no notification API of its own, so this asks osascript; a
// failure is logged and never blocks the UI, since a missing notification must
// not cost the reader the answer itself.
func Answer(text string) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return
	}
	if len([]rune(text)) > 120 {
		text = string([]rune(text)[:120])
	}
	go func() {
		script := "display notification " + quote(text) + " with title " + quote(Title)
		if err := exec.Command("osascript", "-e", script).Run(); err != nil {
			log.Printf("notify: osascript: %v", err)
		}
	}()
}

// quote wraps a string for an AppleScript literal, escaping the two characters
// that end it.
func quote(text string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(text) + `"`
}
