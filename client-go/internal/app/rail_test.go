package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// manyChatsApp is a rail like the real one's: more chats than fit in a short
// window, a section of unknown kind at the very bottom.
func manyChatsApp(t *testing.T) *App {
	t.Helper()
	tuckedPrefs(t)
	a := newApp(Config{})
	now := time.Now()
	a.now, a.online = now, true
	var ss []protocol.Surface
	id := 100
	add := func(n int, kind, chatType, prefix string) {
		for i := 0; i < n; i++ {
			ss = append(ss, protocol.Surface{SurfaceID: "s:" + itoa(id), ConversationID: id,
				Kind: kind, ChatType: chatType, Title: prefix + " " + itoa(i), ProjectID: "workspace"})
			a.vm.tails[id] = now.Add(-time.Duration(i+1) * time.Minute)
			id++
		}
	}
	add(40, "client", "", "live")
	add(12, "other", "", "old")
	a.setSurfaces(ss)
	return a
}

// The rail's list has to be bounded by the window. It once sized itself to its
// content — 100% of the rail's height, with the brand line, the filter and the
// footer already spent on top of that — so it drew past the bottom edge and the
// wheel had nothing to scroll. The last sections were then unreachable: visible
// on a tall window, unclickable on a short one.
func TestRailListScrollsToItsSections(t *testing.T) {
	i18n.Set(i18n.ZH)

	// A short window: the list must be bounded by what is left after the brand
	// line, the filter and the footer, and the wheel must move it.
	a := manyChatsApp(t)
	rows := len(a.railRowsCached())
	if rows < 20 {
		t.Fatalf("rail has %d rows, the test needs more than a window of them", rows)
	}
	tt := ui.NewTester(a.View, 1180, 560)
	tt.Frame()
	// The footer is the rail's last line: it has to be inside the window, or
	// everything the list drew past it is off screen and unclickable.
	if r, ok := tt.Find("中文"); !ok || r.Y >= 556 {
		t.Fatalf("rail footer sits at y=%.0f, outside a 560-high window", r.Y)
	}
	_, last := a.vm.railList.Visible()
	if last >= rows-1 {
		t.Fatalf("the list thinks all %d rows are on screen; it is not bounded", rows)
	}
	for i := 0; i < 20; i++ {
		tt.Scroll(120, 300, 0, 200)
		tt.Frame()
	}
	if _, last = a.vm.railList.Visible(); last < rows-1 {
		t.Fatalf("the wheel never reached the end: last visible row %d of %d", last, rows)
	}

	// A window tall enough to hold the rail: opening the last section has to
	// put its chats on screen, not below the edge.
	b := manyChatsApp(t)
	tall := ui.NewTester(b.View, 1180, 1200)
	tall.Frame()
	if err := tall.Click("其他"); err != nil {
		t.Fatalf("click 其他: %v", err)
	}
	tall.Frame()
	if !tall.HasText("old 0") {
		t.Fatalf("nothing came into view after opening 其他; drew %v", tall.Texts())
	}
}
