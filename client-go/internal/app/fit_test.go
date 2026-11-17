package app

import "testing"

// TestFitPanesKeepsTheChatReadable checks the three columns share a narrow
// window without the chat falling below the width it needs. The panel used to
// keep a 240 px minimum even when the window could not hold it, so at the
// smallest window the transcript was left with 220 px.
func TestFitPanesKeepsTheChatReadable(t *testing.T) {
	for _, width := range []float32{640, 700, 760, 900, 1180, 1600} {
		for _, side := range []bool{false, true} {
			for _, savedRail := range []float32{180, 260, 345} {
				a := New(Config{})
				a.prefs = prefs{}
				a.sideOpen = side
				a.railWidth = savedRail
				a.sideSynced = true
				a.mainPane = width - savedRail - 300
				a.fitPanes(width)
				sideWidth := float32(0)
				if side {
					sideWidth = width - a.railWidth - a.mainPane
				}
				main := width - a.railWidth - sideWidth
				if main < minMainWidth-0.01 {
					t.Fatalf("width %.0f side=%v rail %.0f: the chat got %.1f px, want at least %d", width, side, a.railWidth, main, minMainWidth)
				}
				if a.railWidth < minRailWidth-0.01 {
					t.Fatalf("width %.0f: the rail got %.1f px, want at least %d", width, a.railWidth, minRailWidth)
				}
				if side && sideWidth <= 0 {
					t.Fatalf("width %.0f rail %.0f: the panel was squeezed out of the window", width, a.railWidth)
				}
			}
		}
	}
}

// TestFitPanesLeavesATallWindowAlone checks a window with room to spare keeps
// the widths it was given.
func TestFitPanesLeavesATallWindowAlone(t *testing.T) {
	a := New(Config{})
	a.prefs = prefs{}
	a.sideOpen = true
	a.railWidth = 260
	a.sideSynced = true
	a.mainPane = 1180 - 260 - 300
	a.fitPanes(1180)
	if a.railWidth != 260 {
		t.Fatalf("rail moved to %.1f px in a window with room for it", a.railWidth)
	}
	if side := 1180 - a.railWidth - a.mainPane; side != 300 {
		t.Fatalf("panel moved to %.1f px in a window with room for it", side)
	}
}
