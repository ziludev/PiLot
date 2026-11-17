package app

import (
	"testing"
	"time"

	"pitta/internal/notify"
	"pitta/internal/protocol"
)

// capture swaps the notification sender for a channel, so a test can see what
// would have reached the owner and how often.
func capture(t *testing.T) <-chan [3]string {
	t.Helper()
	saved := notify.Sender
	t.Cleanup(func() { notify.Sender = saved })
	posts := make(chan [3]string, 16)
	notify.Sender = func(id, title, body string) error {
		posts <- [3]string{id, title, body}
		return nil
	}
	return posts
}

// anAnswer is one finished answer, the way a watch frame hands it over.
func anAnswer(id int, text string) []protocol.Entry {
	return []protocol.Entry{{ID: id, Kind: "pi.assistant", Model: rawModel(map[string]any{
		"role": "assistant", "content": text,
	})}}
}

// TestNotifyWaitsForTheTranscriptToSettle is the rule that keeps a long turn to
// one buzz: every frame pushes the wait back, so only an answer that has fallen
// still is announced, and only once.
func TestNotifyWaitsForTheTranscriptToSettle(t *testing.T) {
	posts := capture(t)
	a := New(Config{})
	a.notifyQuiet = 40 * time.Millisecond
	a.focused = false
	a.entries = anAnswer(1, "the first answer")

	// A turn that keeps streaming: each frame re-arms the wait.
	for i := 0; i < 4; i++ {
		a.maybeNotify()
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case got := <-posts:
		t.Fatalf("notified a frame the answer was still moving in: %v", got)
	default:
	}

	select {
	case got := <-posts:
		if got[0] != notify.IDFor(0) {
			t.Fatalf("notification id = %q, want the chat it belongs to", got[0])
		}
		if got[2] != "the first answer" {
			t.Fatalf("notification body = %q", got[2])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a settled answer was never announced")
	}

	// The same answer in the next frame is not news.
	a.maybeNotify()
	time.Sleep(120 * time.Millisecond)
	select {
	case got := <-posts:
		t.Fatalf("announced the same answer twice: %v", got)
	default:
	}
}

// TestNotifyKeepsQuietWhileTheWindowIsOpen checks the reader is not buzzed for
// the answer on screen, and that a wait already armed is dropped when they come
// back before it fires.
func TestNotifyKeepsQuietWhileTheWindowIsOpen(t *testing.T) {
	posts := capture(t)
	a := New(Config{})
	a.notifyQuiet = 20 * time.Millisecond
	a.entries = anAnswer(1, "the first answer")

	a.focused = true
	a.maybeNotify()
	time.Sleep(80 * time.Millisecond)
	select {
	case got := <-posts:
		t.Fatalf("notified while the window had focus: %v", got)
	default:
	}

	// Hidden, a wait starts; the reader comes back before it fires.
	a.focused = false
	a.maybeNotify()
	a.focused = true
	a.maybeNotify()
	time.Sleep(80 * time.Millisecond)
	select {
	case got := <-posts:
		t.Fatalf("notified an answer the reader had come back to: %v", got)
	default:
	}
}

// TestNotifyGapIsPerChat keeps two chats from sharing one throttle, and stops a
// turn that keeps pausing from buzzing once per pause.
func TestNotifyGapIsPerChat(t *testing.T) {
	posts := capture(t)
	a := New(Config{})
	a.notifyQuiet = time.Millisecond
	a.focused = false

	a.entries = anAnswer(1, "the first answer")
	a.maybeNotify()
	select {
	case <-posts:
	case <-time.After(2 * time.Second):
		t.Fatal("no notification for the first answer")
	}

	// Another answer a moment later in the same chat waits for the gap.
	a.entries = anAnswer(2, "and then some more")
	a.maybeNotify()
	time.Sleep(60 * time.Millisecond)
	select {
	case got := <-posts:
		t.Fatalf("buzzed twice inside the gap: %v", got)
	default:
	}

	// Another chat is not throttled by this one.
	a.setCurrent(7)
	a.entries = anAnswer(3, "a different chat")
	a.maybeNotify()
	select {
	case got := <-posts:
		if got[0] != notify.IDFor(7) {
			t.Fatalf("notification id = %q, want chat 7", got[0])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a second chat was throttled by the first")
	}
}

// TestNotificationCarriesItsChat checks the notification names the chat it
// belongs to, so a click can open that chat rather than just the app.
func TestNotificationCarriesItsChat(t *testing.T) {
	posts := capture(t)
	a := snapshotData(t)
	a.focused = false
	a.notifyQuiet = time.Millisecond
	a.apply(func() {
		a.setCurrent(12)
		a.entries = anAnswer(900, "hello there")
	})
	a.maybeNotify()
	select {
	case got := <-posts:
		chatID, ok := notify.ChatFromID(got[0])
		if !ok || chatID != 12 {
			t.Fatalf("notification id %q names chat %d, %v", got[0], chatID, ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notification was sent")
	}
}

// TestNotificationLeadsWithTheChatName checks the title says which chat
// finished: the body alone is the same first line for every conversation.
func TestNotificationLeadsWithTheChatName(t *testing.T) {
	posts := capture(t)
	a := snapshotData(t)
	a.focused = false
	a.notifyQuiet = time.Millisecond
	a.apply(func() {
		a.setCurrent(12)
		a.entries = anAnswer(901, "hello there")
	})
	a.maybeNotify()
	select {
	case got := <-posts:
		if want := a.chatName(); want != "" && got[1] != want+" · "+notify.Title {
			t.Fatalf("notification title = %q, want it to lead with %q", got[1], want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no notification was sent")
	}
}
