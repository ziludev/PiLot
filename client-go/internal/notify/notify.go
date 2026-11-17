package notify

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Title is what a notification says happened.
const Title = "回答完成"

// Agent is what leads the title when the chat has no name to lead with.
const Agent = "Agent"

// nameLimit is how much of the chat's name the title carries before it is cut:
// macOS clips a long title anyway, and a cut reads better than a clipped one.
const nameLimit = 24

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
// client notifies its owner when the window is in the background. The chat's
// name leads the title, so two notifications are told apart at a glance.
func Answer(chatName, text string, chatID int) {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return
	}
	if len([]rune(text)) > 120 {
		text = string([]rune(text)[:120])
	}
	title := Agent + " · " + Title
	if name := strings.TrimSpace(chatName); name != "" && name != Agent {
		title = clip(name, nameLimit) + " · " + Title
	}
	body := text
	id := IDFor(chatID)
	go func() {
		if Sender != nil {
			if err := Sender(id, title, body); err == nil {
				return
			} else {
				log.Printf("notify: %v", err)
				record(err)
			}
		}
		script := "display notification " + quote(body) + " with title " + quote(title)
		if err := exec.Command("osascript", "-e", script).Run(); err != nil {
			log.Printf("notify: osascript: %v", err)
		}
	}()
}

// clip shortens text to n runes, marking the cut.
func clip(text string, n int) string {
	runes := []rune(text)
	if len(runes) <= n {
		return text
	}
	return string(runes[:n]) + "…"
}

// record keeps the reason the native notification failed. A windowed app has no
// stderr to read, and the fallback it takes next is the reason the notification
// arrives under a generic icon, so the cause has to survive somewhere.
func record(err error) {
	dir, derr := os.UserConfigDir()
	if derr != nil {
		return
	}
	path := filepath.Join(dir, "pitta", "notify.log")
	if mkdirErr := os.MkdirAll(filepath.Dir(path), 0o755); mkdirErr != nil {
		return
	}
	f, ferr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if ferr != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %v\n", time.Now().Format(time.RFC3339), err)
}

// quote wraps a string for an AppleScript literal, escaping the two characters
// that end it.
func quote(text string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + replacer.Replace(text) + `"`
}
