package app

import (
	"testing"

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
