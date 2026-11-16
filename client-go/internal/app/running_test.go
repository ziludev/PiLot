package app

import (
	"testing"
	"time"

	"pitta/internal/protocol"
)

// TestRunningChatsListsBusyOnes checks what the tray shows: one row per busy
// chat, in the rail's wording, and nothing for the finished ones.
func TestRunningChatsListsBusyOnes(t *testing.T) {
	a := New(Config{})
	a.surfaces = []protocol.Surface{
		{ConversationID: 5, Title: "deploy notes"},
		{ConversationID: 6, Title: "done already"},
	}
	a.tasks = []protocol.Task{
		{ID: 1, ConversationID: 5, State: "running"},
		{ID: 2, ConversationID: 5, State: "running"}, // one row per chat
		{ID: 3, ConversationID: 6, State: "done"},
	}
	running := a.runningChats()
	if len(running) != 1 {
		t.Fatalf("running chats = %+v", running)
	}
	if running[0].ID != 5 || running[0].Title != "deploy notes" || running[0].State != "running" {
		t.Fatalf("row = %+v", running[0])
	}
}

// TestRunningHookSkipsSecondWindows checks the tray belongs to one window.
func TestRunningHookSkipsSecondWindows(t *testing.T) {
	calls := 0
	SetRunningHook(func([]RunningChat) { calls++ })
	t.Cleanup(func() { SetRunningHook(nil) })
	second := NewSecondary(Config{}, nil, 3)
	second.publishRunning()
	primary := New(Config{})
	primary.publishRunning()
	// publishRunning hands the list over in a goroutine; the second window
	// must not add a call.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && calls == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	if calls != 1 {
		t.Fatalf("hook ran %d times, want once (second windows stay quiet)", calls)
	}
}
