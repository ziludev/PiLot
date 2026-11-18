package app

import (
	"testing"

	"pitta/internal/protocol"
)

// TestFailedTurnMarksTheRow is the gap a dot could not close: a chat whose turn
// ended on an error and one that answered while nobody watched both end with
// something unread, so the rail said the same thing about both. The bridge reads
// the stop reason from the newest assistant entry of every chat, and the row
// says so until the chat is opened.
func TestFailedTurnMarksTheRow(t *testing.T) {
	a := railFixture(t)
	a.surfaces[1].LastStop = "error"
	// 3 answered, 3 did too: only the one that ended badly says so.
	if got := a.railMarkFor(9, false); got != railFailedMark {
		t.Fatalf("a chat whose last turn errored shows mark %d, want the failed mark", got)
	}
	if got := a.railMarkFor(3, false); got == railFailedMark {
		t.Fatalf("a chat that answered shows the failed mark")
	}

	// The mark reaches the frame: the row keeps a slot for it and draws the
	// triangle, which is not there for a chat that answered.
	failed := railTest(t, a)
	if _, ok := failed.Find(railMarkLabelPrefix + itoa(9)); !ok {
		t.Fatalf("the failed chat's row drew no mark, shows %q", failed.Texts())
	}
	a.surfaces[1].LastStop = ""
	answered := railTest(t, a)
	if _, ok := answered.Find(railMarkLabelPrefix + itoa(9)); ok {
		t.Fatalf("a chat with no failure still drew a mark")
	}

	// Opening the chat is reading the failure: the warning is there to bring the
	// reader back, not to sit on the row for days.
	a.surfaces[1].LastStop = "error"
	a.setCurrent(9)
	if got := a.railMarkFor(9, false); got == railFailedMark {
		t.Fatalf("the failure is still marked after the chat was opened")
	}
}

// TestFailedMarkYieldsToNewWork keeps the slot honest: a chat that is working
// again has moved on from the failure, so the ring wins.
func TestFailedMarkYieldsToNewWork(t *testing.T) {
	a := railFixture(t)
	a.surfaces[1].LastStop = "aborted"
	a.tasks = []protocol.Task{{ID: 4, Kind: "pi.generation", ConversationID: 9, State: "running"}}
	if got := a.railMarkFor(9, false); got != railRunningMark {
		t.Fatalf("a chat that failed and then started again shows mark %d, want the running ring", got)
	}
}
