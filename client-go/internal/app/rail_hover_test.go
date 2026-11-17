package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestRailHoverClusterLeavesTheTitleAlone checks the buttons a row shows under
// the pointer are drawn over the row instead of inside it. They used to be laid
// out in the row, so hovering a chat at a narrow rail width shortened its title
// by the whole width of the cluster.
func TestRailHoverClusterLeavesTheTitleAlone(t *testing.T) {
	const railWidth = 210.0
	a := snapshotData(t)
	row, ok := railRowRect(t, a, railWidth, "New chat #13")
	if !ok {
		t.Skip("the fixture row is not on screen")
	}
	tt := ui.NewTester(func(c *ui.Context) {
		a.railWidth = railWidth
		a.rail(c)
	}, int(railWidth), 700)
	tt.Frame()
	rest, ok := tt.Find("New chat #13")
	if !ok {
		t.Skip("the fixture row is not on screen")
	}

	tt.Move(row.X+40, row.Y+rest.H/2)
	tt.Frame()
	hovered, ok := tt.Find("New chat #13")
	if !ok {
		t.Fatal("the title left the row while hovering it")
	}
	if hovered.W != rest.W {
		t.Fatalf("the title is %.1f px wide under the pointer, %.1f at rest: the cluster is laid out in the row", hovered.W, rest.W)
	}

	cluster, ok := tt.Find(railHoverLabel)
	if !ok {
		t.Fatalf("hovering the row does not draw the cluster")
	}
	if cluster.X+cluster.W > railWidth {
		t.Fatalf("the cluster ends at x=%.1f, past the %.0f px rail", cluster.X+cluster.W, railWidth)
	}
	if cluster.X < rest.X {
		t.Fatalf("the cluster starts at x=%.1f, left of the title at %.1f", cluster.X, rest.X)
	}
}

// TestRailHoverClusterCoversTheRightEnd checks the overlay sits against the
// row's right edge, where the row's own numbers were.
func TestRailHoverClusterCoversTheRightEnd(t *testing.T) {
	const railWidth = 210.0
	a := snapshotData(t)
	tt := ui.NewTester(func(c *ui.Context) {
		a.railWidth = railWidth
		a.rail(c)
	}, int(railWidth), 700)
	tt.Frame()
	row, ok := tt.Find("New chat #13")
	if !ok {
		t.Skip("the fixture row is not on screen")
	}
	tt.Move(row.X+40, row.Y+row.H/2)
	tt.Frame()
	cluster, ok := tt.Find(railHoverLabel)
	if !ok {
		t.Fatal("hovering the row does not draw the cluster")
	}
	if gap := railWidth - (cluster.X + cluster.W); gap > 8 {
		t.Fatalf("the cluster leaves %.1f px of empty rail to its right, want it against the edge", gap)
	}
}

// railRowRect lays a rail of the given width out to find a chat's row.
func railRowRect(t *testing.T, a *App, railWidth float32, title string) (ui.Rect, bool) {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) {
		a.railWidth = railWidth
		a.rail(c)
	}, int(railWidth), 700)
	tt.Frame()
	return tt.Find(title)
}
