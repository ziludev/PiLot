package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
	"pilot/internal/protocol"
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
	// The list must be a window onto its rows, not the whole of them: this is the
	// check that fails when it sizes itself to content again.
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

// Folding a section has to fold it, even when the chat on screen lives inside
// it. The rows used to be forced open for the active chat, so clicking 「其他」
// with one of its chats on screen turned the chevron and changed nothing: the
// section the user most wants to tidy up was the one that could not be folded.
func TestRailFoldSticksForActiveChat(t *testing.T) {
	i18n.Set(i18n.ZH)
	tuckedPrefs(t)
	a := newApp(Config{})
	now := time.Now()
	a.now, a.online = now, true
	var ss []protocol.Surface
	for i := 0; i < 3; i++ {
		id := 200 + i
		ss = append(ss, protocol.Surface{SurfaceID: "s:" + itoa(id), ConversationID: id,
			Kind: "client", Title: "live " + itoa(i), ProjectID: "workspace"})
		a.vm.tails[id] = now.Add(-time.Duration(i+1) * time.Minute)
	}
	// A day old and not from this window: the rail files these under 其他.
	for i := 0; i < 5; i++ {
		id := 300 + i
		ss = append(ss, protocol.Surface{SurfaceID: "s:" + itoa(id), ConversationID: id,
			Kind: "feishu", Title: "saved " + itoa(i), ProjectID: "workspace"})
		a.vm.tails[id] = now.Add(-48 * time.Hour)
	}
	a.setSurfaces(ss)
	a.prefs.Sections = map[string]bool{sectionOther: true}
	a.active = 302
	a.stamp++
	if got := countChats(a.railRows()); got != 8 {
		t.Fatalf("open 其他 = %d chats, want all 8", got)
	}

	a.toggleSection(sectionOther)
	if got := countChats(a.railRows()); got != 3 {
		t.Fatalf("folded 其他 = %d chats, want only the 3 live ones", got)
	}
	// And a frame does not undo it: the reveal belongs to opening a chat.
	a.stamp++
	if got := countChats(a.railRows()); got != 3 {
		t.Fatalf("after a frame = %d chats, want the fold to hold", got)
	}

	// Opening a chat inside the folded section unfolds it again.
	a.selectConversation(303)
	if got := countChats(a.railRows()); got != 8 {
		t.Fatalf("after opening 303 = %d chats, want 其他 unfolded", got)
	}
}

// countChats is how many chat rows the rail drew.
func countChats(rows []railRow) int {
	n := 0
	for _, r := range rows {
		if r.kind == railChat {
			n++
		}
	}
	return n
}

// TestRailSpinsOnARunningChat: a chat with a turn in flight carries a spinner
// where the unread dot will sit when it finishes, and an idle one carries
// nothing, so the two are told apart without opening either.
func TestRailSpinsOnARunningChat(t *testing.T) {
	i18n.Set(i18n.EN)
	a := openRailSections(railApp(t, "http://127.0.0.1:1"))
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 12, Kind: "feishu", Title: "sky"},
		{ConversationID: 11, Kind: "client", Title: "why is the sky blue"},
	})
	a.runningConvs = map[int]bool{11: true}
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("why is the sky blue") {
		t.Fatalf("the running chat's row is missing; drew %v", tt.Texts())
	}
	if _, ok := tt.Find("Running"); !ok {
		t.Fatalf("a running chat drew no spinner; drew %v", tt.Texts())
	}
	a.runningConvs = nil
	tt2 := ui.NewTester(a.View, 1180, 820)
	tt2.Frame()
	if _, ok := tt2.Find("Running"); ok {
		t.Fatal("an idle chat drew the running spinner")
	}
}
