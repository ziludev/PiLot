package notify

import "testing"

// TestChatFromID covers the id a click delivers: a chat id survives the round
// trip, and an id nobody minted does not.
func TestChatFromID(t *testing.T) {
	if got := IDFor(42); got != "chat-42" {
		t.Fatalf("IDFor(42) = %q", got)
	}
	if id, ok := ChatFromID(IDFor(9753)); !ok || id != 9753 {
		t.Fatalf("round trip gave %d, %v", id, ok)
	}
	for _, bad := range []string{"", "answer", "chat-", "chat-x", "42"} {
		if _, ok := ChatFromID(bad); ok {
			t.Fatalf("%q was read as a chat", bad)
		}
	}
}
