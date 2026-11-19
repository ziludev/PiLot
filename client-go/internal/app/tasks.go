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
			title := taskTitle(task)
			if filter != "" && !containsFold(title, filter) {
				continue
			}
			tasks = append(tasks, railRow{kind: railTask, task: task, when: clockTime(unixTime(task.Until))})
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

func taskTitle(task protocol.Task) string {
	if task.Title != "" {
		return task.Title
	}
	return task.Kind
}

func (a *App) railTaskRow(c *ui.Context, t theme.Tokens, row railRow) {
	// Two lines of title: a task name plus its id and the next fire do not fit
	// on one line of a 260pt rail, and a clipped name is a task you cannot
	// recognise.
	item := ui.ButtonBase(c).FillWidth().Height(34).Radius(5).Padding(2, 7).
		Gap(6).AlignItems(ui.Center)
	item.Children(func() {
		ui.Text(c, "#"+itoa(row.task.ID)+" · "+taskTitle(row.task)).Grow(1).
			FontSize(11).TextColor(t.Foreground).MaxLines(2)
		if row.when != "" {
			ui.Text(c, i18n.TF("nextAt", map[string]any{"time": row.when})).
				FontSize(9).TextColor(t.MutedFg)
		}
	})
}
