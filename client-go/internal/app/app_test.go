package app

import (
	"testing"

	"pitta/internal/protocol"
)

// TestOlderPageMergesWithoutDuplicates: the page read backwards joins the
// entries already held, an overlapping id is kept once, and the bridge's word
// on what is older than the page replaces the old one.
func TestOlderPageMergesWithoutDuplicates(t *testing.T) {
	a := newApp(Config{})
	th := a.thread(5)
	th.loaded = true
	th.hasMore = true
	th.loadingOlder = true
	th.entries = []protocol.Entry{{ID: 10}, {ID: 11}}
	a.applyOlder(&olderResult{convID: 5, entries: []protocol.Entry{{ID: 8}, {ID: 9}, {ID: 10}}, hasMore: false})
	if len(th.entries) != 4 {
		t.Fatalf("entries %d, want 4 (8, 9, 10, 11)", len(th.entries))
	}
	seen := map[int]int{}
	for _, entry := range th.entries {
		seen[entry.ID]++
	}
	if seen[10] != 1 {
		t.Fatalf("entry 10 kept %d times", seen[10])
	}
	if th.hasMore || th.loadingOlder {
		t.Fatalf("hasMore %v loadingOlder %v after the last page", th.hasMore, th.loadingOlder)
	}
}
