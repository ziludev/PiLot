package app

import (
	"testing"
	"time"

	"pitta/internal/protocol"
)

// slowPost models win.Update: the update lands on another thread, later.
func slowPost(delay time.Duration) func(func()) {
	return func(fn func()) {
		go func() {
			time.Sleep(delay)
			fn()
		}()
	}
}

// TestApplyDoesNotWait pins the reason applyWait exists: a queued update has
// not run when apply returns, so state it writes cannot be read back.
func TestApplyDoesNotWait(t *testing.T) {
	a := &App{}
	a.postFn = slowPost(20 * time.Millisecond)
	ran := false
	a.apply(func() { ran = true })
	if ran {
		t.Fatal("apply returned after running fn; it should only queue it")
	}
	time.Sleep(50 * time.Millisecond)
	if !ran {
		t.Fatal("the queued update never ran")
	}
}

// TestApplyWaitWaits is the fix: the caller sees the state the update wrote.
func TestApplyWaitWaits(t *testing.T) {
	a := &App{}
	a.postFn = slowPost(20 * time.Millisecond)
	ran := false
	a.applyWait(func() { ran = true })
	if !ran {
		t.Fatal("applyWait returned before fn ran")
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
