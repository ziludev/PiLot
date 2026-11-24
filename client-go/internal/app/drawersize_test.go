package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo"
)

// fakeWindow records what the app asked of it, so growth and give-back can be
// checked without a display.
type fakeWindow struct {
	width, height int
	bounds        mygo.Rectangle
}

func (f *fakeWindow) Size() (int, int)          { return f.width, f.height }
func (f *fakeWindow) SetSize(width, height int) { f.width, f.height = width, height }
func (f *fakeWindow) Bounds() mygo.Rectangle    { return f.bounds }

// TestDrawerGrowthGivesTheWidthBack: opening a drawer widens the window by what
// the drawer claims, and closing it takes exactly that back — not more, which is
// how a window ends up narrower than the person left it.
func TestDrawerGrowthGivesTheWidthBack(t *testing.T) {
	win := &fakeWindow{width: 1180, height: 820, bounds: mygo.Rectangle{X: 0, Y: 0, Width: 1180, Height: 820}}
	restore := callerWindow
	callerWindow = func(context.Context) windowSizer { return win }
	restoreArea := workArea
	workArea = func(mygo.Rectangle) float32 { return 3000 }
	defer func() { callerWindow = restore; workArea = restoreArea }()

	a := changesFixture(t)
	// The changes fixture opens its drawer; the growth is measured from closed.
	a.changes.open = false
	a.fitWindowToDrawer()
	if win.width != 1180 {
		t.Fatalf("nothing is open, but the window is %d wide", win.width)
	}
	a.toggleChanges()
	a.fitWindowToDrawer()
	if want := 1180 + int(changesWidth+paneInset); win.width != want {
		t.Fatalf("opening 改动 gave %d, wanted %d", win.width, want)
	}
	a.toggleChanges()
	a.fitWindowToDrawer()
	if win.width != 1180 {
		t.Fatalf("closing 改动 left the window at %d, wanted 1180", win.width)
	}
}

// TestDrawerGrowthStopsAtTheDisplay: a window near the right edge stops at it
// rather than sliding under the screen, and the amount it did grow is what a
// close gives back.
func TestDrawerGrowthStopsAtTheDisplay(t *testing.T) {
	win := &fakeWindow{width: 1300, height: 820, bounds: mygo.Rectangle{X: 0, Y: 0, Width: 1300, Height: 820}}
	restore := callerWindow
	callerWindow = func(context.Context) windowSizer { return win }
	restoreArea := workArea
	workArea = func(mygo.Rectangle) float32 { return 1440 }
	defer func() { callerWindow = restore; workArea = restoreArea }()

	a := changesFixture(t)
	a.changes.open = false
	a.toggleChanges()
	a.fitWindowToDrawer()
	if want := 1440 - 24; win.width != want {
		t.Fatalf("opening 改动 gave %d, wanted the display limit %d", win.width, want)
	}
	a.toggleChanges()
	a.fitWindowToDrawer()
	if win.width != 1300 {
		t.Fatalf("closing 改动 left the window at %d, wanted 1300", win.width)
	}
}
