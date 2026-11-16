package notify

import (
	"log"
	"os/exec"
	"strings"
)

// Title is the notification heading the React client used for a finished
// answer.
const Title = "Agent · 回答完成"

// Sender posts one notification. main.go points it at mygo's own notification,
// which a click can bring back to the window; the fallback below is for the
// builds and platforms where that call fails, such as an unsigned app on macOS.
var Sender func(title, body string) error

// Answer posts a notification with the answer's opening words, the way the
// client notifies its owner when the window is in the background.
func Answer(text string) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return
	}
	if len([]rune(text)) > 120 {
		text = string([]rune(text)[:120])
	}
	body := text
	go func() {
		if Sender != nil {
			if err := Sender(Title, body); err == nil {
				return
			} else {
				log.Printf("notify: %v", err)
			}
		}
		script := "display notification " + quote(body) + " with title " + quote(Title)
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
