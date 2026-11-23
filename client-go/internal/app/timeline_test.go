package app

import (
	"strings"
	"testing"
	"time"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// TestProjectTimelineReadsTheChat covers the derivation: what the drawer lists,
// in what order, and what it leaves out. A thought and a tool result are not
// events of their own — one has no words a list can use, the other belongs to
// the call above it.
func TestProjectTimelineReadsTheChat(t *testing.T) {
	i18n.Set(i18n.EN)
	start := time.Date(2026, 11, 24, 9, 0, 0, 0, time.Local)
	entries := []struct {
		id    int
		kind  string
		at    time.Time
		model string
	}{
		{11, "pi.user", start, `[{"role":"user","content":"why is the sky blue?","timestamp":__ts}]`},
		{12, "pi.assistant", start.Add(time.Second), `[{"role":"assistant","content":[{"type":"thinking","thinking":"Rayleigh"},{"type":"toolCall","name":"bash","arguments":{"command":"go build ./..."}}],"timestamp":__ts}]`},
		{13, "pi.tool-result", start.Add(4 * time.Second), `[{"role":"toolResult","content":[{"type":"text","text":"build ok"}],"timestamp":__ts}]`},
		{14, "pi.assistant", start.Add(6 * time.Second), `[{"role":"assistant","content":[{"type":"text","text":"**Blue** is scattered more."}],"timestamp":__ts}]`},
		{15, "pi.assistant", start.Add(8 * time.Second), `[{"role":"assistant","content":[],"stopReason":"error","errorMessage":"provider returned 502","timestamp":__ts}]`},
	}
	var list []protocol.Entry
	for _, e := range entries {
		list = append(list, entryJSON(t, e.id, e.kind, e.at, e.model))
	}
	// The page and the socket can both name the same entry.
	list = append(list, list[0])

	events := projectTimeline(list)
	if len(events) != 4 {
		t.Fatalf("got %d events, want 4: %+v", len(events), events)
	}
	if events[0].kind != tlUser || events[0].label != i18n.T("tlUser") {
		t.Fatalf("first event is %+v, want the user's message", events[0])
	}
	if events[1].kind != tlTool || !strings.HasPrefix(events[1].detail, "bash"+timelineSep) {
		t.Fatalf("second event is %+v, want the bash call", events[1])
	}
	if !strings.Contains(events[1].detail, "go build ./...") {
		t.Fatalf("tool event lost its command: %q", events[1].detail)
	}
	if events[2].kind != tlAssistant || !strings.Contains(events[2].detail, "Blue") {
		t.Fatalf("third event is %+v, want the answer", events[2])
	}
	if events[3].kind != tlError || !strings.Contains(events[3].detail, "502") {
		t.Fatalf("fourth event is %+v, want the failure", events[3])
	}
	for i := 1; i < len(events); i++ {
		if events[i].at.Before(events[i-1].at) {
			t.Fatalf("events out of order at %d: %v then %v", i, events[i-1].at, events[i].at)
		}
	}
}

// TestTimelineKeepsTheNewest covers the cap: a long chat keeps the events a
// reader would scroll to, not the first ones ever recorded.
func TestTimelineKeepsTheNewest(t *testing.T) {
	i18n.Set(i18n.EN)
	start := time.Date(2026, 11, 24, 9, 0, 0, 0, time.Local)
	var list []protocol.Entry
	for i := 0; i < timelineCap+20; i++ {
		list = append(list, entryJSON(t, i+1, "pi.user", start.Add(time.Duration(i)*time.Second),
			`[{"role":"user","content":"message `+itoa(i)+`","timestamp":__ts}]`))
	}
	events := projectTimeline(list)
	if len(events) != timelineCap {
		t.Fatalf("got %d events, want the cap of %d", len(events), timelineCap)
	}
	if !strings.Contains(events[len(events)-1].detail, "message "+itoa(timelineCap+19)) {
		t.Fatalf("newest event is %q, want the last message", events[len(events)-1].detail)
	}
}

// TestTimelineJumpLandsOnTheRow covers what a clicked event scrolls to: the row
// the entry drew, or the turn's fold when the entry is a tool call that never
// got a row of its own.
func TestTimelineJumpLandsOnTheRow(t *testing.T) {
	rows := []threadRow{
		{kind: rowDay},
		{kind: rowUser, entry: 4},
		{kind: rowSteps, entry: 5}, // the fold a call at 6 lands on
		{kind: rowAssistant, entry: 9},
	}
	if got := entryRowIndex(rows, 4); got != 1 {
		t.Fatalf("entry 4 → row %d, want 1", got)
	}
	if got := entryRowIndex(rows, 6); got != 2 {
		t.Fatalf("folded entry 6 → row %d, want 2", got)
	}
	if got := entryRowIndex(rows, 9); got != 3 {
		t.Fatalf("entry 9 → row %d, want 3", got)
	}
	if got := entryRowIndex(rows, 100); got != 3 {
		t.Fatalf("beyond the page → row %d, want the last one", got)
	}
	if got := entryRowIndex(rows, 1); got != -1 {
		t.Fatalf("before anything → row %d, want -1", got)
	}
}

// TestTaskRowsSortByNextRun is the table's ordering rule: a group is a queue of
// what happens next, and the task with no time at all is the one asking for
// something, so it goes first.
func TestTaskRowsSortByNextRun(t *testing.T) {
	i18n.Set(i18n.EN)
	a := newApp(Config{})
	a.rail = railTasks
	a.tasks = []protocol.Task{
		{ID: 1, Title: "late", State: "sleeping", Until: time.Date(2026, 11, 19, 18, 0, 0, 0, time.Local).UnixMilli()},
		{ID: 2, Title: "soon", State: "sleeping", Until: time.Date(2026, 11, 19, 15, 30, 0, 0, time.Local).UnixMilli()},
		{ID: 3, Title: "waiting on a human", State: "waiting"},
	}
	var order []string
	var counts []int
	for _, row := range a.railRows() {
		if row.kind == railGroup {
			counts = append(counts, row.count)
			continue
		}
		order = append(order, row.task.Title)
	}
	if len(order) != 3 || order[0] != "waiting on a human" || order[1] != "soon" || order[2] != "late" {
		t.Fatalf("order = %v", order)
	}
	if len(counts) != 2 || counts[0] != 1 || counts[1] != 2 {
		t.Fatalf("counts = %v, want the size of each group", counts)
	}
}
