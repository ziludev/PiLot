package app

import (
	"sort"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/motion"
	"pitta/internal/protocol"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// railTaskListLabel names the jobs view, which is how the tests that open and
// measure it find it.
const railTaskListLabel = "rail-tasks"

// railTaskList is the tasks view: every task the owner started, grouped by what
// it is doing — working, waiting on them, or asleep until its next fire.
//
// It takes the room under the filters instead of the 40-point strip that used
// to sit at the foot of the rail: a durable task that wakes after a restart is
// what this client is for, and the strip showed two rows of it with no heading
// and no next fire, so a routine that wakes every five minutes looked exactly
// like a reminder that had already run.
func (a *App) railTaskList(c *ui.Context, t theme.Tokens) {
	tasks := railLiveTasks(a.tasks)
	if len(tasks) == 0 {
		ui.Column(c).Grow(1).FillWidth().Padding(16, 12).Gap(3).
			Label(railTaskListLabel).Transition(motion.FadeIn()).Children(func() {
			ui.Text(c, i18n.T("noTasks")).FontSize(11).TextColor(t.MutedFg).Wrap()
			ui.Text(c, i18n.T("noTasksHint")).FontSize(10).TextColor(t.MutedFg).Wrap()
		})
		return
	}
	a.noteTaskAges(tasks)
	ui.Scroll(c).Grow(1).FillWidth().Label(railTaskListLabel).Children(func() {
		ui.Column(c).FillWidth().Padding(2, 6, 6, 6).Children(func() {
			for _, group := range railTaskGroups(tasks) {
				ui.Row(c).FillWidth().Height(22).PaddingX(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, group.label).FontSize(9).FontWeight(600).TextColor(t.MutedFg).
						Grow(1).MaxLines(1)
					ui.Text(c, itoa(len(group.tasks))).FontSize(9).TextColor(t.MutedFg)
				})
				for _, task := range group.tasks {
					a.railTaskRow(c, t, task)
				}
			}
			// A job that has run for four minutes and one that has run for
			// forty look the same without a clock, and the difference is the
			// whole question of whether it is working or stuck. Five seconds is
			// the cheapest tick that keeps the number from looking frozen: every
			// tick rebuilds the whole window, transcript included.
			ui.Row(c).Height(0).Loop("elapsed", 5*time.Second, ui.Linear)
		})
	})
}

// railTaskRow is one task: its dot and name over what kind of task it is, and
// at the right end what it is doing right now.
func (a *App) railTaskRow(c *ui.Context, t theme.Tokens, task protocol.Task) {
	row := ui.Row(c).FillWidth().Height(34).PaddingX(6).Gap(6).Radius(6).
		Cursor(ui.CursorPointer).AlignItems(ui.Center)
	if row.Hovered() {
		row.Background(c.Theme().SurfaceHover)
	}
	row.OnClick(func() { a.openTask(task.ID) })
	row.Children(func() {
		ui.Box(c).Size(6, 6).Radius(3).AlignSelf(ui.Center).
			Background(railTaskColor(t, task.State))
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, railTaskTitle(task)).FontSize(11).TextColor(t.SidebarFg).MaxLines(1)
			ui.Text(c, "#"+itoa(task.ID)+" · "+railTaskLabel(task.Kind)).
				FontSize(9).TextColor(t.MutedFg).MaxLines(1)
		})
		ui.Text(c, a.taskRightLabel(task)).FontSize(9.5).TextColor(t.MutedFg).Shrink(0)
	})
}

// railTaskGroup is one heading of the jobs view and what sits under it.
type railTaskGroup struct {
	label string
	tasks []protocol.Task
}

// railTaskGroups splits the tasks the way the owner reads them: what is working
// now, what is holding on them, what is asleep. Sleeping sorts by when it
// wakes, so the next thing to happen is the next row; everything else keeps the
// bridge's order.
func railTaskGroups(tasks []protocol.Task) []railTaskGroup {
	working, waiting, sleeping := []protocol.Task{}, []protocol.Task{}, []protocol.Task{}
	for _, task := range tasks {
		switch task.State {
		case "running", "completing":
			working = append(working, task)
		case "waiting", "blocked", "faulted":
			waiting = append(waiting, task)
		default:
			sleeping = append(sleeping, task)
		}
	}
	sort.SliceStable(sleeping, func(i, j int) bool { return sleeping[i].Until < sleeping[j].Until })
	groups := []railTaskGroup{
		{label: railTaskState("running"), tasks: working},
		{label: i18n.T("needsYou"), tasks: waiting},
		{label: railTaskState("sleeping"), tasks: sleeping},
	}
	kept := groups[:0]
	for _, group := range groups {
		if len(group.tasks) > 0 {
			kept = append(kept, group)
		}
	}
	return kept
}

// noteTaskAges remembers when each live job first showed up, which is the only
// start time the client has: protocol.Task carries no timestamp of its own.
func (a *App) noteTaskAges(tasks []protocol.Task) {
	if len(tasks) == 0 {
		a.taskSince = nil
		return
	}
	now := time.Now()
	next := make(map[int]time.Time, len(tasks))
	for _, task := range tasks {
		if !taskUnfinished(task.State) {
			continue
		}
		if since, ok := a.taskSince[task.ID]; ok {
			next[task.ID] = since
			continue
		}
		next[task.ID] = now
	}
	a.taskSince = next
}

// taskRightLabel is what sits at the right end of a job's row: when it wakes
// while it sleeps, how long it has been going while it goes, and what it is
// waiting for when it is not.
func (a *App) taskRightLabel(task protocol.Task) string {
	if task.State == "sleeping" {
		if next := nextFireLabel(task.Until); next != "" {
			return next
		}
	}
	if !taskUnfinished(task.State) || task.State == "waiting" || task.State == "blocked" {
		return railTaskState(task.State)
	}
	since, ok := a.taskSince[task.ID]
	if !ok {
		return railTaskState(task.State)
	}
	elapsed := time.Since(since).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	// Sub-second noise ("37ms") reads as a glitch, not as a clock.
	if elapsed < 1000 {
		elapsed = 1000
	}
	return turns.Clock(elapsed)
}

// nextFireLabel is when a sleeping task wakes, in the shortest form that is
// still unambiguous: a bare time when that is today, with the date in front of
// it when it is not.
func nextFireLabel(until int64) string {
	if until <= 0 {
		return ""
	}
	at := time.UnixMilli(until).In(time.Local)
	now := time.Now()
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return i18n.T("nextFire") + " " + at.Format("15:04")
	}
	return at.Format("01-02 15:04")
}
