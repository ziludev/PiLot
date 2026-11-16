package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestSidePanelWidthFollowsTheDivider checks that the right panel is resizable
// and that the width the divider leaves is what prefs stores.
func TestSidePanelWidthFollowsTheDivider(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	a.sideOpen = true
	a.prefs.SideWidth = 300
	a.conn = ConnOpen

	tt := ui.NewTester(func(c *ui.Context) { a.pane(c) }, 1180, 820)
	tt.Frame()
	if !a.sideSynced {
		t.Fatal("the panel did not learn the room the layout gave it")
	}

	// Drag the divider left by 80 DIPs: main shrinks, the panel grows.
	a.mainPane -= 80
	tt.Frame()
	tt.Frame()
	if a.prefs.SideWidth < 360 {
		t.Fatalf("panel width after the drag = %.0f, want the widened panel", a.prefs.SideWidth)
	}
}
