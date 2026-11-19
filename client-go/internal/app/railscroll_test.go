package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
)

// The rail holds more chats than fit in it, which is the whole point of a list:
// it shows the ones in view and scrolls to the rest. Before this, the column
// around it sized to its content, so the list believed everything was on screen
// and the wheel had nothing to move.
func TestRailListScrolls(t *testing.T) {
	a := newApp(Config{})
	a.now = time.Now()
	a.online = true
	surfaces := make([]protocol.Surface, 120)
	for i := range surfaces {
		surfaces[i] = protocol.Surface{
			SurfaceID:      fmt.Sprintf("client:%d", i+1),
			ConversationID: i + 1,
			Kind:           "client",
			Title:          fmt.Sprintf("Chat %d", i+1),
		}
	}
	// The section is the user's to open; without that the rail would draw one
	// folded heading and nothing to scroll, which is the point of the next test.
	a.prefs.Sections = map[string]bool{sectionDirect: true}
	a.setSurfaces(surfaces)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	first, last := a.vm.railList.Visible()
	if last-first+1 >= len(surfaces) {
		t.Fatalf("rail shows %d of %d rows: nothing left to scroll", last-first+1, len(surfaces))
	}
	if first != 0 {
		t.Fatalf("rail started at row %d, want its top", first)
	}

	a.vm.railList.ScrollToEnd()
	tt.Frame()
	first, last = a.vm.railList.Visible()
	if first == 0 {
		t.Fatal("rail did not move after scrolling to the end")
	}
	if want := len(a.railRows()) - 1; last != want {
		t.Fatalf("rail ended at row %d of %d", last, want)
	}
}
