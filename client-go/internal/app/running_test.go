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
	SetTrayHook(func(TrayState) { calls++ })
	t.Cleanup(func() { SetTrayHook(nil) })
	second := NewSecondary(Config{}, nil, 3)
	second.publishTray()
	primary := New(Config{})
	primary.publishTray()
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

// TestTrayApprovalsNameTheirChat checks the menu's approval rows: each pending
// call carries the chat its surface belongs to, so a click lands there.
func TestTrayApprovalsNameTheirChat(t *testing.T) {
	a := New(Config{})
	a.surfaces = []protocol.Surface{{ConversationID: 4, SurfaceID: "client:mw1"}}
	a.approvals = []protocol.PendingApproval{
		{CallID: "call_1", SurfaceID: "client:mw1", Tool: "bash", Reason: "rm -rf"},
		{CallID: "call_2", SurfaceID: "feishu:zz", Tool: "edit", Reason: "write"},
	}
	rows := a.trayApprovals()
	if len(rows) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].ConversationID != 4 || rows[0].Tool != "bash" {
		t.Fatalf("row = %+v", rows[0])
	}
	// A surface the rail does not list (a Feishu thread here) maps to no chat.
	if rows[1].ConversationID != 0 {
		t.Fatalf("row = %+v", rows[1])
	}
}
