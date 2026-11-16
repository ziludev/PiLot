package app

import (
	"testing"
	"time"

	"pitta/internal/protocol"
)

// TestApplyWaitRunsInlineWithoutWindow pins the headless path every test takes:
// without a window there is no main loop to hop to, so both hand-offs run fn
// on the calling goroutine.
func TestApplyWaitRunsInlineWithoutWindow(t *testing.T) {
	a := &App{}
	ran := false
	a.applyWait(func() { ran = true })
	if !ran {
		t.Fatal("applyWait() returned before running fn without a window")
	}
	ran = false
	a.apply(func() { ran = true })
	if !ran {
		t.Fatal("apply() returned before running fn without a window")
	}
}

// TestSelectConversationFromHandlerDoesNotBlock replays the freeze that shipped
// once: a rail row's OnClick called selectConversation, which handed the state
// write to the main thread and waited for it — on the main thread. The window
// then showed the "not responding" spinner and never recovered.
//
// With a window the wait goes through mygo.RunOnMain, which runs the function
// directly on the main thread instead of queueing it. There is no window here,
// so this test pins the shape of the path: the click handler returns, and the
// state write is visible afterwards.
func TestSelectConversationFromHandlerDoesNotBlock(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	a.surfaces = []protocol.Surface{
		{ConversationID: 5, SurfaceID: "s5"},
		{ConversationID: 9, SurfaceID: "s9"},
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		// The rail row's handler body, minus the widget.
		a.selectConversation(9)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("selectConversation blocked; the click handler would freeze the window")
	}
	if got := a.currentValue(); got != 9 {
		t.Fatalf("current = %d, want 9", got)
	}
}

func TestDefaultConversationID(t *testing.T) {
	surfaces := []protocol.Surface{
		{ConversationID: 5},
		{ConversationID: 9, Archived: true},
		{ConversationID: 7},
		{ConversationID: 0},
	}
	if got := defaultConversationID(surfaces); got != 7 {
		t.Fatalf("defaultConversationID = %d, want 7", got)
	}
	if got := defaultConversationID(nil); got != 0 {
		t.Fatalf("defaultConversationID(nil) = %d, want 0", got)
	}
}
