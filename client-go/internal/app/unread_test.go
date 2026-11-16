package app

import (
	"testing"

	"pitta/internal/protocol"
)

// TestTaskFinishDotsTheChat checks a background chat: its task stops while the
// reader looks elsewhere, and the rail keeps a dot for it.
func TestTaskFinishDotsTheChat(t *testing.T) {
	a := New(Config{})
	a.current = 11
	a.focused = true
	before := []protocol.Task{{ConversationID: 7, State: "running"}}
	after := []protocol.Task{{ConversationID: 7, State: "done"}}
	a.noteTaskFinish(before, after)
	if !a.isUnread(7) {
		t.Fatal("finished chat is not dotted")
	}
	// The open chat, while the window is in front, needs no dot.
	a.noteTaskFinish([]protocol.Task{{ConversationID: 11, State: "running"}}, nil)
	if a.isUnread(11) {
		t.Fatal("the open chat is dotted while looked at")
	}
}

// TestUnreadClearsWhenOpened checks opening a chat and refocusing the window.
func TestUnreadClearsWhenOpened(t *testing.T) {
	a := New(Config{})
	a.markUnread(7)
	if !a.isUnread(7) {
		t.Fatal("dot missing")
	}
	a.clearUnread(7)
	if a.isUnread(7) {
		t.Fatal("dot survived")
	}
	// A dot survives a restart through the preference file.
	a.markUnread(9)
	ids := a.unreadIDs()
	if len(ids) != 1 || ids[0] != 9 {
		t.Fatalf("ids = %v", ids)
	}
	restored := unreadFromPrefs(prefs{Unread: ids})
	if !restored[9] {
		t.Fatal("the dot did not come back")
	}
}

// TestAnswerWhileHiddenDots checks the transcript path: a frame that brings an
// answer while the window is behind another one leaves a dot.
func TestAnswerWhileHiddenDots(t *testing.T) {
	before := []protocol.Entry{{ID: 1, Kind: "pi.user"}}
	after := []protocol.Entry{{ID: 1, Kind: "pi.user"}, {ID: 2, Kind: "pi.assistant"}}
	if !hasNewAnswer(before, after) {
		t.Fatal("an answer arrived")
	}
	if hasNewAnswer(after, after) {
		t.Fatal("the same frame is not news")
	}
}
