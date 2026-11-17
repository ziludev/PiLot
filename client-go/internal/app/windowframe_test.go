package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// laptop is the screen these tests pretend to have.
var laptop = []ui.Rect{{X: 0, Y: 0, W: 1440, H: 875}}

// TestFrameCornerRestoresAVisibleWindow checks a saved place on a connected
// screen is restored as it was.
func TestFrameCornerRestoresAVisibleWindow(t *testing.T) {
	p := prefs{WindowX: 120, WindowY: 80}
	x, y, ok := frameCorner(p, 1180, 820, laptop)
	if !ok || x != 120 || y != 80 {
		t.Fatalf("corner (120, 80) restored as (%d, %d, %v)", x, y, ok)
	}
}

// TestFrameCornerRefusesAnUnpluggedScreen checks a window last closed on a
// display that is gone is not put back where nothing is: the app centers it
// instead, which is what mygo's own state restore does.
func TestFrameCornerRefusesAnUnpluggedScreen(t *testing.T) {
	external := []ui.Rect{{X: 0, Y: 0, W: 1440, H: 875}, {X: 1440, Y: 0, W: 2560, H: 1415}}
	onExternal := prefs{WindowX: 1800, WindowY: 200}
	if _, _, ok := frameCorner(onExternal, 1180, 820, laptop); ok {
		t.Fatal("a corner on the unplugged display was restored")
	}
	if _, _, ok := frameCorner(onExternal, 1180, 820, external); !ok {
		t.Fatal("a corner on a connected display was refused")
	}
	// A sliver of the window still shows enough to grab, so it stays; less than
	// a hundred pixels is not enough to find.
	mostlyOff := prefs{WindowX: -1100, WindowY: 100}
	if _, _, ok := frameCorner(mostlyOff, 1180, 820, laptop); ok {
		t.Fatal("a window showing 80 px of itself was restored")
	}
	slightlyOff := prefs{WindowX: -80, WindowY: 100}
	if _, _, ok := frameCorner(slightlyOff, 1180, 820, laptop); !ok {
		t.Fatal("a window 80 px past the left edge was refused")
	}
}

// TestFrameCornerIgnoresAnUnplacedWindow checks the never-placed frame (0, 0)
// is left to the window manager.
func TestFrameCornerIgnoresAnUnplacedWindow(t *testing.T) {
	p := prefs{WindowX: 0, WindowY: 0}
	if _, _, ok := frameCorner(p, 1180, 820, laptop); ok {
		t.Fatal("an unplaced window was given a position")
	}
}

// TestFrameCornerHandlesNoDisplays checks a machine that reports no display at
// all does not crash and simply keeps the window manager's choice.
func TestFrameCornerHandlesNoDisplays(t *testing.T) {
	p := prefs{WindowX: 120, WindowY: 80}
	if _, _, ok := frameCorner(p, 1180, 820, nil); ok {
		t.Fatal("a window was placed with no displays connected")
	}
}
