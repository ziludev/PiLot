package app

import (
	"sort"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
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
	// The name over what kind of task it is: two lines, because a clipped name
	// with its id on the same line is a task you cannot recognise.
	item := ui.ButtonBase(c).FillWidth().Height(40).Radius(theme.RadiusControl).Padding(3, 8).
		Cursor(ui.CursorPointer).Gap(8).AlignItems(ui.Center).
		OnClick(func() { a.openTaskDetail(row.task.ID) })
	item.Children(func() {
		name, kind := a.taskTitle(row.task), taskKindLabel(row.task.Kind)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, name).FontSize(theme.TextRow).FontWeight(500).TextColor(t.Ink2).MaxLines(1)
			// A task with nothing to name it but its kind says it once, not
			// twice under itself.
			if kind != name {
				ui.Text(c, kind).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
			}
		})
		if row.when != "" || row.cost != "" {
			ui.Column(c).Gap(1).AlignItems(ui.End).Children(func() {
				if row.when != "" {
					ui.Text(c, i18n.TF("nextAt", map[string]any{"time": row.when})).
						FontSize(theme.TextMicro).TextColor(t.Ink3)
				}
				if row.cost != "" {
					ui.Text(c, row.cost).FontSize(theme.TextMicro).TextColor(t.Ink3)
				}
			})
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
		ui.Row(c).FillWidth().Height(22).Padding(0, 4).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, i18n.T("tasks")).Grow(1).FontSize(theme.TextMicro).
				FontWeight(600).TextColor(t.Ink3).MaxLines(1)
			ui.Text(c, i18n.T("taskNextRun")).FontSize(theme.TextMicro).
				FontWeight(600).TextColor(t.Ink3).MaxLines(1)
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
