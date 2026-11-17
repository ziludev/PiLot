package app

import (
	"encoding/json"
	"testing"

	"pitta/internal/protocol"
)

// Grouping parses every entry, so a frame that only scrolls must reuse what the
// last change grouped.
func TestGroupedTurnsGroupsOncePerChange(t *testing.T) {
	a := snapshotData(t)
	a.entries = []protocol.Entry{{
		ID:    1,
		Kind:  "pi.user",
		Model: json.RawMessage(`[{"role":"user","content":"why is the sky blue"}]`),
	}}
	a.turnStale = true
	first := a.groupedTurns()
	if len(first) != 1 {
		t.Fatalf("grouped %d turns, want 1", len(first))
	}
	if again := a.groupedTurns(); &again[0] != &first[0] {
		t.Fatal("grouping ran again without a change")
	}
	a.growEntries([]protocol.Entry{{
		ID:    2,
		Kind:  "pi.assistant",
		Model: json.RawMessage(`[{"role":"assistant","content":[{"type":"text","text":"because"}]}]`),
	}})
	if next := a.groupedTurns(); &next[0] == &first[0] {
		t.Fatal("grouping did not run after the entries changed")
	}
}
