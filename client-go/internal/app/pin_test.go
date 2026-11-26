package app

import (
	"testing"
	"time"

	"pilot/internal/i18n"
	"pilot/internal/protocol"

	"github.com/egoist/mygo/ui"
)

// shelves is the rail's headings in order, and who is filed under each.
func shelves(a *App) []string {
	out := []string{}
	for _, row := range a.chatRows("") {
		if row.kind == railGroup {
			out = append(out, row.label)
		}
	}
	return out
}

// chatShows is whether a chat's row is under the named heading.
func chatShows(a *App, convID int, shelf string) bool {
	under := ""
	for _, row := range a.chatRows("") {
		if row.kind == railGroup {
			under = row.label
			continue
		}
		if row.kind == railChat && row.conv.ConversationID == convID {
			return under == shelf
		}
	}
	return false
}

// TestPinnedChatSitsAtTheTop: a pin moves the chat out of wherever it was filed
// and puts it in the first shelf, which is open — the point of a pin is not
// having to look.
func TestPinnedChatSitsAtTheTop(t *testing.T) {
	i18n.Set(i18n.EN)
	a := railApp(t, "http://127.0.0.1:1")
	a.prefs.Pinned = []int{11}

	if got := shelves(a); len(got) == 0 || got[0] != "Pinned" {
		t.Fatalf("shelves = %v, want Pinned first", got)
	}
	if !chatShows(a, 11, "Pinned") {
		t.Fatalf("11 is not in the pinned shelf: %v", a.chatRows(""))
	}
	if !chatShows(a, 12, "Active") {
		t.Fatalf("12 left the active shelf")
	}
	if a.prefs.sectionClosed(sectionPinned) {
		t.Fatal("the pinned shelf starts folded")
	}
}

// TestPinnedChatIsNeverFiledAway: a pin outranks the clock. A chat pinned on
// Friday is still on top on Monday, which is the whole reason to pin it.
func TestPinnedChatIsNeverFiledAway(t *testing.T) {
	i18n.Set(i18n.EN)
	a := railApp(t, "http://127.0.0.1:1")
	a.vm.tails[11] = a.now.Add(-30 * 24 * time.Hour)
	a.prefs.Pinned = []int{11}

	if chatShows(a, 11, "Archived") {
		t.Fatalf("pinned chat was filed away: %v", a.chatRows(""))
	}
	if !chatShows(a, 11, "Pinned") {
		t.Fatalf("pinned chat is not on top")
	}
}

// TestPinFromTheMenu: the verb is reachable, and it changes the rail without
// telling the bridge — a pin is the window's own bookkeeping.
func TestPinFromTheMenu(t *testing.T) {
	i18n.Set(i18n.EN)
	a := openRailSections(railApp(t, "http://127.0.0.1:1"))

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("⋯"); err != nil {
		t.Fatalf("no row menu: %v", err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("Pin to top"); err != nil {
		t.Fatalf("choose pin: %v", err)
	}
	tt.Frame()
	tt.Frame()

	if !a.prefs.pinnedChat(12) {
		t.Fatalf("12 not pinned: %v", a.prefs.Pinned)
	}
	if !chatShows(a, 12, "Pinned") {
		t.Fatalf("row did not move to the pinned shelf")
	}
}

// TestRailGroupedByProject: with the grouping on, the headings are the projects
// the chats belong to, and a chat with no project falls to 其他.
func TestRailGroupedByProject(t *testing.T) {
	i18n.Set(i18n.EN)
	a := railApp(t, "http://127.0.0.1:1")
	a.projects.items = []protocol.Project{{ID: "p1", Name: "dora-service"}}
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 12, Kind: "client", Title: "fix android", ProjectID: "p1"},
		{ConversationID: 11, Kind: "client", Title: "sky"},
	})
	a.prefs.Group = groupProject

	// An empty shelf is not drawn, so 置顶 waits until something is pinned.
	got := shelves(a)
	if len(got) != 2 || got[0] != "dora-service" || got[1] != "Other" {
		t.Fatalf("shelves = %v, want dora-service, Other", got)
	}
	if !chatShows(a, 12, "dora-service") {
		t.Fatal("12 is not under its project")
	}
	// 其他 is a grab bag, so it stays folded: the heading says the chats are
	// there without making the reader scroll past them.
	if !a.prefs.sectionClosed(sectionOther) {
		t.Fatal("Other starts open")
	}
	if !chatShows(a, 12, "dora-service") || chatShows(a, 12, "Other") {
		t.Fatal("12 is filed in the wrong shelf")
	}
	if a.prefs.sectionClosed(projectSectionKey("p1")) {
		t.Fatal("a project shelf starts folded")
	}

	// Back to the default grouping: both chats are active, one shelf needs no
	// heading, and the project name is gone from the rail.
	a.setRailGroup("")
	if got := shelves(a); len(got) != 0 {
		t.Fatalf("shelves after regrouping = %v, want no heading", got)
	}
	chats := 0
	for _, row := range a.chatRows("") {
		if row.kind == railChat {
			chats++
		}
	}
	if chats != 2 {
		t.Fatalf("regrouped rail holds %d chats, want 2", chats)
	}
}
