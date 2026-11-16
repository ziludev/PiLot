package app

import (
	"testing"
	"time"

	"pitta/internal/notify"
	"pitta/internal/protocol"
)

// TestMaybeNotifyOnlyWhenHidden checks the rule the React transcript followed:
// nothing is posted while the window has focus, one answer is posted once, and
// an empty answer is never posted.
func TestMaybeNotifyOnlyWhenHidden(t *testing.T) {
	a := New(Config{})
	a.entries = []protocol.Entry{
		{ID: 1, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "content": "the first answer",
		})},
	}

	a.focused = true
	a.maybeNotify()
	if a.notifiedID != "" {
		t.Fatalf("notified while focused: %q", a.notifiedID)
	}

	a.focused = false
	a.maybeNotify()
	if a.notifiedID != "1" {
		t.Fatalf("notifiedID = %q, want 1", a.notifiedID)
	}

	// The same answer arriving again in the next frame must not re-notify.
	a.notifiedID = "1"
	a.maybeNotify()
	if a.notifiedID != "1" {
		t.Fatalf("notified twice: %q", a.notifiedID)
	}

	a.entries = append(a.entries, protocol.Entry{ID: 2, Kind: "pi.assistant", Model: rawModel(map[string]any{
		"role": "assistant", "content": "   ",
	})})
	a.maybeNotify()
	if a.notifiedID != "1" {
		t.Fatalf("notified an empty answer: %q", a.notifiedID)
	}
}

// TestNotificationCarriesItsChat checks the notification names the chat it
// belongs to, so a click can open that chat rather than just the app.
func TestNotificationCarriesItsChat(t *testing.T) {
	saved := notify.Sender
	defer func() { notify.Sender = saved }()
	sent := make(chan string, 1)
	notify.Sender = func(id, title, body string) error {
		sent <- id
		return nil
	}
	a := snapshotData(t)
	a.focused = false
	a.apply(func() {
		a.current = 12
		a.entries = []protocol.Entry{{ID: 900, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "content": "hello there",
		})}}
		a.notifiedID = ""
	})
	a.maybeNotify()
	select {
	case id := <-sent:
		chatID, ok := notify.ChatFromID(id)
		if !ok || chatID != 12 {
			t.Fatalf("notification id %q names chat %d, %v", id, chatID, ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notification was sent")
	}
}
