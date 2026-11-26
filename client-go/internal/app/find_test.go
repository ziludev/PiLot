package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
)

// findFixture opens the transcript fixture with the bar closed, one frame drawn
// so the rows exist.
func findFixture(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	return a, tt
}

// TestFindCountsRowsAndWalksThem: a hit is a row, and the walk starts at the
// first one; the bar's own buttons move it.
func TestFindCountsRowsAndWalksThem(t *testing.T) {
	a, tt := findFixture(t)

	a.find.open = true
	a.find.query = "build"
	tt.Frame()

	// The question, the folded go build step, and the reply that says "The build
	// is green".
	if len(a.find.matches) != 3 {
		t.Fatalf("matched %v, want the three rows that say build", a.find.matches)
	}
	if a.findMark(a.find.matches[0]) != 1 || a.findMark(a.find.matches[1]) != 0 {
		t.Fatalf("walk stands in the wrong place: marks %d and %d",
			a.findMark(a.find.matches[0]), a.findMark(a.find.matches[1]))
	}
	if a.vm.threadList.FollowEnd {
		t.Fatal("the transcript still follows the end while a hit holds the view")
	}

	a.find.current = 1
	tt.Frame()
	if a.findMark(a.find.matches[0]) != 0 || a.findMark(a.find.matches[1]) != 1 {
		t.Fatal("stepping to the second hit did not move the mark")
	}
}

// TestFindReachesIntoFoldedSteps: a hit inside a tool call is still a hit, even
// though the row shows a summary until the fold opens.
func TestFindReachesIntoFoldedSteps(t *testing.T) {
	a, tt := findFixture(t)

	a.find.open = true
	a.find.query = "build ok"
	tt.Frame()

	if len(a.find.matches) != 1 {
		t.Fatalf("a folded tool call was not searched: %v", a.find.matches)
	}
	if !tt.HasText("1/1") {
		t.Fatalf("the bar did not count the hit; drew %v", tt.Texts())
	}
}

// TestFindClosesBackToTheEnd: closing the bar gives the transcript its
// follow-the-end behaviour back and forgets the walk.
func TestFindClosesBackToTheEnd(t *testing.T) {
	a, tt := findFixture(t)
	a.find.open = true
	a.find.query = "build"
	tt.Frame()

	a.ToggleFind()
	tt.Frame()
	if a.find.open || a.findBrowsing() || len(a.find.matches) != 0 {
		t.Fatalf("closing left state behind: open=%v matches=%v", a.find.open, a.find.matches)
	}
	if !a.vm.threadList.FollowEnd {
		t.Fatal("the transcript did not go back to following the end")
	}
}

// TestFindSaysNothingWhenNothingMatches keeps the bar honest: a query with no
// rows says so rather than standing on a hit that is not there.
func TestFindSaysNothingWhenNothingMatches(t *testing.T) {
	a, tt := findFixture(t)
	a.find.open = true
	a.find.query = "около"
	tt.Frame()

	if len(a.find.matches) != 0 {
		t.Fatalf("matched %v", a.find.matches)
	}
	if !tt.HasText("没有命中") {
		t.Fatalf("the bar did not say it found nothing; drew %v", tt.Texts())
	}
}

// TestFindDoesNothingWithoutAChat: the bar belongs to a conversation, so with
// none open Cmd-F is not a command.
func TestFindDoesNothingWithoutAChat(t *testing.T) {
	a := fixtureApp(t, false)
	a.active = 0
	a.ToggleFind()
	if a.find.open {
		t.Fatal("opened the bar with no chat to search")
	}
}
