package app

// How far a task with children has got, in one line.
//
// A step list answers "what did it do", and only by being read line by line. A
// task that spawns four children (build_feature: design → implement → test →
// review) has a denominator the step list never had — the children themselves —
// and "▲▲▲△ 3/4" answers "how far along" without counting rows. The shape is
// assistant-ui's ToolTimeline reduced to what a 460pt dialog can hold: a bar and
// the two numbers behind it, on the header of the timeline it summarises.
//
// It stays a bar of *stages*, not of steps: steps are what already happened, so
// a bar over them would always be nearly full and would jump as the agent found
// more work. Children are decided in advance by whoever spawned them, which is
// the only honest denominator here.

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// stageTally counts a task's children. A failed stage is spent, not pending:
// counting it in the denominator but not the numerator would make a pipeline
// that broke at stage three read as though it never started.
func stageTally(children []api.TaskSubAgent) (done, total, failed int) {
	total = len(children)
	for _, child := range children {
		switch child.State {
		case "done", "finished", "terminal":
			done++
		case "failed", "error":
			done++
			failed++
		case "aborted", "canceled":
			done++
		}
	}
	return done, total, failed
}

// taskProgress draws that bar and says the numbers out loud, because a bar alone
// makes the reader estimate and an estimate is not an answer. Nothing is drawn
// without children: a task that ran in one turn has no planned stages, and a bar
// that is always full is decoration.
func taskProgress(c *ui.Context, t theme.Tokens, children []api.TaskSubAgent) {
	done, total, failed := stageTally(children)
	if total == 0 {
		return
	}
	// Amber while there is work left, red once a stage broke, green when all of
	// them came back — the same three readings the rail's running dot gives.
	color := t.Warning
	switch {
	case failed > 0:
		color = t.Destructive
	case done == total:
		color = t.Success
	}
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		// 88pt wide and 4pt tall: a bar that can be read at a glance beside a
		// 10.5pt label, not a chart of its own.
		ui.Meter(c, float64(done), 0, float64(total), &ui.MeterLevels{NormalColor: color}).
			Width(96).Height(4).MinWidth(0).Shrink(0).Label(i18n.T("taskProgress"))
		ui.Text(c, itoa(done)+"/"+itoa(total)).Font(monoFont).FontSize(9.5).
			TextColor(t.MutedFg).MaxLines(1)
	})
}
