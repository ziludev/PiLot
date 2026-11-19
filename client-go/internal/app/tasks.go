package app

import (
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
func (a *App) taskRows(filter string) []railRow {
	rows := make([]railRow, 0, len(a.tasks)+len(taskGroups))
	for _, group := range taskGroups {
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
		rows = append(rows, railRow{kind: railGroup, label: i18n.T(group.label)})
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
	item := ui.ButtonBase(c).FillWidth().Height(34).Radius(5).Padding(2, 7).
		Cursor(ui.CursorPointer).Gap(6).AlignItems(ui.Center).
		OnClick(func() { a.openTaskDetail(row.task.ID) })
	item.Children(func() {
		name, kind := a.taskTitle(row.task), taskKindLabel(row.task.Kind)
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, name).FontSize(11).TextColor(t.Foreground).MaxLines(1)
			// A task with nothing to name it but its kind says it once, not
			// twice under itself.
			if kind != name {
				ui.Text(c, kind).FontSize(9).TextColor(t.MutedFg).MaxLines(1)
			}
		})
		if row.when != "" || row.cost != "" {
			ui.Column(c).Gap(1).AlignItems(ui.End).Children(func() {
				if row.when != "" {
					ui.Text(c, i18n.TF("nextAt", map[string]any{"time": row.when})).
						FontSize(9).TextColor(t.MutedFg)
				}
				if row.cost != "" {
					ui.Text(c, row.cost).FontSize(9).TextColor(t.MutedFg)
				}
			})
		}
	})
}
