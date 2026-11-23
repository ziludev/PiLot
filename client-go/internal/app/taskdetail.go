package app

// The task dialog: one task, read in full.
//
// The rail's task rows are a list of what the agent has scheduled, and a list
// answers "is anything broken" but not "why". A row that cannot be opened is a
// row the user can only guess about, so every row opens this: what the task is,
// what it was asked, whether it is alive, and — for a task that is running — the
// one action worth having, stop.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// taskDetailPanel is the dialog's state. seq retires a reply from a task the
// user already closed and opened something else on.
type taskDetailPanel struct {
	open bool
	id   int
	seq  int

	busy   bool
	failed string
	task   *api.TaskDetail
	// stopping is set between the click and the bridge's answer, so the button
	// cannot be pressed twice for the same task.
	stopping bool
	// log is what the task did, stage by stage: the task's own turn when it
	// spawned nothing, one stage per child when it did.
	log []api.TaskLogStage
	// expanded is the stage whose steps are open, by conversation id; 0 is none.
	// One at a time — two open stages on a phone-sized dialog is a transcript.
	expanded int
	// stepOpen is the steps opened under their row, by entry id. A call can be
	// read as one line or as what it was told and what it answered; the row
	// above is the index, this is the evidence.
	stepOpen map[int]bool
	// A routine's schedule, being read or being edited: editing swaps the
	// dialog's buttons for the picker and keeps the list behind it untouched.
	// freq is 0 daily, 1 weekdays, 2 weekly, 3 every; at is the time of day
	// the two clock shapes fire at, every the hours between rounds. saving is
	// the replace in flight, saveErr what the bridge said about the last
	// attempt.
	editing bool
	freq    int
	at      time.Time
	every   float64
	saving  bool
	saveErr string
	// scroll is the body's offset, kept across frames so the dialog stays where
	// the reader left it while the task's log streams in.
	scroll ui.ScrollState
}

// openTaskDetail shows the dialog for id and asks the bridge about it.
func (a *App) openTaskDetail(id int) {
	a.detail = taskDetailPanel{open: true, id: id, busy: true}
	a.loadTaskDetail(id)
	a.invalidate()
}

// loadTaskDetail reads one task; the reply lands on the frame that can draw it.
func (a *App) loadTaskDetail(id int) {
	seq := a.detail.seq
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		task, err := a.api.Task(ctx, id)
		a.post(event{kind: evTaskDetail, taskID: id, seq: seq, task: &task, err: err})
	}()
	// The steps come from the store, not from the task record, and they are a
	// transcript's worth of rows: a slow one must not hold up the header.
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		stages, err := a.api.TaskLog(ctx, id)
		a.post(event{kind: evTaskLog, taskID: id, seq: seq, stages: stages, err: err})
	}()
}

// applyTaskDetail fills the dialog, unless the dialog moved on: a slow reply for
// a task nobody is looking at any more must not overwrite the one that is up.
func (a *App) applyTaskDetail(id, seq int, task *api.TaskDetail, err error) {
	if !a.detail.open || a.detail.seq != seq || a.detail.id != id {
		return
	}
	a.detail.busy = false
	if err != nil {
		a.detail.failed = err.Error()
		a.invalidate()
		return
	}
	a.detail.failed = ""
	a.detail.task = task
	// The bridge's list rows carry the same fields the dialog does; a task that
	// finished while the dialog was open says so without a second click.
	a.invalidate()
}

// applyTaskLog fills in the step list. A log that fails to arrive is not the
// dialog's failure: the task's own fields already answered everything it was
// asked except "what did it do", and a red line for a missing bonus row would
// read as the task itself being broken.
func (a *App) applyTaskLog(id, seq int, stages []api.TaskLogStage, err error) {
	if !a.detail.open || a.detail.seq != seq || a.detail.id != id || err != nil {
		return
	}
	a.detail.log = stages
	a.invalidate()
}

// stopTask asks the bridge to stop a running task, then re-reads it: the bridge
// aborts asynchronously, and a dialog that claimed "stopped" without asking
// again would be guessing.
func (a *App) stopTask(id int) {
	if a.detail.stopping {
		return
	}
	a.detail.stopping = true
	seq := a.detail.seq
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		err := a.api.CancelTask(ctx, id)
		a.post(event{kind: evTaskStop, taskID: id, seq: seq, err: err})
	}()
	a.invalidate()
}

// applyTaskStop is the bridge's answer to a stop: either the dialog says it was
// requested, or it says why not.
func (a *App) applyTaskStop(id, seq int, err error) {
	if a.detail.id != id || a.detail.seq != seq {
		return
	}
	a.detail.stopping = false
	if err != nil {
		a.detail.failed = err.Error()
		a.invalidate()
		return
	}
	if a.detail.task != nil {
		a.detail.task.AbortRequested = true
	}
	// A stop is worth saying out loud: nothing on screen changes for a moment
	// after the click, because the bridge aborts asynchronously.
	a.post(event{kind: evNotice, notice: i18n.T("taskStopRequested")})
	a.loadTaskDetail(id)
	a.invalidate()
}

// taskDialogMaxHeight is how tall the dialog grows before its body scrolls
// instead. A fixed height and not a percentage: inside a modal the parent's
// height is not the window's, and a percentage there is read against the
// content the dialog is trying to shrink.
const taskDialogMaxHeight = 700

// canReschedule is whether this task has a clock to move: only a routine keeps
// one, and only while it is asleep. A round in flight is already deciding what
// it says, so its schedule waits for the next sleep.
func (a *App) canReschedule() bool {
	task := a.detail.task
	if task == nil || task.Kind != "app.routine" || a.detail.saving {
		return false
	}
	switch task.Status {
	case "sleeping", "waiting", "pending":
		return true
	}
	return false
}

// scheduleText reads a routine's schedule back as the line a person wrote, so
// the field opens on what is running instead of empty. An unknown shape shows
// nothing rather than a guess that would be saved over the real one.
func scheduleText(input json.RawMessage) string {
	var raw struct {
		Schedule struct {
			Kind    string `json:"kind"`
			At      int    `json:"at"`
			Weekday int    `json:"weekday"`
			EveryMs int64  `json:"everyMs"`
		} `json:"schedule"`
	}
	if len(input) == 0 || json.Unmarshal(input, &raw) != nil {
		return ""
	}
	schedule := raw.Schedule
	clock := fmt.Sprintf("%02d:%02d", schedule.At/60, schedule.At%60)
	switch schedule.Kind {
	case "daily", "weekdays":
		return schedule.Kind + " " + clock
	case "weekly":
		days := []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}
		if schedule.Weekday >= 0 && schedule.Weekday < len(days) {
			return "weekly " + days[schedule.Weekday] + " " + clock
		}
	case "every":
		switch {
		case schedule.EveryMs >= 86_400_000 && schedule.EveryMs%86_400_000 == 0:
			return "every " + itoa(int(schedule.EveryMs/86_400_000)) + "d"
		case schedule.EveryMs >= 3_600_000 && schedule.EveryMs%3_600_000 == 0:
			return "every " + itoa(int(schedule.EveryMs/3_600_000)) + "h"
		case schedule.EveryMs >= 60_000:
			return "every " + itoa(int(schedule.EveryMs/60_000)) + "m"
		}
	}
	return ""
}

// scheduleFields is a routine's clock as the picker holds it.
type scheduleFields struct {
	freq  int
	at    time.Time
	every float64
}

// readSchedule loads a routine's clock out of its input, so the picker opens on
// what is running. A weekly routine is offered as Monday's: it is the only
// weekday the picker can state, and the two routines in use are both Monday's.
func readSchedule(input json.RawMessage, now time.Time) scheduleFields {
	// Six hours is the shape most routines in use take; a routine that fires by
	// the clock overwrites this the moment its input is read.
	fields := scheduleFields{at: now, every: 6}
	var raw struct {
		Schedule struct {
			Kind    string `json:"kind"`
			At      int    `json:"at"`
			EveryMs int64  `json:"everyMs"`
		} `json:"schedule"`
	}
	if len(input) == 0 || json.Unmarshal(input, &raw) != nil {
		return fields
	}
	schedule := raw.Schedule
	switch schedule.Kind {
	case "daily":
		fields.freq = 0
	case "weekdays":
		fields.freq = 1
	case "weekly":
		fields.freq = 2
	case "every":
		fields.freq = 3
		fields.every = float64(schedule.EveryMs) / 3_600_000
		if fields.every < 1 {
			fields.every = 1
		}
		return fields
	default:
		// No clock to load: the picker stays on the hour the dialog opened.
		return fields
	}
	fields.at = time.Date(now.Year(), now.Month(), now.Day(), schedule.At/60, schedule.At%60, 0, 0, now.Location())
	return fields
}

// scheduleLine writes the picker's choice in the words the routine tool takes.
func (a *App) scheduleLine() string {
	clock := a.detail.at.Format("15:04")
	switch a.detail.freq {
	case 1:
		return "weekdays " + clock
	case 2:
		return "weekly mon " + clock
	case 3:
		hours := int(a.detail.every)
		if hours < 1 {
			hours = 1
		}
		return "every " + itoa(hours) + "h"
	}
	return "daily " + clock
}

// startReschedule opens the picker on the schedule that is running now.
func (a *App) startReschedule() {
	task := a.detail.task
	if task == nil {
		return
	}
	fields := readSchedule(task.Input, a.now)
	a.detail.editing = true
	a.detail.freq, a.detail.at, a.detail.every = fields.freq, fields.at, fields.every
	a.detail.saveErr = ""
	a.invalidate()
}

// submitReschedule hands the picked line to the bridge, which replaces the
// sleeping task: a deadline belongs to the sleep it is in, so moving it means
// a new task with the same text and the same chat.
func (a *App) submitReschedule() {
	task := a.detail.task
	if task == nil || a.detail.saving {
		return
	}
	line := a.scheduleLine()
	a.detail.saving = true
	seq := a.detail.seq
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		id, err := a.api.RescheduleTask(ctx, task.ID, line)
		a.post(event{kind: evTaskReschedule, taskID: task.ID, seq: seq, newID: id, err: err})
	}()
	a.invalidate()
}

// applyTaskReschedule turns the dialog onto the task that now owns the clock.
// The old id is dead the moment the bridge aborted it, so staying on it would
// show a schedule nobody is keeping.
func (a *App) applyTaskReschedule(id, seq, newID int, err error) {
	if a.detail.id != id || a.detail.seq != seq {
		return
	}
	a.detail.saving = false
	if err != nil {
		a.detail.saveErr = err.Error()
		a.invalidate()
		return
	}
	a.post(event{kind: evNotice, notice: i18n.T("taskRescheduled")})
	a.detail = taskDetailPanel{open: true, id: newID, seq: seq + 1, busy: true}
	a.loadTaskDetail(newID)
	a.invalidate()
}

// taskDetailDialog draws the task the rail's list opened.
func (a *App) taskDetailDialog(c *ui.Context, t theme.Tokens) {
	if !a.detail.open {
		return
	}
	ui.Modal(c, &a.detail.open, func() {
		ui.Column(c).Width(460).Padding(16).Gap(12).Radius(10).MaxHeight(taskDialogMaxHeight).
			Background(t.Popover).Border(1, t.Border).Children(func() {
			title := a.detailTitle()
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(12.5).FontWeight(600).
					TextColor(t.Foreground).MaxLines(2).Grow(1)
				a.taskStateBadge(c, t)
			})
			// The body scrolls, the title and the buttons do not: a task's input
			// is JSON of whatever length its kind wanted, and a dialog that
			// clipped it to a dozen lines showed a text it could not finish.
			ui.Scroll(c).FillWidth().Grow(1).TrackScroll(&a.detail.scroll).Children(func() {
				ui.Column(c).FillWidth().Gap(12).Children(func() {
					if a.detail.busy && a.detail.task == nil {
						ui.Text(c, i18n.T("loading")).FontSize(11).TextColor(t.MutedFg)
					}
					if a.detail.failed != "" {
						ui.Text(c, a.detail.failed).FontSize(11).TextColor(t.Destructive).MaxLines(4)
					}
					if task := a.detail.task; task != nil {
						a.taskFacts(c, t, task)
						// How far along it is comes before what it was asked: the input can be
						// four hundred lines of JSON, and the stages pushed under it were only
						// reachable by scrolling past the one thing nobody opens this for.
						a.taskStages(c, t, task)
						a.taskInput(c, t, task)
						a.taskOutcome(c, t, task)
					}
				})
			})
			// Stopping is the one thing here that cannot be undone, so it does not sit
			// eight points from the button every hand reaches for: it takes the far
			// end of the row, and close keeps the near one.
			// A routine asleep on a clock is the one task whose setting a person can
			// change from here, so it takes the far end beside stop. While the
			// picker is open it replaces the row: a schedule written as a command
			// line is a syntax to remember, and the shape of a routine's clock is
			// four choices deep.
			if a.detail.editing {
				ui.Column(c).FillWidth().Gap(8).Children(func() {
					ui.Segmented(c, &a.detail.freq,
						i18n.T("freqDaily"), i18n.T("freqWeekdays"),
						i18n.T("freqWeekly"), i18n.T("freqEvery"))
					ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
						if a.detail.freq == 3 {
							ui.NumberInput(c, &a.detail.every, 1, 24, 1)
							ui.Text(c, i18n.T("freqHours")).FontSize(11).TextColor(t.MutedFg)
						} else {
							ui.TimeInput(c, &a.detail.at)
						}
						ui.Row(c).Grow(1)
						a.dialogButton(c, t, i18n.T("save"), true, func() {
							a.submitReschedule()
						})
						a.dialogButton(c, t, i18n.T("cancel"), false, func() {
							a.detail.editing = false
							a.detail.saveErr = ""
							a.invalidate()
						})
					})
				})
				if a.detail.saveErr != "" {
					ui.Text(c, a.detail.saveErr).FontSize(10.5).TextColor(t.Destructive).MaxLines(3)
				}
			} else {
				justify := ui.End
				if a.canStop() || a.canReschedule() {
					justify = ui.SpaceBetween
				}
				ui.Row(c).FillWidth().Justify(justify).Gap(8).Children(func() {
					if a.canReschedule() {
						a.dialogButton(c, t, i18n.T("taskReschedule"), false, func() {
							a.startReschedule()
						})
					}
					if a.canStop() {
						a.dialogButton(c, t, i18n.T("taskStop"), false, func() {
							a.stopTask(a.detail.id)
						})
					}
					a.dialogButton(c, t, i18n.T("close"), true, func() {
						a.detail.open = false
					})
				})
			}
		})
	})
}

// detailTitle names what the dialog is about before anything has arrived: the
// bridge's title when it has one, the task's kind until then.
func (a *App) detailTitle() string {
	if a.detail.task != nil && a.detail.task.Title != "" {
		return a.detail.task.Title
	}
	for _, task := range a.tasks {
		if task.ID == a.detail.id {
			if title := a.taskTitle(task); title != "" {
				return title
			}
		}
	}
	return i18n.T("taskDetail")
}

// canStop is whether stopping would do anything: a terminal task has nothing
// left to abort, and a stop already in flight needs no second one.
func (a *App) canStop() bool {
	task := a.detail.task
	if task == nil || task.AbortRequested || a.detail.stopping {
		return false
	}
	switch task.Status {
	case "running", "waiting", "working", "queued":
		return true
	}
	return false
}

// taskStateBadge is the task's liveness in one word, colored like the rail's
// running dot.
func (a *App) taskStateBadge(c *ui.Context, t theme.Tokens) {
	if a.detail.task == nil {
		return
	}
	label, color := taskStatusLabel(t, a.detail.task)
	ui.Text(c, label).FontSize(9.5).FontWeight(600).TextColor(color).
		Padding(2, 6).Radius(4).Background(t.SidebarAccent)
}

// taskFacts is the four answers a task's header should give: what kind, which
// chat, since when, until when.
func (a *App) taskFacts(c *ui.Context, t theme.Tokens, task *api.TaskDetail) {
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		a.fact(c, t, i18n.T("taskKind"), taskKindLabel(task.Kind))
		if task.ConversationID != 0 {
			a.fact(c, t, i18n.T("taskChat"), a.chatName(task.ConversationID))
		}
		if task.StartedAt > 0 {
			a.fact(c, t, i18n.T("taskStarted"),
				unixTime(task.StartedAt).Format("01-02 15:04")+" · "+since(a.now, unixTime(task.StartedAt)))
		}
		if task.Until > 0 {
			a.fact(c, t, i18n.T("taskNext"), unixTime(task.Until).Format("01-02 15:04"))
		}
		if line := scheduleText(task.Input); line != "" {
			a.fact(c, t, i18n.T("taskSchedule"), line)
		}
		if task.Background {
			a.fact(c, t, i18n.T("taskMode"), i18n.T("taskBackground"))
		}
		a.fact(c, t, i18n.T("taskID"), itoa(task.ID))
		// A stop is not a close: the turn ends, the chat and the files stay where
		// they are. Saying it out loud is the difference between a paused task
		// and one the user thinks they threw away.
		if task.AbortRequested || task.Status == "aborted" || task.Status == "canceled" {
			a.fact(c, t, i18n.T("taskStopped"), i18n.T("taskStopLeaves"))
		}
	})
}

func (a *App) fact(c *ui.Context, t theme.Tokens, label, value string) {
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(10.5).TextColor(t.MutedFg).Width(66)
		ui.Text(c, value).FontSize(11).TextColor(t.Foreground).MaxLines(2).Grow(1)
	})
}

// taskInput is what the task was asked, as the bridge kept it. It is JSON
// because a kind decides its own input shape, and a dialog that guessed at
// fields would show nothing for the kinds it guessed wrong about.
func (a *App) taskInput(c *ui.Context, t theme.Tokens, task *api.TaskDetail) {
	text := rawText(task.Input)
	if text == "" {
		return
	}
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, i18n.T("taskInput")).FontSize(10.5).TextColor(t.MutedFg)
		// 400 lines is a guard against a pasted file, not a summary: the dialog
		// scrolls, so the whole input is one flick away.
		ui.Text(c, clipLines(text, 400)).Font(monoFont).FontSize(10).
			TextColor(t.Foreground).MaxLines(400).Padding(8).Radius(6).
			Background(t.SidebarAccent)
	})
}

// taskOutcome is what the last run produced, which is the only evidence a
// finished task leaves behind.
func (a *App) taskOutcome(c *ui.Context, t theme.Tokens, task *api.TaskDetail) {
	text := rawText(task.Outcome)
	if text == "" {
		return
	}
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, i18n.T("taskOutcome")).FontSize(10.5).TextColor(t.MutedFg)
		ui.Text(c, clipLines(text, 120)).Font(monoFont).FontSize(10).
			TextColor(t.Foreground).MaxLines(120).Padding(8).Radius(6).
			Background(t.SidebarAccent)
	})
}

// taskStageRow is one line of the run as the dialog draws it: a child, or the
// task's own turn when it spawned nothing.
type taskStageRow struct {
	conv   int
	title  string
	state  string
	status stageStatus
	steps  []api.TaskLogStep
	more   int
	open   bool
	// inline rows have nowhere to open to: they are one step, and hiding it
	// behind a click would cost the answer more than it saves the dialog.
	inline bool
}

// The two columns a stage's trailing facts sit in. Fixed, because a state word
// that shifts 13 points left on the one row carrying a failure count stops
// reading as a column — and that is the row the eye goes to first.
const (
	stageStateWidth float32 = 34
	stageFailWidth  float32 = 22
)

// taskStages reads the run as a timeline. The child list said how far each stage
// got; these rows say what it did, because "写测试 · 跑了 · 2 步" never told
// anyone which command went red at 03:40.
func (a *App) taskStages(c *ui.Context, t theme.Tokens, task *api.TaskDetail) {
	rows := a.stageRows(task)
	if len(rows) == 0 {
		return
	}
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			// Indented to where the stage titles start, so the heading names the
			// list under it instead of sitting 18 points out in the margin.
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 0, 0, 18).Children(func() {
				ui.Text(c, i18n.T("taskSteps")).FontSize(10.5).TextColor(t.MutedFg)
				if a.anyClickable(rows) {
					// The dot keeps the hint from reading as one sentence with the label
					// ("步骤点一行看它做了什么") — same size and colour, 8pt apart.
					ui.Text(c, "· "+i18n.T("taskStepsHint")).FontSize(9.5).TextColor(t.MutedFg).MaxLines(1)
				}
			})
			// The count sits at the row's end, on the same edge as the badge above
			// it: "how far along" is a header fact, not part of the hint.
			ui.Row(c).Grow(1)
			taskProgress(c, t, task.SubAgents)
		})
		for i := range rows {
			row := rows[i]
			clickable := len(row.steps) > 0 && row.conv != 0 && !row.inline
			// Left padding only: the right padding kept this row's content on 799
			// while every other column in the dialog ended on 803.
			line := ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Height(20).Radius(4).Padding(0, 0, 0, 4)
			if clickable {
				line = line.Cursor(ui.CursorPointer)
				if row.open {
					line = line.Background(t.SidebarAccent)
				}
				conv := row.conv
				line.OnClick(func() {
					if a.detail.expanded == conv {
						a.detail.expanded = 0
					} else {
						a.detail.expanded = conv
					}
					a.invalidate()
				})
			}
			line.Children(func() {
				glyph := " "
				if clickable {
					glyph = "▸"
					if row.open {
						glyph = "▾"
					}
				}
				ui.Text(c, glyph).FontSize(9).TextColor(t.MutedFg).Width(8)
				ui.Text(c, row.title).FontSize(10.5).TextColor(t.Foreground).MaxLines(1).Grow(1)
				// The state word is the only thing on this row that says whether
				// to stop reading, so it carries the colour the bar gives it. All
				// four words in muted grey made "阶段 2 失败" look like "已结束".
				color := t.MutedFg
				switch row.status {
				case stageFailed:
					color = t.Destructive
				case stageRunning:
					color = t.Warning
				}
				ui.Text(c, row.state).FontSize(9.5).TextColor(color).MaxLines(1).
					Width(stageStateWidth).TextAlign(ui.End).Shrink(0)
				failures := failedSteps(row.steps)
				mark := ""
				if failures > 0 {
					mark = "✗ " + itoa(failures)
				}
				ui.Text(c, mark).FontSize(9.5).TextColor(t.Destructive).MaxLines(1).
					Width(stageFailWidth).TextAlign(ui.End).Shrink(0)
			})
			if row.open || row.inline {
				a.taskStepList(c, t, row.steps, row.more)
			}
		}
	})
}

// stageRows is the list a task's dialog draws. A task with children is read
// through them — that is where a pipeline's work happens. A task without them
// is its own transcript, which is every ordinary turn.
func (a *App) stageRows(task *api.TaskDetail) []taskStageRow {
	rows := []taskStageRow{}
	// The row that started the task comes first, above its stages: "this ran
	// because the agent picked the delegate skill" is the sentence a step list
	// without it cannot say.
	for _, stage := range a.detail.log {
		if !stage.Start || len(stage.Steps) == 0 {
			continue
		}
		rows = append(rows, taskStageRow{
			title:  i18n.T("taskStartedFrom"),
			steps:  stage.Steps,
			inline: true,
		})
	}
	if len(task.SubAgents) > 0 {
		for _, child := range task.SubAgents {
			row := taskStageRow{conv: child.ConversationID, title: child.Title,
				state: taskStateText(child.State), status: stageStatusOf(child.State)}
			if stage := a.stageOf(child.ConversationID); stage != nil {
				row.steps, row.more = stage.Steps, stage.More
				if row.title == "" {
					row.title = stage.Role
				}
			}
			if row.title == "" {
				row.title = i18n.T("taskKind")
			}
			row.open = row.conv != 0 && a.detail.expanded == row.conv
			rows = append(rows, row)
		}
		return rows
	}
	for _, stage := range a.detail.log {
		if !stage.Parent || stage.Start {
			continue
		}
		rows = append(rows, taskStageRow{
			conv:   stage.ConversationID,
			title:  i18n.T("taskDid"),
			state:  taskStateText(task.Status),
			status: stageStatusOf(task.Status),
			steps:  stage.Steps,
			more:   stage.More,
			open:   a.detail.expanded == stage.ConversationID,
		})
	}
	return rows
}

// stageOf is a conversation's part of the run, once the log has arrived.
func (a *App) stageOf(conv int) *api.TaskLogStage {
	for i := range a.detail.log {
		if a.detail.log[i].ConversationID == conv {
			return &a.detail.log[i]
		}
	}
	return nil
}

// anyClickable is whether the hint under the header is true: a list of inline
// rows has nothing to open.
func (a *App) anyClickable(rows []taskStageRow) bool {
	for _, row := range rows {
		if len(row.steps) > 0 && !row.inline {
			return true
		}
	}
	return false
}

func failedSteps(steps []api.TaskLogStep) int {
	n := 0
	for _, step := range steps {
		if step.State == "failed" {
			n++
		}
	}
	return n
}

// taskStepList is one stage's calls, indented under its row. Twenty is the most
// a dialog can hold beside everything else; the tail is what survives, because a
// run ends where the trouble is.
func (a *App) taskStepList(c *ui.Context, t theme.Tokens, steps []api.TaskLogStep, more int) {
	const shown = 20
	kept := steps
	if len(kept) > shown {
		more += len(kept) - shown
		kept = kept[len(kept)-shown:]
	}
	ui.Column(c).FillWidth().Gap(2).Padding(0, 0, 0, 18).Children(func() {
		for _, step := range kept {
			a.taskStepRow(c, t, step)
		}
		if more > 0 {
			ui.Text(c, i18n.TF("taskStepMore", map[string]any{"n": more})).
				FontSize(9.5).TextColor(t.MutedFg)
		}
	})
}

// taskStepRow is one call. A tool call that has something to show opens: the
// row is the index line, and what the tool was told and answered sits under it.
// A reply in words has nothing to open and stays one line.
func (a *App) taskStepRow(c *ui.Context, t theme.Tokens, step api.TaskLogStep) {
	openable := step.Kind == "tool" && (step.Args != "" || step.Output != "")
	open := openable && a.detail.stepOpen[step.ID]
	line := ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Start)
	if openable {
		line = line.Cursor(ui.CursorPointer)
		line.OnClick(func() {
			if a.detail.stepOpen == nil {
				a.detail.stepOpen = map[int]bool{}
			}
			a.detail.stepOpen[step.ID] = !a.detail.stepOpen[step.ID]
			a.invalidate()
		})
	}
	line.Children(func() {
		glyph, color := "✓", t.MutedFg
		switch step.State {
		case "failed":
			glyph, color = "✗", t.Destructive
		case "running":
			glyph, color = "·", t.Warning
		}
		ui.Text(c, glyph).FontSize(9.5).TextColor(color).Width(10)
		if step.Kind == "say" {
			ui.Text(c, step.Text).FontSize(10).TextColor(t.MutedFg).MaxLines(2).Grow(1)
			return
		}
		ui.Text(c, step.Tool).Font(monoFont).FontSize(9.5).TextColor(t.Foreground).MaxLines(1)
		ui.Text(c, step.Label).Font(monoFont).FontSize(9.5).
			TextColor(t.MutedFg).MaxLines(1).Grow(1)
		caret := " "
		if openable {
			caret = "▸"
			if open {
				caret = "▾"
			}
		}
		// Brighter than the label: at 8pt muted grey the caret read as dirt at
		// the end of the row, and a row nobody can tell is openable is a row
		// nobody opens.
		caretColor := t.MutedFg
		if openable {
			caretColor = t.Foreground.Alpha(0.7)
		}
		ui.Text(c, caret).FontSize(11).TextColor(caretColor).Width(11).Shrink(0)
	})
	if !open {
		return
	}
	// Indented to where the tool's name starts, so the blocks read as part of
	// the row they belong to rather than as another step.
	ui.Column(c).FillWidth().Gap(4).Padding(2, 0, 2, 32).Children(func() {
		if step.Args != "" {
			ui.Text(c, i18n.T("taskStepArgs")).FontSize(9).TextColor(t.MutedFg)
			ui.Text(c, clipLines(step.Args, 10)).Font(monoFont).FontSize(9).
				TextColor(t.MutedFg).MaxLines(10).FillWidth().
				Padding(6).Radius(4).Background(t.SidebarAccent)
		}
		if step.Output != "" {
			label, color := i18n.T("taskStepOutput"), t.MutedFg
			if step.State == "failed" {
				label, color = i18n.T("taskStepError"), t.Destructive
			}
			ui.Text(c, label).FontSize(9).TextColor(color)
			ui.Text(c, clipLines(step.Output, 12)).Font(monoFont).FontSize(9).
				TextColor(t.MutedFg).MaxLines(12).FillWidth().
				Padding(6).Radius(4).Background(t.SidebarAccent)
		}
	})
}

// taskStatusLabel puts a state into words and gives it the color the rail would
// have used: running is amber, a sleep is quiet, an end is a check.
func taskStatusLabel(t theme.Tokens, task *api.TaskDetail) (string, ui.Color) {
	fg, dim := t.Warning, t.MutedFg
	switch task.Status {
	case "running", "working":
		return i18n.T("taskRunning"), fg
	case "waiting", "queued":
		return i18n.T("taskQueued"), fg
	case "sleeping":
		if task.Until > 0 {
			return i18n.T("taskSleeping"), dim
		}
		return i18n.T("taskSleepingNoTime"), dim
	}
	if task.AbortRequested {
		return i18n.T("taskAborted"), dim
	}
	return i18n.T("taskDone"), dim
}

// taskStateText puts a child's state into the same words as the parent's: a
// Chinese interface with an English "running" in it reads like a bug.
func taskStateText(state string) string {
	switch state {
	case "running", "working":
		return i18n.T("taskRunning")
	case "waiting", "queued", "pending":
		return i18n.T("taskQueued")
	case "done", "finished", "terminal":
		return i18n.T("taskDone")
	case "failed", "error":
		return i18n.T("taskFailed")
	}
	return state
}

// chatName is a rail title for a conversation id, for the one place a dialog
// needs to say which chat a task belongs to.
func (a *App) chatName(convID int) string {
	for _, s := range a.surfaceSnapshot() {
		if s.ConversationID == convID {
			if s.Title != "" {
				return s.Title
			}
			return "#" + itoa(convID)
		}
	}
	return "#" + itoa(convID)
}

// since says how long ago something started, in the rail's own words.
func since(now, at time.Time) string {
	if at.IsZero() || now.Before(at) {
		return i18n.T("justNow")
	}
	gap := now.Sub(at)
	switch {
	case gap < time.Minute:
		return i18n.T("justNow")
	case gap < time.Hour:
		return i18n.TF("minutesAgo", map[string]any{"n": int(gap.Minutes())})
	case gap < 24*time.Hour:
		return i18n.TF("hoursAgo", map[string]any{"n": int(gap.Hours())})
	}
	return i18n.TF("daysAgo", map[string]any{"n": int(gap.Hours() / 24)})
}

// rawText is a raw message as the dialog shows it: the value re-indented when it
// is JSON, and nothing at all when the bridge kept nothing.
func rawText(raw json.RawMessage) string {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" || text == "{}" || text == "[]" {
		return ""
	}
	return prettyJSON(text)
}

// clipLines keeps the first n lines of a block. The callers pick n as a guard
// against a pasted document, not as a summary: the dialog scrolls, so a task's
// input is readable down to its last field.
func clipLines(text string, n int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= n {
		return text
	}
	kept := strings.Join(lines[:n], "\n")
	if utf8.RuneCountInString(kept) > 4000 {
		kept = string([]rune(kept)[:4000])
	}
	return kept + "\n…"
}
