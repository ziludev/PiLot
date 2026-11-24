package app

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// timelineWidth is how wide the drawer is. Narrower than the changes drawer: a
// line of "ran bash · go build ./..." needs no diff's worth of room.
const timelineWidth = 320

// timelineCap is how many of the newest events are kept. A chat that ran all
// afternoon has thousands of blocks, and nobody scrolls to the top of a day to
// find what happened at breakfast; the cache also stays small this way.
const timelineCap = 400

// timelineState is the drawer's flag and the events derived for the chat it was
// opened on, remembered so the projection runs once per change, not per frame.
type timelineState struct {
	open bool

	conv    int
	version uint64
	events  []timelineEvent
	// fresh is false when the cache does not describe this chat any more.
	fresh bool
}

// timelineKind is what an event was, which is what colour its dot is.
type timelineKind int

const (
	tlUser timelineKind = iota
	tlAssistant
	tlThinking
	tlTool
	tlError
)

// timelineEvent is one thing that happened at one time: who, what, and the line
// that says which of twenty bash calls it was.
type timelineEvent struct {
	at     time.Time
	kind   timelineKind
	label  string
	detail string
	// entry is the message the event came from, so a click can go there.
	entry int
}

// toggleTimeline opens the drawer on the active chat, or closes it.
func (a *App) toggleTimeline() {
	a.tl.open = !a.tl.open
	if a.tl.open {
		// One right edge, one drawer.
		a.mem.open = false
		a.changes.open = false
		a.projects.open = false
	}
	a.invalidate()
}

// timelineEvents is the active chat's history as a list of what happened when.
// It is derived from the entries the transcript already has: the bridge has no
// endpoint for this, and it does not need one — every event is a message the
// reader has already been shown, in the order it arrived.
func (a *App) timelineEvents() []timelineEvent {
	th := a.threads[a.active]
	if th == nil || !th.loaded {
		return nil
	}
	if a.tl.fresh && a.tl.conv == a.active && a.tl.version == th.version {
		return a.tl.events
	}
	events := projectTimeline(th.entries)
	a.tl = timelineState{open: a.tl.open, conv: a.active, version: th.version, events: events, fresh: true}
	return events
}

// projectTimeline is the pure half: entries in, events out, oldest first.
func projectTimeline(entries []protocol.Entry) []timelineEvent {
	ordered := make([]protocol.Entry, len(entries))
	copy(ordered, entries)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	events := make([]timelineEvent, 0, len(ordered))
	seen := map[int]bool{}
	for _, entry := range ordered {
		// A page and the socket's window can name the same entry twice.
		if seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		view := parseEntry(entry)
		if view.userText != "" {
			events = append(events, timelineEvent{
				at: view.at, kind: tlUser, entry: entry.ID,
				label:  i18n.T("tlUser"),
				detail: clip(oneLine(view.userText), 120),
			})
		}
		for _, block := range view.blocks {
			switch block.kind {
			case "text":
				events = append(events, timelineEvent{
					at: block.at, kind: tlAssistant, entry: entry.ID,
					label:  i18n.T("tlAssistant"),
					detail: clip(oneLine(block.text), 120),
				})
			case "thinking":
				// A thought is the one event with nothing to say in a list: the
				// words are the transcript's, and "thought for a while" is not
				// worth a row. It stays in the count, not in the timeline.
				continue
			case "tool-result":
				continue
			default:
				detail := argsSummary(json.RawMessage(block.args))
				if detail == "" {
					detail = clip(oneLine(block.text), 120)
				}
				events = append(events, timelineEvent{
					at: block.at, kind: tlTool, entry: entry.ID,
					label:  i18n.T("tlTool"),
					detail: block.name + timelineSep + detail,
				})
			}
		}
		if view.errText != "" {
			events = append(events, timelineEvent{
				at: view.at, kind: tlError, entry: entry.ID,
				label:  i18n.T("tlError"),
				detail: clip(oneLine(view.errText), 120),
			})
		}
	}
	if len(events) > timelineCap {
		events = events[len(events)-timelineCap:]
	}
	return events
}

// timelineSep is what stands between a tool's name and its argument: a middle
// dot, not a dash, so "bash · go build" reads as one label rather than a range.
const timelineSep = " · "

// timelineView draws the drawer: one heading per day, one line per event, the
// clock at the left edge so the column of times can be read as a column.
func (a *App) timelineView(c *ui.Context, t theme.Tokens) {
	ui.Column(c).Width(timelineWidth).Shrink(1).MinWidth(280).FillHeight().
		Radius(theme.RadiusWindow).Border(1, t.Line).Background(t.Surface).Clip().
		Children(func() {
			a.timelineHeader(c, t)
			ui.Box(c).Width(timelineWidth).Height(1).Background(t.Line)
			events := a.timelineEvents()
			if len(events) == 0 {
				ui.Box(c).Padding(14, 14).Children(func() {
					ui.Text(c, i18n.T("timelineEmpty")).FontSize(theme.TextSmall).TextColor(t.Ink3).MaxLines(3)
				})
				return
			}
			a.timelineList(c, t, events)
		})
}

func (a *App) timelineHeader(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(48).Padding(0, 12).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Icon(c, icons.Clock).Size(13, 13).TextColor(t.Ink3)
		ui.Text(c, i18n.T("timeline")).FontSize(theme.TextTitle).FontWeight(500).TextColor(t.Ink).MaxLines(1)
		ui.Spacer(c)
		closeBtn := ui.ButtonBase(c).Size(20, 20).Radius(theme.RadiusChip).Justify(ui.Center).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("close"))
		closeBtn.Children(func() {
			ui.Icon(c, icons.X).Size(12, 12).TextColor(t.Ink3)
		})
		closeBtn.OnClick(func() { a.tl.open = false })
	})
}

// timelineList draws the events newest last, so the bottom of the drawer is the
// newest thing that happened — the same direction the transcript reads.
func (a *App) timelineList(c *ui.Context, t theme.Tokens, events []timelineEvent) {
	list := ui.Column(c).FillWidth().Grow(1).Padding(4, 8, 12, 8).Gap(2)
	list.Children(func() {
		lastDay := ""
		for _, event := range events {
			if day := dayKey(event.at); day != "" && day != lastDay {
				lastDay = day
				ui.Row(c).FillWidth().Height(26).Padding(4, 4, 0, 4).AlignItems(ui.End).Children(func() {
					ui.Text(c, dayLabel(event.at)).FontSize(theme.TextMicro).FontWeight(600).
						TextColor(t.Ink3).MaxLines(1)
				})
			}
			a.timelineRow(c, t, event)
		}
	})
}

// timelineRow is one event: the clock, the dot that says what it was, and two
// lines — what happened and the words that identify it. A row is a button: the
// transcript and the timeline are two views of the same messages, and clicking
// one is how a reader gets from "when" to "which".
func (a *App) timelineRow(c *ui.Context, t theme.Tokens, event timelineEvent) {
	row := ui.ButtonBase(c).FillWidth().Padding(3, 4).Gap(8).AlignItems(ui.Start).
		Radius(theme.RadiusChip).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row = row.Background(t.Field)
	}
	row.Children(func() {
		ui.Text(c, clockTime(event.at)).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
		ui.Box(c).Width(6).Height(6).Radius(3).Background(timelineTone(t, event.kind)).
			Margin(5, 0, 0, 0)
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, event.label).FontSize(theme.TextRow).FontWeight(500).
				TextColor(t.Ink).MaxLines(1)
			if event.detail != "" {
				ui.Text(c, event.detail).FontSize(theme.TextSmall).TextColor(t.Ink3).MaxLines(2)
			}
		})
	})
	row.OnClick(func() { a.jumpToEntry(event.entry) })
}

// jumpToEntry scrolls the transcript to the row the entry produced. A tool call
// has no row of its own — it is folded behind its turn — so an entry the
// projection did not put on a row lands on the nearest row before it, which is
// that turn's fold.
func (a *App) jumpToEntry(entry int) {
	if entry <= 0 {
		return
	}
	target := entryRowIndex(a.threadRows(a.active), entry)
	if target < 0 {
		return
	}
	a.vm.threadList.ScrollTo(target, ui.Center)
}

// entryRowIndex is the pure half of jumpToEntry: the last row at or before the
// entry, which is where a reader expects the answer to be.
func entryRowIndex(rows []threadRow, entry int) int {
	target := -1
	for i := range rows {
		if rows[i].entry > entry {
			break
		}
		if rows[i].entry > 0 {
			target = i
		}
	}
	return target
}

// timelineTone is the dot's colour: who spoke, and whether something broke.
func timelineTone(t theme.Tokens, kind timelineKind) ui.Color {
	switch kind {
	case tlUser:
		return t.Brand
	case tlAssistant:
		return t.Green
	case tlTool:
		return t.Orange
	case tlError:
		return t.Red
	}
	return t.Ink3
}
