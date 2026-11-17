package app

import "testing"

// TestSyncPanesGivesTheGrowthToTheChat checks a resized window is shared out the
// way the reader expects: the panel keeps the width they gave it and the chat
// takes the difference. The panel used to grow with the window and rewrite its
// own saved width, so widening only ever made the side panel wider.
func TestSyncPanesGivesTheGrowthToTheChat(t *testing.T) {
	a := New(Config{})
	a.prefs = prefs{}
	a.sideOpen = true
	a.sideWidth = 300

	a.syncPanes(900)
	if a.mainPane != 600 {
		t.Fatalf("first layout: chat %.1f px, want 600", a.mainPane)
	}

	a.syncPanes(1200)
	if a.mainPane != 900 {
		t.Fatalf("widened to 1200: chat %.1f px, want 900 (the panel keeps its 300)", a.mainPane)
	}
	if a.prefs.SideWidth != 0 {
		t.Fatalf("widening the window saved a panel width of %.1f", a.prefs.SideWidth)
	}

	a.syncPanes(700)
	if a.mainPane != 400 {
		t.Fatalf("narrowed to 700: chat %.1f px, want 400", a.mainPane)
	}
	if a.sideWidth != 300 {
		t.Fatalf("narrowing the window changed the panel to %.1f px", a.sideWidth)
	}
}

// TestSyncPanesRemembersADraggedDivider checks the divider is what sets the
// panel's width: moving it with the window unchanged saves the new width.
func TestSyncPanesRemembersADraggedDivider(t *testing.T) {
	a := New(Config{})
	a.prefs = prefs{}
	a.sideOpen = true
	a.sideWidth = 300
	a.syncPanes(900)

	// What a drag does: the split writes the chat's new width.
	a.mainPane = 500
	a.syncPanes(900)
	if a.prefs.SideWidth != 400 {
		t.Fatalf("dragged the divider to a 400 px panel, saved %.1f", a.prefs.SideWidth)
	}

	// A window that grows after a drag hands the room to the chat, and the
	// dragged width stays.
	a.syncPanes(1100)
	if a.mainPane != 700 {
		t.Fatalf("after the drag, widening gave the chat %.1f px, want 700", a.mainPane)
	}
}

// TestSyncPanesIgnoresSubPixelJitter checks a re-layout that changes nothing
// does not save preferences on every frame.
func TestSyncPanesIgnoresSubPixelJitter(t *testing.T) {
	a := New(Config{})
	a.prefs = prefs{}
	a.sideOpen = true
	a.sideWidth = 300
	a.syncPanes(900)
	a.mainPane = 600.5
	a.syncPanes(900)
	if a.prefs.SideWidth != 0 {
		t.Fatalf("a half-pixel difference saved a panel width of %.1f", a.prefs.SideWidth)
	}
}
