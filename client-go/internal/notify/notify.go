package notify

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
)

// Title is the notification heading the earlier web client used for a finished
// answer.
const Title = "Agent · 回答完成"

// Sender posts one notification. main.go points it at mygo's own notification,
// which a click can bring back to the window; the fallback below is for the
// builds and platforms where that call fails, such as an unsigned app on macOS.
// The id names the chat, so a click can land on the answer it was about.
var Sender func(id, title, body string) error

// IDFor is the notification id of a chat. main.go reads the number back out of
// the id a click delivers.
func IDFor(chatID int) string { return "chat-" + strconv.Itoa(chatID) }

// ChatFromID is the chat a notification id names, and whether it named one.
func ChatFromID(id string) (int, bool) {
	rest, ok := strings.CutPrefix(id, "chat-")
	if !ok {
		return 0, false
	}
	chatID, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return chatID, true
}

// Answer posts a notification with the answer's opening words, the way the
// client notifies its owner when the window is in the background.
func Answer(text string, chatID int) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return
	}
	if len([]rune(text)) > 120 {
		text = string([]rune(text)[:120])
	}
	body := text
	id := IDFor(chatID)
	go func() {
		if Sender != nil {
			if err := Sender(id, Title, body); err == nil {
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
