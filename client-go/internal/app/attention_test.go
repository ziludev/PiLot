package app

import (
	"testing"

	"pitta/internal/protocol"
)

// TestAttentionSoundsOncePerWait checks the alert fires for a new approval,
// stays quiet on the next poll, and is told when the wait is over.
func TestAttentionSoundsOncePerWait(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	var calls []bool
	SetAttention(func(on bool) { calls = append(calls, on) })
	t.Cleanup(func() { SetAttention(nil) })

	first := []protocol.PendingApproval{{CallID: "call_1", Tool: "bash"}}
	a.apply(func() { a.notifyAttention(first) })
	if len(calls) != 1 || !calls[0] {
		t.Fatalf("first wait sounded %v, want one true", calls)
	}
	a.apply(func() { a.notifyAttention(first) })
	if len(calls) != 1 {
		t.Fatalf("the second poll sounded again: %v", calls)
	}
	a.apply(func() { a.notifyAttention(nil) })
	if len(calls) != 2 || calls[1] {
		t.Fatalf("the end of the wait was not reported: %v", calls)
	}
}
