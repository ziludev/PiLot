package app

import (
	"fmt"
	"testing"

	"pitta/internal/protocol"
)

// TestRailFiltersByWhatAChatIsDoing checks the two status views: the filter
// keeps a chat by the work it has, not only by where it came from.
func TestRailFiltersByWhatAChatIsDoing(t *testing.T) {
	a := New(Config{})
	a.surfaces = []protocol.Surface{
		{ConversationID: 1, Title: "busy", Kind: "client"},
		{ConversationID: 2, Title: "waiting", Kind: "feishu"},
		{ConversationID: 3, Title: "answered unseen", Kind: "feishu"},
		{ConversationID: 4, Title: "quiet", Kind: "client"},
	}
	a.tasks = []protocol.Task{
		{ID: 10, ConversationID: 1, State: "running"},
		{ID: 11, ConversationID: 2, State: "blocked"},
		{ID: 12, ConversationID: 4, State: "done"},
	}
	a.unread = map[int]bool{3: true}

	running, unread := railFilterIndex("running"), railFilterIndex("unread")
	client, feishu := railFilterIndex("client"), railFilterIndex("feishu")

	var shown []int
	for _, s := range a.surfaces {
		if a.railShown(s, running) {
			shown = append(shown, s.ConversationID)
		}
	}
	if len(shown) != 2 || shown[0] != 1 || shown[1] != 2 {
		t.Fatalf("running view shows %v, want [1 2]: a queued approval is work in flight too", shown)
	}
	if !a.railShown(a.surfaces[2], unread) || a.railShown(a.surfaces[0], unread) {
		t.Fatalf("unread view shows %v", shown)
	}
	// The kind views keep their meaning: they must not start filtering on state.
	if !a.railShown(a.surfaces[0], client) || a.railShown(a.surfaces[1], client) {
		t.Fatalf("client view changed meaning")
	}
	if !a.railShown(a.surfaces[1], feishu) || a.railShown(a.surfaces[0], feishu) {
		t.Fatalf("feishu view changed meaning")
	}
}

// TestRailFilterCountsMatchTheViews checks the counts the labels promise against
// what each view draws, including the archive fold and the search.
func TestRailFilterCountsMatchTheViews(t *testing.T) {
	a := New(Config{})
	a.surfaces = []protocol.Surface{
		{ConversationID: 1, Title: "deploy notes", Kind: "client"},
		{ConversationID: 2, Title: "deploy review", Kind: "feishu"},
		{ConversationID: 3, Title: "deploy old", Kind: "feishu", Archived: true},
		{ConversationID: 4, Title: "grocery list", Kind: "client"},
	}
	a.tasks = []protocol.Task{{ID: 10, ConversationID: 1, State: "waiting"}}
	a.unread = map[int]bool{2: true}

	counts := a.railFilterCounts()
	for i, key := range railFilterKeys {
		got := 0
		for _, s := range a.sortedSurfaces() {
			if a.railShown(s, i) {
				got++
			}
		}
		if counts[i] != got {
			t.Fatalf("%s: label says %d, view draws %d", key, counts[i], got)
		}
	}
	// The archived chat is out of every count until the rail shows archives,
	// and no chat waits for its owner here, so the "needs you" view is empty.
	if fmt.Sprint(counts) != "[3 2 1 1 0 1]" {
		t.Fatalf("counts = %v, want [3 2 1 1 0 1]", counts)
	}

	a.query = "deploy"
	counts = a.railFilterCounts()
	if fmt.Sprint(counts) != "[2 1 1 1 0 1]" {
		t.Fatalf("search counts = %v, want [2 1 1 1 0 1]: both matching chats counted once each", counts)
	}
}

// TestRailMarkPrefersWhatTheReaderMustDo checks the one slot a row has: a task
// that waits outranks one that runs, which outranks an answer nobody read.
func TestRailMarkPrefersWhatTheReaderMustDo(t *testing.T) {
	a := New(Config{})
	a.tasks = []protocol.Task{
		{ID: 10, ConversationID: 1, State: "running"},
		{ID: 11, ConversationID: 1, State: "blocked"},
		{ID: 12, ConversationID: 2, State: "running"},
		{ID: 13, ConversationID: 5, State: "sleeping"},
		{ID: 14, ConversationID: 6, State: "sleeping"},
	}
	a.unread = map[int]bool{1: true, 2: true, 3: true}

	cases := []struct {
		conversation int
		selected     bool
		want         railMark
	}{
		{1, false, railNeedsYouMark}, // blocked outranks running and unread
		{1, true, railNeedsYouMark},  // and the open chat still says it waits
		{2, false, railRunningMark},  // running outranks unread
		{3, false, railUnreadMark},
		{3, true, railNoMark}, // the chat on screen has been read
		{4, false, railNoMark},
		{5, false, railSleepingMark}, // parked until its clock: a clock, not a dot
		{6, true, railSleepingMark},  // and the open chat says it too
	}
	for _, c := range cases {
		if got := a.railMarkFor(c.conversation, c.selected); got != c.want {
			t.Fatalf("mark(%d, selected=%t) = %d, want %d", c.conversation, c.selected, got, c.want)
		}
	}
}

// TestRailMarksReadTaskStates checks the state words against the two lists the
// marks and the tray read, so a new state cannot silently stop showing up.
func TestRailMarksReadTaskStates(t *testing.T) {
	// The scheduler's vocabulary, as inspect() reports it.
	for _, state := range []string{"running", "completing", "waiting", "pending"} {
		if !taskInFlight(state) {
			t.Fatalf("%q should count as in flight", state)
		}
	}
	for _, state := range []string{"blocked", "faulted"} {
		if !taskNeedsYou(state) || taskInFlight(state) {
			t.Fatalf("%q should wait for its owner, not be in flight", state)
		}
	}
	for _, state := range []string{"running", "waiting", "blocked", "faulted"} {
		if !taskUnfinished(state) {
			t.Fatalf("%q should read as unfinished", state)
		}
	}
	// A sleeping task wakes on its own clock, so it is neither running nor news:
	// a reminder parked for an hour must not ring the rail's running view.
	if taskInFlight("sleeping") || taskNeedsYou("sleeping") || taskUnfinished("sleeping") {
		t.Fatal("a sleeping task should not read as work in progress")
	}
	if !taskSleeping("sleeping") || taskSleeping("running") {
		t.Fatal("taskSleeping should name the sleeping state alone")
	}
	for _, state := range []string{"done", "cancelled", ""} {
		if taskInFlight(state) || taskNeedsYou(state) || taskUnfinished(state) {
			t.Fatalf("%q should be neither running nor waiting", state)
		}
	}
}
