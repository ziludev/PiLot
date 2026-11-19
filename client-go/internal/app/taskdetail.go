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
				a.taskChildren(c, t, task)
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

// taskChildren names the sub-agents a task spawned: a running task's work is in
// its children, not in its own record.
func (a *App) taskChildren(c *ui.Context, t theme.Tokens, task *api.TaskDetail) {
	if len(task.SubAgents) == 0 {
		return
	}
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		ui.Text(c, i18n.T("taskChildren")).FontSize(10.5).TextColor(t.MutedFg)
		for _, child := range task.SubAgents {
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, taskKindLabel(child.Kind)).FontSize(10.5).
					TextColor(t.Foreground).MaxLines(1)
				ui.Text(c, child.Title).FontSize(10.5).TextColor(t.MutedFg).
					MaxLines(1).Grow(1)
				ui.Text(c, taskStateText(child.State)).FontSize(9.5).TextColor(t.MutedFg)
			})
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
