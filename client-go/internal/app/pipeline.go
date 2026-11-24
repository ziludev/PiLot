package app

import (
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// The pipeline tab: the branch's latest run, its jobs grouped by stage, and one
// job's log when a row is clicked. Reading is a GET on the drawer's own
// conversation, so the answer lands back on the chat it was asked for.

// jobStatusTone is the dot beside a job: the same colours the transcript uses
// for a step that worked, failed or is still going.
func jobStatusTone(t theme.Tokens, status string) ui.Color {
	switch status {
	case "success":
		return t.Green
	case "failed":
		return t.Red
	case "running", "pending":
		return t.Brand
	case "canceled", "cancelled":
		return t.Ink3
	case "manual":
		return t.Orange
	case "skipped":
		return t.Ink3
	}
	return t.Ink3
}

// refreshPipeline reads the branch's latest run for the drawer's conversation.
func (a *App) refreshPipeline() {
	conv := a.changes.conv
	if a.api == nil || conv == 0 {
		return
	}
	ctx := a.ctx
	client := a.api
	a.changes.ciConv = conv
	a.changes.ciBusy = true
	a.invalidate()
	go func() {
		got, err := client.Pipeline(ctx, conv)
		a.post(event{kind: evPipeline, convID: conv, pipeline: got, err: err})
	}()
}

// applyPipeline takes the read's answer only for the chat it was read for.
func (a *App) applyPipeline(conv int, got *protocol.Pipeline, err error) {
	if conv != a.changes.ciConv {
		return
	}
	a.changes.ciBusy = false
	if err != nil {
		a.changes.ciFailed = err.Error()
		a.invalidate()
		return
	}
	a.changes.ciFailed = ""
	a.changes.pipeline = got
	a.invalidate()
}

// openJob swaps the stage list for one job's log and reads it.
func (a *App) openJob(jobID int) {
	conv := a.changes.conv
	if a.api == nil || conv == 0 || jobID == 0 {
		return
	}
	ctx := a.ctx
	client := a.api
	a.changes.openJob = jobID
	a.changes.ciLog = ""
	a.changes.ciLogCut = false
	a.changes.ciLogBusy = true
	a.changes.ciLogFailed = ""
	a.invalidate()
	go func() {
		log, err := client.JobLog(ctx, conv, jobID, 120)
		a.post(event{kind: evJobLog, convID: conv, jobID: jobID, text: logText(log), logCut: logCut(log), logTail: logBodyOrErr(log, err), err: err})
	}()
}

// logText and friends keep applyJobLog readable: the read returns the log and
// the error beside it, and the drawer shows whichever arrived.
func logText(got *protocol.JobTrace) string {
	if got == nil {
		return ""
	}
	return stripANSI(got.Text)
}

func logCut(got *protocol.JobTrace) bool {
	return got != nil && got.Cut
}

func logBodyOrErr(got *protocol.JobTrace, err error) string {
	if err == nil {
		return ""
	}
	if got != nil && got.Message != "" {
		return got.Message
	}
	return err.Error()
}

// applyJobLog lands a log on the job it was read for.
func (a *App) applyJobLog(jobID int, text string, cut bool, failed string) {
	// A log for a job the drawer has already left is stale.
	if jobID != a.changes.openJob {
		return
	}
	a.changes.ciLogBusy = false
	a.changes.ciLog = text
	a.changes.ciLogCut = cut
	a.changes.ciLogFailed = failed
	a.invalidate()
}

// pipelineWrite retries or cancels, then re-reads so the tab shows the new state.
func (a *App) pipelineWrite(action string, jobID, pipelineID int) {
	conv := a.changes.conv
	if a.api == nil || conv == 0 {
		return
	}
	ctx := a.ctx
	client := a.api
	a.changes.ciWriteBusy = true
	a.invalidate()
	go func() {
		got, err := client.PipelineWrite(ctx, conv, action, jobID, pipelineID)
		a.post(event{kind: evPipelineWrite, convID: conv, text: writeStatus(got, err), err: err})
	}()
}

// writeStatus is what the tab says after a write: the status it got, or the error.
func writeStatus(got *protocol.PipelineWrite, err error) string {
	if got == nil {
		if err != nil {
			return err.Error()
		}
		return ""
	}
	return got.Status
}

// applyPipelineWrite clears the write spinner, says what happened, and re-reads.
func (a *App) applyPipelineWrite(status string) {
	a.changes.ciWriteBusy = false
	a.changes.toast = status
	if status == "ok" || status == "retried" || status == "canceled" {
		a.refreshPipeline()
		if a.changes.openJob != 0 {
			a.openJob(a.changes.openJob)
		}
	}
	a.invalidate()
}

// ciRow is one line of the stage list: a stage heading or a job under it.
type ciRow struct {
	stage string
	job   *protocol.PipelineJob
}

// ciRows flattens the run into the rows the list draws, stages first seen first.
func (a *App) ciRows() []ciRow {
	got := a.changes.pipeline
	if got == nil || got.Pipeline == nil {
		return nil
	}
	rows := make([]ciRow, 0, len(got.Jobs)+len(got.Stages))
	for _, stage := range got.Stages {
		rows = append(rows, ciRow{stage: stage})
		for i := range got.Jobs {
			if got.Jobs[i].Stage == stage {
				rows = append(rows, ciRow{job: &got.Jobs[i]})
			}
		}
	}
	return rows
}

// pipelineBody is the tab: the run, its stages, or one job's log.
func (a *App) pipelineBody(c *ui.Context, t theme.Tokens) {
	if a.changes.openJob != 0 {
		a.jobLogView(c, t)
		return
	}
	switch {
	case a.changes.ciBusy && a.changes.pipeline == nil:
		ui.Row(c).FillWidth().Padding(12, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Size(12, 12).TextColor(t.Ink3)
			ui.Text(c, i18n.T("loading")).FontSize(theme.TextSmall).TextColor(t.Ink3)
		})
		return
	case a.changes.ciFailed != "":
		ui.Text(c, a.changes.ciFailed).FontSize(theme.TextSmall).TextColor(t.Red).Padding(12, 12).MaxLines(4)
		return
	case a.changes.pipeline == nil:
		return
	}
	got := a.changes.pipeline
	if got.Status != "ok" {
		a.pipelineUnavailable(c, t, got)
		return
	}
	if got.Pipeline == nil {
		ui.Text(c, i18n.T("ciNone")).FontSize(theme.TextSmall).TextColor(t.Ink3).Padding(12, 12).MaxLines(3)
		return
	}
	a.pipelineHead(c, t, got)
	rows := a.ciRows()
	ui.Column(c).FillWidth().Grow(1).Children(func() {
		ui.List(c, &a.changes.ciList, len(rows), func(row int) {
			if rows[row].job == nil {
				a.stageRow(c, t, rows[row].stage)
				return
			}
			a.jobRow(c, t, *rows[row].job)
		}).Grow(1).Width(changesWidth - 16)
	})
	a.pipelineFooter(c, t, got)
}

// pipelineUnavailable says which condition stopped the read.
func (a *App) pipelineUnavailable(c *ui.Context, t theme.Tokens, got *protocol.Pipeline) {
	key := "ciFailed"
	switch got.Status {
	case "not-authenticated":
		key = "mrNeedLogin"
	case "no-glab":
		key = "mrNoGlab"
	case "no-remote":
		key = "mrNoRemote"
	case "not-gitlab":
		key = "mrNotGitlab"
	case "detached":
		key = "mrDetached"
	}
	ui.Column(c).FillWidth().Padding(12, 12).Gap(4).Children(func() {
		ui.Text(c, i18n.T(key)).FontSize(theme.TextSmall).TextColor(t.Ink2).MaxLines(3)
		if got.Message != "" {
			ui.Text(c, got.Message).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(3)
		}
		if got.Status == "not-authenticated" {
			ui.Text(c, "glab auth login").Font(monoFont).FontSize(theme.TextSmall).TextColor(t.Ink2)
		}
	})
}

// pipelineHead is the run itself: id, status, what it ran on.
func (a *App) pipelineHead(c *ui.Context, t theme.Tokens, got *protocol.Pipeline) {
	run := got.Pipeline
	ui.Column(c).FillWidth().Padding(10, 12, 8, 12).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Width(6).Height(6).Radius(3).Background(jobStatusTone(t, run.Status))
			ui.Text(c, "#"+itoa(run.ID)).Font(monoFont).FontSize(theme.TextSmall).TextColor(t.Ink3)
			ui.Text(c, a.ciStatusText(run.Status)).FontSize(theme.TextRow).FontWeight(600).TextColor(t.Ink)
			ui.Spacer(c)
			if run.WebURL != "" {
				a.mergeAction(c, t, i18n.T("ciOpenPage"), func() { openExternal(run.WebURL) })
			}
		})
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			ui.Text(c, shortSHA(run.SHA)).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Ink3)
			if run.Source != "" {
				ui.Text(c, "· "+run.Source).FontSize(theme.TextMicro).TextColor(t.Ink3)
			}
		})
	})
	ui.Box(c).FillWidth().Height(1).Background(t.LineSoft)
}

// ciStatusText is the status in words, reused for the run and for a job.
func (a *App) ciStatusText(status string) string {
	switch status {
	case "success", "passed":
		return i18n.T("ciPassed")
	case "failed":
		return i18n.T("ciFailedShort")
	case "running":
		return i18n.T("ciRunning")
	case "pending":
		return i18n.T("ciPending")
	case "canceled", "cancelled":
		return i18n.T("ciCanceled")
	case "manual":
		return i18n.T("ciManual")
	case "skipped":
		return i18n.T("ciSkipped")
	}
	return status
}

// stageRow is one stage's heading in the list.
func (a *App) stageRow(c *ui.Context, t theme.Tokens, stage string) {
	ui.Row(c).FillWidth().Height(26).Padding(6, 4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, stage).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Ink3)
	})
}

// jobRow is one job: its dot, name and either how long it took or what it is doing.
func (a *App) jobRow(c *ui.Context, t theme.Tokens, job protocol.PipelineJob) {
	row := ui.ButtonBase(c).FillWidth().Height(28).Radius(theme.RadiusChip).Padding(0, 14).Gap(6).
		AlignItems(ui.Center).Justify(ui.Start)
	if row.Hovered() {
		row = row.Background(t.Hover)
	}
	row.OnClick(func() { a.openJob(job.ID) })
	row.Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Background(jobStatusTone(t, job.Status))
		// One string holds the name and its note: a text beside a growing
		// sibling inside a list row is not painted.
		note := a.ciStatusText(job.Status)
		if job.Duration != nil {
			note = durationText(*job.Duration)
		} else if job.AllowFailure && job.Status == "failed" {
			note = i18n.T("ciAllowed")
		}
		ui.Text(c, job.Name+"  "+note).FontSize(theme.TextRow).
			TextColor(branchTextColor(t, job.Status)).MaxLines(1)
	})
}

// branchTextColor keeps a failed job's name readable: the dot already carries red.
func branchTextColor(t theme.Tokens, status string) ui.Color {
	if status == "success" || status == "failed" {
		return t.Ink
	}
	return t.Ink2
}

// durationText writes seconds the way a person reads them.
func durationText(seconds float32) string {
	if seconds < 60 {
		return itoa(int(seconds)) + "s"
	}
	minutes := int(seconds) / 60
	return itoa(minutes) + "m" + itoa(int(seconds)%60) + "s"
}

// shortSHA is the first seven characters, which is what fits and what people say.
func shortSHA(sha string) string {
	if len(sha) <= 7 {
		return sha
	}
	return sha[:7]
}

// pipelineFooter offers the whole-run actions, and says what the last write did.
func (a *App) pipelineFooter(c *ui.Context, t theme.Tokens, got *protocol.Pipeline) {
	run := got.Pipeline
	if run == nil {
		return
	}
	ui.Row(c).FillWidth().Padding(6, 12, 10, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		if a.changes.ciWriteBusy {
			ui.Spinner(c).Size(12, 12).TextColor(t.Ink3)
		}
		switch run.Status {
		case "failed", "canceled", "cancelled":
			a.mergeSecondaryAction(c, t, i18n.T("ciRetry"), func() { a.pipelineWrite("retry", 0, run.ID) })
		case "running", "pending":
			a.mergeSecondaryAction(c, t, i18n.T("ciCancel"), func() { a.pipelineWrite("cancel", 0, run.ID) })
		}
		ui.Spacer(c)
		if a.changes.toast != "" {
			ui.Text(c, a.changes.toast).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
		}
	})
}

// jobLogView is one job's log, the way the tail command would show it.
func (a *App) jobLogView(c *ui.Context, t theme.Tokens) {
	job := a.ciJob(a.changes.openJob)
	ui.Column(c).FillWidth().Grow(1).Children(func() {
		ui.Row(c).FillWidth().Height(34).Padding(0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
			if job != nil {
				ui.Box(c).Width(6).Height(6).Radius(3).Background(jobStatusTone(t, job.Status))
				ui.Text(c, job.Name).FontSize(theme.TextRow).FontWeight(600).TextColor(t.Ink).MaxLines(1)
			}
			ui.Spacer(c)
			if job != nil && (job.Status == "failed" || job.Status == "canceled" || job.Status == "cancelled") {
				a.mergeSecondaryAction(c, t, i18n.T("ciRetry"), func() { a.pipelineWrite("retry", job.ID, 0) })
			}
			a.mergeSecondaryAction(c, t, i18n.T("ciJobs"), func() {
				a.changes.openJob = 0
				a.changes.ciLog = ""
				a.invalidate()
			})
		})
		ui.Box(c).FillWidth().Height(1).Background(t.LineSoft)
		switch {
		case a.changes.ciLogBusy && a.changes.ciLog == "":
			ui.Row(c).FillWidth().Padding(12, 12).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c).Size(12, 12).TextColor(t.Ink3)
				ui.Text(c, i18n.T("ciLogLoading")).FontSize(theme.TextSmall).TextColor(t.Ink3)
			})
		case a.changes.ciLogFailed != "":
			ui.Text(c, a.changes.ciLogFailed).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Red).Padding(12, 12).MaxLines(4)
		case a.changes.ciLog == "":
			ui.Text(c, i18n.T("ciLogEmpty")).FontSize(theme.TextSmall).TextColor(t.Ink3).Padding(12, 12)
		default:
			lines := strings.Split(a.changes.ciLog, "\n")
			ui.Column(c).FillWidth().Grow(1).Children(func() {
				ui.List(c, &a.changes.logList, len(lines), func(row int) {
					// The whole line is one string: a text beside a sibling in a
					// list row is not painted.
					ui.Text(c, lines[row]).Font(monoFont).FontSize(theme.TextMicro).
						TextColor(logLineColor(t, lines[row])).MaxLines(1).Padding(0, 12)
				}).Grow(1).Width(changesWidth - 16)
			})
			if a.changes.ciLogCut {
				ui.Text(c, i18n.T("ciLogTail")).FontSize(theme.TextMicro).TextColor(t.Ink3).Padding(4, 12)
			}
		}
	})
}

// logLineColor picks out the line that says a job failed, which is the one
// anybody opens a log to find.
func logLineColor(t theme.Tokens, line string) ui.Color {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed"):
		return t.Red
	case strings.Contains(lower, "warning"):
		return t.Orange
	}
	return t.Ink2
}

// ciJob finds one job of the run that is on screen.
func (a *App) ciJob(jobID int) *protocol.PipelineJob {
	got := a.changes.pipeline
	if got == nil {
		return nil
	}
	for i := range got.Jobs {
		if got.Jobs[i].ID == jobID {
			return &got.Jobs[i]
		}
	}
	return nil
}
