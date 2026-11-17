package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestSettingsFitsASmallWindow checks the panel survives a window shorter than
// its content: the closing button stays on screen and the preferences that no
// longer fit are reachable by scrolling instead of being cut off. The key list
// used to sit in a fixed 220 px scroll inside a non-scrolling panel, so at the
// smallest window the panel's own height cap sliced the list and the button.
func TestSettingsFitsASmallWindow(t *testing.T) {
	const width, height = 760, 400
	a := snapshotData(t)
	a.apply(func() { a.settingsOpen = true; a.invalidate() })
	tt := ui.NewTester(a.View, width, height)
	tt.Frame()

	cancel, ok := tt.Find("Cancel")
	if !ok || cancel.W == 0 || cancel.H == 0 {
		t.Fatalf("the panel's closing button is not drawn at %dx%d", width, height)
	}
	if cancel.Y+cancel.H > height {
		t.Fatalf("the closing button ends at y=%.1f, past the window's %d", cancel.Y+cancel.H, height)
	}
	if _, ok := tt.Find(settingsTitle); !ok {
		t.Fatalf("the panel's title is not drawn")
	}

	// The last row of the key list is below the panel's room; a scroll over the
	// panel has to bring it in.
	keys := shortcuts()
	last := keys[len(keys)-1].Action
	if row, ok := tt.Find(last); ok && row.W > 0 {
		t.Fatalf("%q is on screen in a %d px window; the test needs a shorter one", last, height)
	}
	tt.Move(width/2, height/2)
	tt.Frame()
	tt.Scroll(width/2, height/2, 0, 400)
	tt.Frame()
	row, ok := tt.Find(last)
	if !ok {
		t.Fatalf("%q is not in the panel's tree", last)
	}
	if row.W == 0 || row.H == 0 {
		t.Fatalf("scrolling the panel does not reach %q", last)
	}
	if row.Y < 0 || row.Y+row.H > height {
		t.Fatalf("%q scrolled to y=%.1f (+%.1f), outside the %d px window", last, row.Y, row.H, height)
	}
}

// TestSettingsKeepsTheFooterStill checks the closing button does not scroll with
// the body: it is the panel's own, not the last entry of the list.
func TestSettingsKeepsTheFooterStill(t *testing.T) {
	const width, height = 760, 400
	a := snapshotData(t)
	a.apply(func() { a.settingsOpen = true; a.invalidate() })
	tt := ui.NewTester(a.View, width, height)
	tt.Frame()
	before, ok := tt.Find("Cancel")
	if !ok {
		t.Fatal("the panel's closing button is not drawn")
	}
	tt.Move(width/2, height/2)
	tt.Frame()
	tt.Scroll(width/2, height/2, 0, 400)
	tt.Frame()
	after, ok := tt.Find("Cancel")
	if !ok {
		t.Fatal("the closing button left the panel while scrolling")
	}
	if before.Y != after.Y {
		t.Fatalf("the closing button moved from y=%.1f to y=%.1f while the body scrolled", before.Y, after.Y)
	}
}

// TestSettingsBodyFillsATallWindow checks the panel does not reserve scroll
// space it does not need: in a tall window the key list ends inside the panel.
func TestSettingsBodyFillsATallWindow(t *testing.T) {
	a := snapshotData(t)
	a.apply(func() { a.settingsOpen = true; a.invalidate() })
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	keys := shortcuts()
	last, ok := tt.Find(keys[len(keys)-1].Action)
	if !ok || last.W == 0 {
		t.Fatalf("the last shortcut is not drawn in a 820 px window")
	}
	cancel, ok := tt.Find("Cancel")
	if !ok {
		t.Fatal("the closing button is not drawn")
	}
	if last.Y+last.H > cancel.Y {
		t.Fatalf("the key list runs into the footer: last row ends at %.1f, footer at %.1f", last.Y+last.H, cancel.Y)
	}
}
