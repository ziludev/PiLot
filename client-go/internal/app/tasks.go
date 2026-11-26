package app

import (
	"sort"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
	"pilot/internal/icons"
	"pilot/internal/protocol"
	"pilot/internal/theme"
)

// The task half of the rail: three groups, in the order the agent's day moves,
// and rows that name the task and its next fire.

// taskGroups is the order the rail draws them in.
var taskGroups = []struct {
	state string
	label string
}{
	{"working", "groupWorking"},
	{"waiting", "groupWaiting"},
	{"sleeping", "groupSleeping"},
}

// taskRows projects the task list: one header per non-empty group, then its
// rows, filtered by the rail's search box.
func (a *App) taskRows(filter, state string) []railRow {
	rows := make([]railRow, 0, len(a.tasks)+len(taskGroups))
	for _, group := range taskGroups {
		if state != "" && state != group.state {
			continue
		}
		var tasks []railRow
		for _, task := range a.tasks {
			if taskState(task) != group.state {
				continue
			}
			title := a.taskTitle(task)
			if filter != "" && !containsFold(title, filter) {
				continue
			}
			tasks = append(tasks, railRow{
				kind: railTask,
				task: task,
				when: clockTime(unixTime(task.Until)),
				cost: money(task.Spend),
			})
		}
		if len(tasks) == 0 {
			continue
		}
		// Soonest first: a group is a queue of what happens next, so the task
		// that runs in a minute belongs at the top. A task with no time at all
		// (one running now, or one waiting on a human) goes above the timed
		// ones — it is the one asking for something.
		sort.SliceStable(tasks, func(i, j int) bool {
			a, b := tasks[i].task.Until, tasks[j].task.Until
			if (a == 0) != (b == 0) {
				return a == 0
			}
			return a < b
		})
		rows = append(rows, railRow{kind: railGroup, label: i18n.T(group.label), count: len(tasks)})
		rows = append(rows, tasks...)
	}
	if len(rows) == 0 {
		label := i18n.T("noTasks")
		if filter != "" {
			label = i18n.T("noMatches")
		}
		rows = append(rows, railRow{kind: railHint, label: label})
	}
	return rows
}

// taskState folds the bridge's task states into the rail's three groups: a task
// whose time has passed is about to run, so it is working.
func taskState(task protocol.Task) string {
	switch task.State {
	case "waiting":
		return "waiting"
	case "sleeping":
		return "sleeping"
	}
	return "working"
}

// taskTitle is what the row says about a task. The bridge names a task when the
// task knows what it is about; otherwise the chat it belongs to is the honest
// answer, because a turn's kind — "pi.generation" — is the same for every turn
// ever run, and says nothing about this one.
func (a *App) taskTitle(task protocol.Task) string {
	if task.Title != "" {
		return task.Title
	}
	if task.ConversationID != 0 {
		for _, s := range a.surfaceSnapshot() {
			if s.ConversationID == task.ConversationID && s.Title != "" {
				return s.Title
			}
		}
	}
	return taskKindLabel(task.Kind)
}

// taskKindLabel puts the scheduler's name for a kind into words.
func taskKindLabel(kind string) string {
	switch kind {
	case "pi.generation":
		return i18n.T("taskTurn")
	case "pi.tool":
		return i18n.T("taskTool")
	case "app.routine":
		return i18n.T("taskRoutine")
	case "app.digest":
		return i18n.T("taskDigest")
	case "app.usage-sampler":
		return i18n.T("taskSampler")
	case "app.reminder":
		return i18n.T("taskReminder")
	case "app.feature":
		return i18n.T("taskFeature")
	}
	return kind
}

func (a *App) railTaskRow(c *ui.Context, t theme.Tokens, row railRow) {
	// Beautiful UI's TaskRows: 44 tall, a 24px badge, the name at 13 medium,
	// the amount right-aligned in tabular figures, and one pill for the state.
	item := ui.ButtonBase(c).FillWidth().Height(44).Radius(theme.RadiusControl).Padding(4, 8).
		Cursor(ui.CursorPointer).Gap(10).AlignItems(ui.Center).
		OnClick(func() { a.openTaskDetail(row.task.ID) })
	if item.Hovered() {
		item = item.Background(t.Hover)
	}
	name := a.taskTitle(row.task)
	tone := taskTone(t, row.task.State)
	// The rail is 232 wide: a name, a pill and a time column do not fit, so the
	// time joins the second line the way TaskRows keeps its own second line.
	sub := taskKindLabel(row.task.Kind)
	if sub == name {
		// 用量采样 over 用量采样: the kind said nothing the name did not.
		sub = ""
	}
	if row.when != "" {
		if sub != "" {
			sub += " · "
		}
		sub += row.when
	}
	item.Children(func() {
		ui.Box(c).Size(24, 24).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			// A running task gets a spinner rather than a glyph: the badge is
			// 24 points of answer to "is anything happening".
			if row.task.State == "working" || row.task.State == "running" {
				ui.Spinner(c).Size(13, 13).TextColor(t.Brand)
				return
			}
			ui.Icon(c, tone.icon).Size(14, 14).TextColor(tone.color)
		})
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, name).FontSize(theme.TextBody).FontWeight(500).TextColor(t.Ink).MaxLines(1)
			if sub != "" {
				ui.Text(c, sub).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
			}
		})
		taskStatePill(c, t, row.task.State)
	})
}

// taskStatePill is the one word that says what the row is doing, on a tinted
// pill 22 tall — the shape TaskRows uses instead of a second text line.
func taskStatePill(c *ui.Context, t theme.Tokens, state string) {
	tone := taskTone(t, state)
	ui.Row(c).Height(22).Padding(0, 8).Radius(11).AlignItems(ui.Center).
		Background(tone.tint).Children(func() {
		ui.Text(c, taskStateLabel(state)).FontSize(theme.TextSmall).FontWeight(500).
			TextColor(tone.color).MaxLines(1)
	})
}

// taskStateLabel is the pill's one word. The group headings above the rows use
// the same words, so a row and its heading cannot name the same state twice.
func taskStateLabel(state string) string {
	switch state {
	case "working", "running":
		return i18n.T("groupWorking")
	case "waiting":
		return i18n.T("groupWaiting")
	case "sleeping":
		return i18n.T("groupSleeping")
	}
	return i18n.T("groupWorking")
}

// taskTone is one state's icon, ink and tint, so a row and its pill cannot
// disagree about what colour running is.
type taskToneDef struct {
	icon  *ui.SVG
	color ui.Color
	tint  ui.Color
}

func taskTone(t theme.Tokens, state string) taskToneDef {
	switch state {
	case "working", "running":
		return taskToneDef{icons.Clock, t.Brand, t.BrandTint}
	case "waiting":
		return taskToneDef{icons.Clock, t.Orange, t.OrangeTint}
	case "sleeping":
		return taskToneDef{icons.Moon, t.Ink3, t.Field}
	}
	return taskToneDef{icons.Check, t.Green, t.GreenTint}
}

// taskTableFoot is the task table's foot: how much work there is, and how much
// of it is running. The per-state counts live on the filter chips, which is
// where they are actionable; the foot only totals.
func (a *App) taskTableFoot(c *ui.Context, t theme.Tokens) {
	working := 0
	for _, task := range a.tasks {
		if task.State == "working" || task.State == "running" {
			working++
		}
	}
	ui.Box(c).FillWidth().Height(1).Background(t.Line)
	ui.Row(c).FillWidth().Height(30).Padding(0, 8).Gap(6).AlignItems(ui.Center).
		Background(t.Inset).Children(func() {
		ui.Text(c, i18n.TF("taskSum", map[string]any{"n": len(a.tasks)})).
			FontSize(theme.TextSmall).FontWeight(500).TextColor(t.Ink2).Grow(1).MaxLines(1)
		if working > 0 {
			ui.Text(c, itoa(working)+" "+i18n.T("groupWorking")).FontSize(theme.TextMicro).
				TextColor(t.Brand).MaxLines(1)
		}
	})
}

// taskSegNarrow is the rail width under which the four segments stop sharing one
// row. At 224 the widest label has nothing left for the gap beside it, so the
// row folds into two; the default rail is wider and keeps one.
const taskSegNarrow = 224

// taskSegment is one filter choice: the state it selects (empty is 全部) and the
// words it is drawn with.
type taskSegment struct {
	state string
	label string
}

// taskFilterRow is the task tab's filter: one segment per state. The counts live
// in the group headings below, where the rows they count are; repeating them
// here made "Sleeping 7" wider than the rail could hold.
// It only exists on that tab — the chat tab is filtered by the search box.
func (a *App) taskFilterRow(c *ui.Context, t theme.Tokens) {
	segments := []taskSegment{
		{"", i18n.T("taskAll")},
		{"working", i18n.T("groupWorking")},
		{"waiting", i18n.T("groupWaiting")},
		{"sleeping", i18n.T("groupSleeping")},
	}
	perRow := len(segments)
	if a.railW <= taskSegNarrow {
		perRow = 2
	}
	track := ui.Column(c).FillWidth().Padding(2).Gap(2).
		Radius(theme.RadiusControl).Background(t.Hover)
	track.Children(func() {
		for start := 0; start < len(segments); start += perRow {
			end := start + perRow
			if end > len(segments) {
				end = len(segments)
			}
			a.taskSegmentRow(c, t, segments[start:end])
		}
	})
}

// taskTableHead is Records Table's header row over the task list: a name column
// that grows, a time column on the right, and a hairline under both. Two labels
// and a rule are what turn a list of rows into a table.
func (a *App) taskTableHead(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Children(func() {
		// Records Table's header row: 35 tall, 12.5 at 500 in the second ink.
		ui.Row(c).FillWidth().Height(35).Padding(0, 12).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, i18n.T("tasks")).Grow(1).FontSize(theme.TextRow).
				FontWeight(500).TextColor(t.Ink2).MaxLines(1)
			ui.Text(c, i18n.T("taskNextRun")).FontSize(theme.TextRow).
				FontWeight(500).TextColor(t.Ink2).MaxLines(1)
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Line)
	})
}

// taskSegmentRow draws one line of the filter: each segment grows equally, so
// the row is as wide as the rail and no wider.
func (a *App) taskSegmentRow(c *ui.Context, t theme.Tokens, segments []taskSegment) {
	ui.Row(c).FillWidth().Height(24).Gap(2).AlignItems(ui.Center).Children(func() {
		for _, segment := range segments {
			segment := segment
			active := a.taskFilter == segment.state
			btn := ui.ButtonBase(c).Grow(1).Height(24).Radius(theme.RadiusChip).
				Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer)
			if active {
				btn = btn.Background(t.Surface)
			} else if btn.Hovered() {
				btn = btn.Background(t.Hover2)
			}
			btn.Children(func() {
				color := t.Ink2
				weight := 500
				if active {
					color, weight = t.Ink, 600
				}
				ui.Text(c, segment.label).FontSize(theme.TextMicro).FontWeight(weight).
					TextColor(color).MaxLines(1)
			})
			btn.OnClick(func() {
				a.taskFilter = segment.state
				a.invalidate()
			})
		}
	})
}
