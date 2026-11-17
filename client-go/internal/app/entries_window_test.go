package app

import (
	"fmt"
	"testing"

	"pitta/internal/protocol"
)

// counters builds entries with the given IDs, the way the bridge numbers them.
func counters(ids ...int) []protocol.Entry {
	entries := make([]protocol.Entry, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, protocol.Entry{ID: id, Kind: "pi.assistant"})
	}
	return entries
}

func ids(entries []protocol.Entry) string {
	out := ""
	for i, entry := range entries {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprint(entry.ID)
	}
	return out
}

// The bridge's watch window holds the newest 50 entries, so a frame after a
// 200-entry load used to shrink the chat back to its tail and leave nothing to
// scroll. What a chat already holds must survive.
func TestGrowEntriesKeepsWhatTheWindowDropped(t *testing.T) {
	a := &App{atBottom: true}
	a.growEntries(counters(1, 2, 3, 4, 5))
	if got := ids(a.entries); got != "1,2,3,4,5" {
		t.Fatalf("first frame kept %s", got)
	}
	a.growEntries(counters(4, 5, 6))
	if got := ids(a.entries); got != "1,2,3,4,5,6" {
		t.Fatalf("a window that dropped the head left %s", got)
	}
	// A window reaching back to the oldest entry held is a full reload.
	a.growEntries(counters(1, 2, 3))
	if got := ids(a.entries); got != "1,2,3" {
		t.Fatalf("a full reload left %s", got)
	}
	// An empty window says nothing about the transcript.
	a.growEntries(nil)
	if got := ids(a.entries); got != "1,2,3" {
		t.Fatalf("an empty window cleared the chat to %s", got)
	}
}

// Entries arriving while the reader is scrolled away still count as unseen.
func TestGrowEntriesCountsWhatTheWindowAdds(t *testing.T) {
	a := &App{atBottom: false}
	a.entries = counters(1, 2, 3)
	a.growEntries(counters(3, 4, 5))
	if a.newMessages != 2 {
		t.Fatalf("a window adding two entries counted %d unseen", a.newMessages)
	}
}
