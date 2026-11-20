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

// taskDetailDialog draws the task the rail's list opened.
func (a *App) taskDetailDialog(c *ui.Context, t theme.Tokens) {
	if !a.detail.open {
		return
	}
	ui.Modal(c, &a.detail.open, func() {
		ui.Column(c).Width(460).Padding(16).Gap(12).Radius(10).
			Background(t.Popover).Border(1, t.Border).Children(func() {
			title := a.detailTitle()
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, title).FontSize(12.5).FontWeight(600).
					TextColor(t.Foreground).MaxLines(2).Grow(1)
				a.taskStateBadge(c, t)
			})
			if a.detail.busy && a.detail.task == nil {
				ui.Text(c, i18n.T("loading")).FontSize(11).TextColor(t.MutedFg)
			}
			if a.detail.failed != "" {
				ui.Text(c, a.detail.failed).FontSize(11).TextColor(t.Destructive).MaxLines(4)
			}
			if task := a.detail.task; task != nil {
				a.taskFacts(c, t, task)
				a.taskInput(c, t, task)
				a.taskOutcome(c, t, task)
				a.taskStages(c, t, task)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				if a.canStop() {
					a.dialogButton(c, t, i18n.T("taskStop"), false, func() {
						a.stopTask(a.detail.id)
					})
				}
				a.dialogButton(c, t, i18n.T("close"), true, func() {
					a.detail.open = false
				})
			})
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
		ui.Text(c, clipLines(text, 12)).Font(monoFont).FontSize(10).
			TextColor(t.Foreground).MaxLines(14).Padding(8).Radius(6).
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
		ui.Text(c, clipLines(text, 8)).Font(monoFont).FontSize(10).
			TextColor(t.Foreground).MaxLines(10).Padding(8).Radius(6).
			Background(t.SidebarAccent)
	})
}

// taskStageRow is one line of the run as the dialog draws it: a child, or the
// task's own turn when it spawned nothing.
type taskStageRow struct {
	conv  int
	title string
	state string
	steps []api.TaskLogStep
	more  int
	open  bool
}

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
			ui.Text(c, i18n.T("taskSteps")).FontSize(10.5).TextColor(t.MutedFg).Grow(1)
			if a.anySteps(rows) {
				ui.Text(c, i18n.T("taskStepsHint")).FontSize(9.5).TextColor(t.MutedFg).MaxLines(1)
			}
		})
		for i := range rows {
			row := rows[i]
			clickable := len(row.steps) > 0 && row.conv != 0
			line := ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Height(20).Radius(4).Padding(0, 4)
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
				if row.state != "" {
					ui.Text(c, row.state).FontSize(9.5).TextColor(t.MutedFg).MaxLines(1)
				}
				if failures := failedSteps(row.steps); failures > 0 {
					ui.Text(c, "✗ "+itoa(failures)).FontSize(9.5).TextColor(t.Destructive).MaxLines(1)
				}
			})
			if row.open {
				a.taskStepList(c, t, row.steps, row.more)
			}
		}
	})
}

// stageRows is the list a task's dialog draws. A task with children is read
// through them — that is where a pipeline's work happens. A task without them
// is its own transcript, which is every ordinary turn.
func (a *App) stageRows(task *api.TaskDetail) []taskStageRow {
	if len(task.SubAgents) > 0 {
		rows := make([]taskStageRow, 0, len(task.SubAgents))
		for _, child := range task.SubAgents {
			row := taskStageRow{conv: child.ConversationID, title: child.Title, state: taskStateText(child.State)}
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
		if !stage.Parent {
			continue
		}
		return []taskStageRow{{
			conv:  stage.ConversationID,
			title: i18n.T("taskDid"),
			state: taskStateText(task.Status),
			steps: stage.Steps,
			more:  stage.More,
			open:  a.detail.expanded == stage.ConversationID,
		}}
	}
	return nil
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

func (a *App) anySteps(rows []taskStageRow) bool {
	for _, row := range rows {
		if len(row.steps) > 0 {
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
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Start).Children(func() {
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
				if step.Label != "" {
					ui.Text(c, step.Label).Font(monoFont).FontSize(9.5).
						TextColor(t.MutedFg).MaxLines(1).Grow(1)
				}
			})
		}
		if more > 0 {
			ui.Text(c, i18n.TF("taskStepMore", map[string]any{"n": more})).
				FontSize(9.5).TextColor(t.MutedFg)
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

// clipLines keeps the first n lines of a block: an input or an outcome can be a
// whole document, and the dialog is not the place to read it.
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
