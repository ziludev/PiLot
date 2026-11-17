package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestNarrowWindowKeepsTheRailUsable checks a window narrower than the saved
// rail: a 260 px rail in a 420 px window cut the header's name into slivers.
func TestNarrowWindowKeepsTheRailUsable(t *testing.T) {
	a := snapshotData(t)
	a.railWidth = 260
	a.sideOpen = false
	tt := ui.NewTester(a.View, 420, 600)
	tt.Frame()
	if a.railWidth > 420*0.45+1 {
		t.Fatalf("rail kept %.0f px in a 420 px window", a.railWidth)
	}
	if !tt.HasText("Pitta") {
		t.Fatalf("the header lost its name: %q", tt.Texts())
	}
}

// TestSidePanelYieldsToANarrowWindow checks the panel giving way instead of
// squeezing the transcript out of the window.
func TestSidePanelYieldsToANarrowWindow(t *testing.T) {
	a := snapshotData(t)
	a.railWidth = 220
	a.sideOpen = true
	a.prefs.SideWidth = 520
	tt := ui.NewTester(a.View, 900, 700)
	tt.Frame()
	tt.Frame()
	if side := 900 - a.railWidth - a.mainPane; side > 900-a.railWidth-minMainWidth+2 {
		t.Fatalf("panel kept %.0f px next to a %.0f px transcript", side, a.mainPane)
	}
}

// TestSaneFrame replaces a sliver with the default size.
func TestSaneFrame(t *testing.T) {
	if w, h := saneFrame(122, 118, defaultW, defaultH); w != defaultW || h != defaultH {
		t.Fatalf("sliver frame kept at %.0f×%.0f", w, h)
	}
	if w, h := saneFrame(1180, 820, defaultW, defaultH); w != 1180 || h != 820 {
		t.Fatalf("good frame changed to %.0f×%.0f", w, h)
	}
}
