package app

import (
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
)

// TestSearchHitsHugAShortList checks three hits take only the room three hits
// need. The list had a fixed 140 px height, which left dead rail between the
// last hit and the views below it.
func TestSearchHitsHugAShortList(t *testing.T) {
	short := railWithHits(t, 3)
	shortHeight := searchHitsBox(t, short)
	if shortHeight >= searchHitsHeight {
		t.Fatalf("three hits take %.1f px, want less than %d", shortHeight, searchHitsHeight)
	}

	long := railWithHits(t, 20)
	longHeight := searchHitsBox(t, long)
	if longHeight > searchHitsHeight {
		t.Fatalf("twenty hits take %.1f px, want at most %d", longHeight, searchHitsHeight)
	}
	if longHeight <= shortHeight {
		t.Fatalf("twenty hits take %.1f px, no more than three (%.1f)", longHeight, shortHeight)
	}
}

// TestSearchHitsScrollPastTheCap checks a long list is not simply cut off: the
// last hit is reachable inside the capped box.
func TestSearchHitsScrollPastTheCap(t *testing.T) {
	a := railWithHits(t, 20)
	tt := ui.NewTester(a.View, 210, 700)
	tt.Frame()
	last := "snippet 19"
	if row, ok := tt.Find(last); ok && row.W > 0 {
		t.Fatalf("%q is visible without scrolling", last)
	}
	// Where the box sits is not fixed: the rail's top area can move it down.
	box, ok := tt.Find(searchHitsLabel)
	if !ok {
		t.Fatal("the results box is not drawn")
	}
	x, y := box.X+box.W/2, box.Y+box.H/2
	tt.Move(x, y)
	tt.Frame()
	tt.Scroll(x, y, 0, 800)
	tt.Frame()
	row, ok := tt.Find(last)
	if !ok || row.W == 0 {
		t.Fatalf("scrolling the results does not reach %q", last)
	}
}

// railWithHits builds a rail whose search returned n hits.
func railWithHits(t *testing.T, n int) *App {
	t.Helper()
	a := snapshotData(t)
	a.query = "deploy"
	hits := make([]protocol.SearchHit, 0, n)
	for i := 0; i < n; i++ {
		hits = append(hits, protocol.SearchHit{
			ConversationID: 20 + i,
			Kind:           "pi.message",
			Snippet:        "snippet " + strconv.Itoa(i),
		})
	}
	a.searchHits = hits
	return a
}

// searchHitsBox measures the scrolling box the hits are drawn in.
func searchHitsBox(t *testing.T, a *App) float32 {
	t.Helper()
	tt := ui.NewTester(a.View, 210, 700)
	tt.Frame()
	box, ok := tt.Find(searchHitsLabel)
	if !ok {
		t.Fatalf("the results box is not drawn")
	}
	return box.H
}
