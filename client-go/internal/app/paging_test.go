package app

import (
	"testing"

	"pitta/internal/protocol"
)

// TestPrependOlderDropsWhatTheClientAlreadyHas: a frame can land while the page
// is in flight, so the page and the transcript overlap — the overlap must not
// draw twice, and the order must stay the chat's.
func TestPrependOlderDropsWhatTheClientAlreadyHas(t *testing.T) {
	held := []protocol.Entry{{ID: 10}, {ID: 11}, {ID: 12}}
	older := []protocol.Entry{{ID: 7}, {ID: 8}, {ID: 9}, {ID: 10}, {ID: 11}}

	merged, added := prependOlder(held, older)
	if added != 3 {
		t.Fatalf("added %d entries, want 3", added)
	}
	want := []int{7, 8, 9, 10, 11, 12}
	if len(merged) != len(want) {
		t.Fatalf("merged %d entries, want %d: %v", len(merged), len(want), merged)
	}
	for i, id := range want {
		if merged[i].ID != id {
			t.Fatalf("entry %d has id %d, want %d", i, merged[i].ID, id)
		}
	}

	// A page the transcript already covers adds nothing and keeps what it had.
	again, added := prependOlder(merged, older)
	if added != 0 || len(again) != len(merged) {
		t.Fatalf("replaying a page added %d entries (%d total)", added, len(again))
	}
}

// TestOldestEntryIDIsWhatTheNextPageEndsBefore is the cursor's only source.
func TestOldestEntryIDIsWhatTheNextPageEndsBefore(t *testing.T) {
	a := transcriptApp(turnEntries(40, "hi", "hello")...)
	if got := a.oldestEntryID(); got != 40 {
		t.Fatalf("oldest entry %d, want 40", got)
	}
	a.entries = nil
	if got := a.oldestEntryID(); got != 0 {
		t.Fatalf("an empty chat ends before %d, want 0", got)
	}
}
