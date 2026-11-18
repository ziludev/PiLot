package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCmdArrowsPageTheTranscript checks the keys a long chat needs: ⌘↑ moves
// back a page of rows, and ⌘↓ comes back down.
func TestCmdArrowsPageTheTranscript(t *testing.T) {
	a := snapshotData(t)
	a.entries = nil
	for i := 0; i < 30; i++ {
		a.entries = append(a.entries, turnEntries(100+i, "question", "answer")...)
	}
	tt := ui.NewTester(a.View, 1180, 700)
	tt.Frame()
	tt.Frame() // the first frame measures the rows
	top, _ := a.turnList.Visible()
	tt.Key(ui.Cmd, ui.KeyUp)
	tt.Frame()
	tt.Frame()
	up, _ := a.turnList.Visible()
	if up >= top {
		t.Fatalf("the page did not move up: row %d, was %d", up, top)
	}
	if a.atBottom {
		t.Fatal("paging back left the transcript following its end")
	}
	tt.Key(ui.Cmd, ui.KeyUp)
	tt.Frame()
	tt.Frame()
	further, _ := a.turnList.Visible()
	if further >= up {
		t.Fatalf("the second page did not move further up: row %d, was %d", further, up)
	}
	tt.Key(ui.Cmd, ui.KeyDown)
	tt.Frame()
	tt.Frame()
	if down, _ := a.turnList.Visible(); down <= further {
		t.Fatalf("⌘↓ did not come back down: row %d, was %d", down, further)
	}
}
