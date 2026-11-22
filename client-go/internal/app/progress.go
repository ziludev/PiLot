package app

// How far a task with children has got, in one line.
//
// A step list answers "what did it do", and only by being read line by line. A
// task that spawns four children (build_feature: design → implement → test →
// review) has a denominator the step list never had — the children themselves —
// and one segment per child answers "how far along" without counting rows. The
// shape is assistant-ui's ToolTimeline reduced to what a 460pt dialog can hold:
// the stages as a row of ticks and the two numbers behind them, on the header of
// the timeline it summarises.
//
// It stays a bar of *stages*, not of steps: steps are what already happened, so
// a bar over them would always be nearly full and would jump as the agent found
// more work. Children are decided in advance by whoever spawned them, which is
// the only honest denominator here.
//
// One segment per stage rather than one filled bar: a single amber fill painted
// the three finished stages as "three quarters wrong" the moment stage two went
// red. Segments say which stage broke, and they can be counted even when the
// track is nearly invisible in dark mode.

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// stageStatus is how one child of a task ended up — the four readings a
// pipeline actually has.
type stageStatus int

const (
	stageQueued stageStatus = iota
	stageRunning
	stageDone
	stageFailed
)

func stageStatusOf(state string) stageStatus {
	switch state {
	case "done", "finished", "completed", "terminal":
		return stageDone
	case "failed", "error":
		return stageFailed
	case "running", "working", "started", "active", "in_progress":
		return stageRunning
	// An aborted stage was still spent: the pipeline will not come back to it,
	// so it is not queued and it is not broken.
	case "aborted", "canceled":
		return stageDone
	}
	return stageQueued
}

// stageTally counts a task's children. A failed stage is spent, not pending:
// counting it in the denominator but not the numerator would make a pipeline
// that broke at stage three read as though it never started.
func stageTally(children []api.TaskSubAgent) (done, total, failed int) {
	total = len(children)
	for _, child := range children {
		switch stageStatusOf(child.State) {
		case stageDone:
			done++
		case stageFailed:
			done++
			failed++
		}
	}
	return done, total, failed
}

// taskProgress draws those segments and says the numbers out loud, because ticks
// alone make the reader estimate and an estimate is not an answer. Nothing is
// drawn without children: a task that ran in one turn has no planned stages, and
// a bar that is always full is decoration.
func taskProgress(c *ui.Context, t theme.Tokens, children []api.TaskSubAgent) {
	done, total, failed := stageTally(children)
	if total == 0 {
		return
	}
	const (
		barWidth float32 = 96
		barGap   float32 = 4
	)
	segment := (barWidth - barGap*float32(total-1)) / float32(total)
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Height(4).Gap(barGap).Label(i18n.T("taskProgress")).Role(ui.RoleProgress).
			Children(func() {
				for _, child := range children {
					// Queued is the track: a wash of the foreground, not a theme border
					// — the border token is nearly the popover's own colour in dark
					// mode, which made a four-stage bar read as three. Not fainter
					// than a quarter either: at 16% the empty segments disappeared
					// and the bar was counted as 3.
					color := t.Foreground.Alpha(0.26)
					switch stageStatusOf(child.State) {
					case stageDone:
						color = t.Success
					case stageFailed:
						color = t.Destructive
					case stageRunning:
						color = t.Warning
					}
					ui.Box(c).Width(segment).Height(4).Radius(2).Shrink(0).Background(color)
				}
			})
		// Red once a stage broke, green when all of them came back. The count is
		// the one exact reading on this control, so the in-between case is a
		// dimmed foreground, not muted grey: muted put it below the segments it
		// was summarising.
		color := t.Foreground.Alpha(0.7)
		switch {
		case failed > 0:
			color = t.Destructive
		case done == total:
			color = t.Success
		}
		ui.Text(c, itoa(done)+"/"+itoa(total)).Font(monoFont).FontSize(9.5).
			TextColor(color).MaxLines(1)
	})
}
