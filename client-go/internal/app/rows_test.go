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
	// The tool result joins the call it answers instead of taking a line.
	if got := rows[1].steps[1].body; got != "echo blue → blue" {
		t.Fatalf("tool step body = %q", got)
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
	if len(rows) != 2 {
		t.Fatalf("rows = %+v, want the two live chats", rows)
	}
	if rows[0].conv.ConversationID != 2 || rows[1].conv.ConversationID != 3 {
		t.Fatalf("order = %d, %d; want newest first", rows[0].conv.ConversationID, rows[1].conv.ConversationID)
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
		{ID: 11, State: "running", Kind: "app.usage-sampler"},
	}
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
	// A task without a title falls back to its kind rather than drawing a gap.
	if taskTitle(tasks[0].task) != "app.usage-sampler" {
		t.Fatalf("untitled task title = %q", taskTitle(tasks[0].task))
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
