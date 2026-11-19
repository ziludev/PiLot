package app

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// entryJSON builds the model the bridge sends: one pi message, timestamped.
func entryJSON(t *testing.T, id int, kind string, at time.Time, model string) protocol.Entry {
	t.Helper()
	if model == "" {
		return protocol.Entry{ID: id, Kind: kind, Model: nil}
	}
	// The timestamp belongs to the message, so splice the time into the marker.
	stamped := strings.ReplaceAll(model, "__ts", strconv.FormatInt(at.UnixMilli(), 10))
	return protocol.Entry{ID: id, Kind: kind, Model: []byte(stamped)}
}

func TestProjectEntriesFoldsSteps(t *testing.T) {
	i18n.Set(i18n.EN)
	start := time.Date(2026, 11, 19, 15, 0, 0, 0, time.Local)
	entries := []protocol.Entry{
		entryJSON(t, 1, "pi.user", start, `[{"role":"user","content":"why is the sky blue","timestamp":__ts}]`),
		entryJSON(t, 2, "pi.assistant", start.Add(2*time.Second), `[{"role":"assistant","content":[{"type":"thinking","thinking":"Rayleigh scattering"},{"type":"toolCall","name":"bash","arguments":{"command":"echo blue"}}],"timestamp":__ts}]`),
		entryJSON(t, 3, "pi.tool-result", start.Add(5*time.Second), `[{"role":"toolResult","content":[{"type":"text","text":"blue"}],"timestamp":__ts}]`),
		entryJSON(t, 4, "pi.assistant", start.Add(12*time.Second), `[{"role":"assistant","content":[{"type":"text","text":"Shorter wavelengths scatter more."}],"timestamp":__ts}]`),
	}
	rows, turns := projectEntries(7, entries)
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want user + steps + assistant: %+v", len(rows), rows)
	}
	if rows[0].kind != rowUser || rows[0].text != "why is the sky blue" {
		t.Fatalf("row 0 = %+v", rows[0])
	}
	if rows[1].kind != rowSteps || len(rows[1].steps) != 2 {
		t.Fatalf("row 1 = %+v", rows[1])
	}
	// The tool result joins the call it answers instead of taking a line, and
	// is kept whole for the output block rather than clipped into the summary.
	if got := rows[1].steps[1].body; got != "echo blue" {
		t.Fatalf("tool step body = %q", got)
	}
	if got := rows[1].steps[1].out; got != "blue" {
		t.Fatalf("tool step out = %q", got)
	}
	// The span runs from the first step to the tool result that closed it.
	if rows[1].noSpan || rows[1].span != 3*time.Second {
		t.Fatalf("span = %v (noSpan %v), want 3s", rows[1].span, rows[1].noSpan)
	}
	if rows[1].id != foldKey(7, 1) {
		t.Fatalf("fold key = %q, want the first entry of the turn", rows[1].id)
	}
	if len(turns) != 1 || turns[0] != 1 {
		t.Fatalf("turns = %v", turns)
	}
	if rows[2].kind != rowAssistant || rows[2].text != "Shorter wavelengths scatter more." {
		t.Fatalf("row 2 = %+v", rows[2])
	}
}

func TestProjectEntriesErrorRow(t *testing.T) {
	i18n.Set(i18n.EN)
	entries := []protocol.Entry{
		entryJSON(t, 1, "pi.user", time.Now(), `[{"role":"user","content":"hi","timestamp":__ts}]`),
		entryJSON(t, 2, "pi.assistant", time.Now(), `[{"role":"assistant","content":[],"stopReason":"error","errorMessage":"provider 500","timestamp":__ts}]`),
	}
	rows, _ := projectEntries(1, entries)
	if len(rows) != 2 || rows[1].kind != rowError || rows[1].text != "provider 500" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestProjectEntriesUserImages(t *testing.T) {
	i18n.Set(i18n.EN)
	entries := []protocol.Entry{
		entryJSON(t, 1, "pi.user", time.Now(),
			`[{"role":"user","content":[{"type":"text","text":"look"},{"type":"image","data":"aGk=","mimeType":"image/png"}],"timestamp":__ts}]`),
	}
	rows, _ := projectEntries(1, entries)
	if len(rows) != 1 || rows[0].text != "look" || len(rows[0].images) != 1 {
		t.Fatalf("rows = %+v", rows)
	}
	if string(rows[0].images[0].data) != "hi" {
		t.Fatalf("image data = %q", rows[0].images[0].data)
	}
}

func TestRailRowsChats(t *testing.T) {
	i18n.Set(i18n.EN)
	a := newApp(Config{})
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.Local)
	a.now = now
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 3, Kind: "feishu", Title: "deploy"},
		{ConversationID: 2, Kind: "client", Title: "sky"},
		{ConversationID: 4, Kind: "client", Title: "old", Archived: true},
	})
	a.vm.tails[2] = now.Add(-3 * time.Minute)
	a.vm.tails[3] = now.Add(-2 * time.Hour)
	a.vm.unread[3] = true

	rows := a.railRows()
	// The live chats, then the archived shelf: the archived chat is neither
	// dropped nor mixed into a section, so the rail is three rows, not two.
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want the two live chats and the archived shelf", rows)
	}
	if rows[0].conv.ConversationID != 2 || rows[1].conv.ConversationID != 3 {
		t.Fatalf("order = %d, %d; want newest first", rows[0].conv.ConversationID, rows[1].conv.ConversationID)
	}
	if rows[2].kind != railGroup || rows[2].section != sectionArchived || rows[2].count != 1 || rows[2].open {
		t.Fatalf("archived shelf = %+v, want one folded chat", rows[2])
	}
	if rows[0].meta != "Local" || rows[1].meta != "Feishu" {
		t.Fatalf("source labels = %q, %q", rows[0].meta, rows[1].meta)
	}
	if rows[1].unread != true || rows[0].unread != false {
		t.Fatalf("unread = %v, %v", rows[0].unread, rows[1].unread)
	}

	a.filter = "sky"
	if rows := a.railRows(); len(rows) != 1 || rows[0].conv.ConversationID != 2 {
		t.Fatalf("filtered rows = %+v", rows)
	}
	a.filter = "nothing"
	if rows := a.railRows(); len(rows) != 1 || rows[0].kind != railHint {
		t.Fatalf("empty filter rows = %+v", rows)
	}
}

func TestRailRowsTasks(t *testing.T) {
	i18n.Set(i18n.EN)
	a := newApp(Config{})
	a.rail = railTasks
	until := time.Date(2026, 11, 19, 15, 4, 0, 0, time.Local)
	a.tasks = []protocol.Task{
		{ID: 12, Title: "follow up on the review", State: "sleeping", Until: until.UnixMilli()},
		{ID: 9, Title: "waiting on a human", State: "waiting"},
		{ID: 11, State: "running", Kind: "app.usage-sampler", ConversationID: 7},
	}
	a.setSurfaces([]protocol.Surface{{ConversationID: 7, Kind: "direct", Title: "Sky's build"}})
	rows := a.railRows()
	// working, waiting, sleeping: three groups, each with its own header.
	var groups []string
	var tasks []railRow
	for _, row := range rows {
		if row.kind == railGroup {
			groups = append(groups, row.label)
		} else {
			tasks = append(tasks, row)
		}
	}
	if len(groups) != 3 || groups[0] != "Working" || groups[2] != "Sleeping" {
		t.Fatalf("groups = %v", groups)
	}
	if len(tasks) != 3 {
		t.Fatalf("tasks = %+v", tasks)
	}
	if tasks[2].when != "15:04" {
		t.Fatalf("next = %q, want 15:04", tasks[2].when)
	}
	// A task without a title of its own is named by the chat it belongs to, and
	// only a task with no chat at all falls back to its kind in words.
	if got := a.taskTitle(tasks[0].task); got != "Sky's build" {
		t.Fatalf("task title = %q, want the chat's", got)
	}
	if got := taskKindLabel("app.usage-sampler"); got != "Usage sample" {
		t.Fatalf("kind label = %q", got)
	}
}

func TestRelTime(t *testing.T) {
	i18n.Set(i18n.EN)
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.Local)
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Time{}, ""},
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-12 * time.Minute), "12m ago"},
		{now.Add(-5 * time.Hour), "5h ago"},
		{now.Add(-3 * 24 * time.Hour), "3d ago"},
	}
	for _, c := range cases {
		if got := relTime(now, c.at); got != c.want {
			t.Errorf("relTime(%v) = %q, want %q", c.at, got, c.want)
		}
	}
	if got := shortDuration(12 * time.Second); got != "12s" {
		t.Errorf("shortDuration = %q", got)
	}
	if got := shortDuration(95 * time.Second); got != "1m35s" {
		t.Errorf("shortDuration = %q", got)
	}
}

// TestLongTitleKeepsTheTimeOnTheRail: a long chat title must not push the
// conversation's time out of the 260pt rail.
func TestLongTitleKeepsTheTimeOnTheRail(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	a.setSurfaces([]protocol.Surface{{
		ConversationID: 11,
		Kind:           "feishu",
		Title:          "[alex 本人] 感觉你理解错我的意思了，我不是要重写那个包的测试，我是想让你先看一眼",
	}})
	a.vm.tails[11] = a.now.Add(-3 * time.Minute)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("3m ago") {
		t.Fatalf("the time fell off the rail; drew %v", tt.Texts())
	}
}

// The rail files chats by what they are, and only the ones being worked on are
// drawn: everything else waits behind a heading with its count on it.
func TestRailRowsSections(t *testing.T) {
	i18n.Set(i18n.EN)
	a := newApp(Config{})
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.Local)
	a.now = now
	day := 24 * time.Hour
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 1, SurfaceID: "oc_1", Kind: "feishu", Title: "alex", ChatType: "p2p"},
		{ConversationID: 2, SurfaceID: "oc_2", Kind: "feishu", Title: "backend", ChatType: "group"},
		{ConversationID: 3, SurfaceID: "oc_3", Kind: "feishu", Title: "release", ChatType: "group", Topic: true},
		{ConversationID: 4, SurfaceID: "oc_4", Kind: "feishu", Title: "now", ChatType: "p2p"},
		{ConversationID: 5, Kind: "client", Title: "local"},
		{ConversationID: 6, SurfaceID: "oc_6", Kind: "feishu", Title: "mystery"},
	})
	a.vm.tails[1] = now.Add(-3 * day)
	a.vm.tails[2] = now.Add(-5 * day)
	a.vm.tails[3] = now.Add(-6 * day)
	a.vm.tails[4] = now.Add(-time.Hour)
	a.vm.tails[6] = now.Add(-5 * day) // a week would file it away

	rows := a.railRows()
	want := []struct {
		kind    railRowKind
		section string
		conv    protocol.Surface
	}{
		{railGroup, sectionActive, protocol.Surface{}},
		{railChat, "", protocol.Surface{ConversationID: 4}},
		{railGroup, sectionGroup, protocol.Surface{}},
		{railGroup, sectionTopic, protocol.Surface{}},
		{railGroup, sectionDirect, protocol.Surface{}},
		{railGroup, sectionOther, protocol.Surface{}},
	}
	if len(rows) != len(want) {
		t.Fatalf("rows = %+v, want %d", rows, len(want))
	}
	for i, w := range want {
		if rows[i].kind != w.kind || rows[i].section != w.section || rows[i].conv.ConversationID != w.conv.ConversationID {
			t.Fatalf("row %d = %+v, want %+v", i, rows[i], w)
		}
	}
	if rows[0].count != 1 || !rows[0].open {
		t.Fatalf("active heading = %+v, want one chat drawn", rows[0])
	}
	if rows[2].count != 1 || rows[2].open {
		t.Fatalf("group heading = %+v, want one chat folded", rows[2])
	}

	// Opening a section keeps its chats, and the choice outlives the run.
	a.prefs.Sections = map[string]bool{sectionDirect: true}
	rows = a.railRows()
	drawn := make([]int, 0, len(rows))
	for _, r := range rows {
		if r.kind == railChat {
			drawn = append(drawn, r.conv.ConversationID)
		}
	}
	if len(drawn) != 3 {
		t.Fatalf("drawn = %v, want the active chat and both direct ones", drawn)
	}

	// Opening a chat unfolds the section holding it, never the other way: the
	// fold belongs to the user, so a closed section is not reopened by a frame.
	a.prefs.Sections = map[string]bool{}
	a.active = 2
	if rows = a.railRows(); rows[2].open {
		t.Fatalf("group heading = %+v, want it still folded until the chat is opened", rows[2])
	}
	a.revealChatSection(2)
	rows = a.railRows()
	if rows[2].section != sectionGroup || !rows[2].open {
		t.Fatalf("group heading = %+v, want it open because chat 2 was just opened", rows[2])
	}
	if rows[3].conv.ConversationID != 2 {
		t.Fatalf("row after the group heading = %+v, want chat 2", rows[3])
	}

	// And a fold made by hand afterwards sticks, chat on screen or not.
	a.prefs.Sections[sectionGroup] = false
	if rows = a.railRows(); rows[2].open {
		t.Fatalf("group heading = %+v, want the hand fold to hold", rows[2])
	}
}

func TestRailArchivedShelf(t *testing.T) {
	i18n.Set(i18n.EN)
	a := newApp(Config{})
	a.now = time.Date(2026, 11, 19, 15, 0, 0, 0, time.Local)
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 2, Kind: "client", Title: "sky"},
		{ConversationID: 4, Kind: "client", Title: "old", Archived: true},
		{ConversationID: 5, Kind: "client", Title: "older", Archived: true},
	})
	a.vm.tails[2] = a.now.Add(-time.Minute)

	// Folded by default: one live chat and the shelf, which says how many it holds.
	rows := a.railRows()
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want the live chat and the folded shelf", rows)
	}
	if shelf := rows[1]; shelf.kind != railGroup || shelf.section != sectionArchived || shelf.count != 2 || shelf.open {
		t.Fatalf("shelf = %+v, want two chats folded away", shelf)
	}

	// Unfolding is a preference, and the unfolded shelf lists its chats.
	a.prefs.Sections = map[string]bool{sectionArchived: true}
	rows = a.railRows()
	if len(rows) != 4 {
		t.Fatalf("rows = %+v, want the shelf open", rows)
	}
	// Newest first, like the sections above it.
	if !rows[1].open || rows[2].kind != railChat || rows[2].conv.ConversationID != 5 {
		t.Fatalf("open shelf = %+v", rows[1:])
	}

	// A filter opens the shelf: you cannot filter for what stays hidden.
	a.prefs.Sections = map[string]bool{}
	a.filter = "old"
	rows = a.railRows()
	if len(rows) != 3 || rows[0].kind != railGroup || !rows[0].open {
		t.Fatalf("filtered rows = %+v, want the matching chats under an open shelf", rows)
	}

	// Opening a chat that lives on the shelf unfolds the shelf.
	a.filter = ""
	a.active = 4
	a.revealChatSection(4)
	rows = a.railRows()
	found := false
	for _, row := range rows[2:] {
		if row.kind == railChat && row.conv.ConversationID == 4 {
			found = true
		}
	}
	if !rows[1].open || !found {
		t.Fatalf("shelf = %+v, want it open over the chat just opened", rows[1:])
	}
}

func TestRailSearchResults(t *testing.T) {
	i18n.Set(i18n.EN)
	a := newApp(Config{})
	a.now = time.Date(2026, 11, 19, 15, 0, 0, 0, time.Local)
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 2, Kind: "client", Title: "sky"},
		{ConversationID: 4, Kind: "client", Title: "old", Archived: true},
	})
	a.vm.tails[2] = a.now.Add(-time.Minute)
	a.search.query = "deploy"
	a.search.hits = []protocol.SearchHit{
		{ConversationID: 2, EntryID: 9, Snippet: "line one\nline two"},
		{ConversationID: 7, EntryID: 3, Snippet: "no surface yet"},
	}

	// One character is the local filter; the bridge is not involved.
	a.filter = "z"
	rows := a.railRows()
	if rows[0].kind != railHint {
		t.Fatalf("rows = %+v, want no hit list under one character", rows)
	}

	a.filter = "deploy"
	rows = a.railRows()
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want the header and the two hits", rows)
	}
	if rows[0].kind != railGroup || rows[0].label != i18n.T("searchResults") {
		t.Fatalf("header = %+v", rows[0])
	}
	if rows[1].kind != railSearchHit || rows[1].convID != 2 || rows[1].entryID != 9 || rows[1].label != "sky" {
		t.Fatalf("hit = %+v, want the chat's title", rows[1])
	}
	if rows[1].snippet != "line one line two" {
		t.Fatalf("snippet = %q, want it on one line", rows[1].snippet)
	}
	// A hit in a chat the snapshot has not caught up with keeps its number.
	if rows[2].label != "#7" {
		t.Fatalf("hit label = %q, want the chat's number", rows[2].label)
	}

	// A reply for a query the user has typed past is not drawn.
	a.search.query = "dep"
	if rows := a.railRows(); rows[0].kind == railGroup {
		t.Fatalf("rows = %+v, want the stale hits gone", rows)
	}

	// The filter moving on is a cache miss, so the block follows the box.
	a.search.query = "deploy"
	a.filter = "deploy"
	a.vm.railCache, a.vm.railStamp, a.vm.railFilter = nil, 0, ""
	if rows := a.railRowsCached(); rows[0].kind != railGroup {
		t.Fatalf("cached rows = %+v, want the hits", rows)
	}
	a.filter = "deploy more"
	if rows := a.railRowsCached(); rows[0].kind == railGroup {
		t.Fatalf("cached rows = %+v, want them rebuilt for the new filter", rows)
	}
}

// TestSectionFoldRedrawsTheRail: folding a section changes no data from the
// bridge, so the cached rows have to be retired by hand. Without that the
// chevron turns and the list stays exactly as it was, which reads as a dead
// click on every section header.
func TestSectionFoldRedrawsTheRail(t *testing.T) {
	i18n.Set(i18n.EN)
	tuckedPrefs(t)
	a := newApp(Config{})
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 1, Kind: "feishu", ChatType: "p2p", Title: "one"},
		{ConversationID: 2, Kind: "feishu", ChatType: "p2p", Title: "two"},
	})
	// A new window opens 进行中 and folds everything else, so this one starts
	// folded: header and nothing under it.
	folded := a.railRowsCached()
	if len(folded) != 1 {
		t.Fatalf("folded section drew %d rows", len(folded))
	}
	a.toggleSection("direct")
	opened := a.railRowsCached()
	if len(opened) != len(folded)+2 {
		t.Fatalf("opening the section changed %d rows to %d", len(folded), len(opened))
	}
	if header := opened[0]; header.section != "direct" || !header.open {
		t.Fatalf("header = %+v", header)
	}

	a.toggleSection("direct")
	if again := a.railRowsCached(); len(again) != len(folded) {
		t.Fatalf("folding again left %d rows, want %d", len(again), len(folded))
	}
}

// A chat nobody has said anything in for a week leaves its section by itself.
// The rail is a place to work, and a hundred chats of history is not work — but
// the chat on screen and one with an unread reply are still asking for
// something, so those stay where they are.
func TestAutoArchiveOldChats(t *testing.T) {
	i18n.Set(i18n.EN)
	tuckedPrefs(t)
	a := newApp(Config{})
	now := time.Date(2026, 11, 19, 15, 0, 0, 0, time.Local)
	a.now = now
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 1, Kind: "feishu", Title: "stale", ChatType: "p2p"},
		{ConversationID: 2, Kind: "feishu", Title: "fresh", ChatType: "p2p"},
		{ConversationID: 3, Kind: "feishu", Title: "unread", ChatType: "p2p"},
	})
	a.vm.tails[1] = now.Add(-8 * 24 * time.Hour)
	a.vm.tails[2] = now.Add(-2 * time.Hour)
	a.vm.tails[3] = now.Add(-8 * 24 * time.Hour)
	a.vm.unread[3] = true

	// The direct section is folded, so count its heading rather than its rows:
	// what it holds is the two chats nobody filed away.
	counts := map[string]int{}
	for _, row := range a.railRows() {
		if row.kind == railGroup {
			counts[row.section] = row.count
		}
	}
	if counts[sectionArchived] != 1 {
		t.Fatalf("已归档 holds %d chats, want the one stale chat", counts[sectionArchived])
	}
	if counts[sectionActive] != 1 {
		t.Fatalf("进行中 holds %d chats, want the fresh one", counts[sectionActive])
	}
	if counts[sectionDirect] != 1 {
		t.Fatalf("直接 holds %d chats, want the unread one", counts[sectionDirect])
	}

	// Taking it back out is this window's opinion, not the bridge's: the chat
	// returns and the file remembers, so it does not go again on the next frame
	// or the next start.
	a.setArchived(1, false)
	counts = map[string]int{}
	for _, row := range a.railRows() {
		if row.kind == railGroup {
			counts[row.section] = row.count
		}
	}
	if counts[sectionArchived] != 0 || counts[sectionDirect] != 2 {
		t.Fatalf("after the take-back: 已归档 %d, 直接 %d", counts[sectionArchived], counts[sectionDirect])
	}
	if !a.prefs.keptChat(1) {
		t.Fatalf("the take-back was not remembered")
	}
}
