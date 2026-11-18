package app

import (
	"github.com/egoist/mygo/ui"
	"pitta/internal/i18n"
	"pitta/internal/motion"
	"pitta/internal/protocol"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// chatKind is what one row of the chat shows. A row is the unit the list
// virtualizes, so it is a step rather than a turn: a turn on a real chat holds
// forty tool calls and stands two thousand points tall, and a list that had to
// build one whole turn to show a line of it would cost what the window did.
type chatKind int

const (
	chatEmpty chatKind = iota
	chatMore
	chatNote
	chatUser
	chatReport
	chatSteps
	chatFold
	chatFinal
	chatLive
	chatPhase
	chatSummary
)

// chatRow is one row of the chat, with the identity the list keeps its place
// by: a chat that loads older turns above the view holds the line the reader
// is on because the rows under it keep their keys.
type chatRow struct {
	kind  chatKind
	turn  *turns.Turn
	entry protocol.Entry
	steps []turns.Step
	// folded is the steps a chatFold line stands in for. It is here rather than
	// derived from the turn so a search hit knows whether its step is under the
	// line or beside it.
	folded []turns.Step
	live   bool
	first  bool
	key    any
}

// buildChatRows lays the chat out as rows: the offer to read more, then each
// turn's parts in the order they are read.
func (a *App) buildChatRows(built []turns.Turn) []chatRow {
	rows := make([]chatRow, 0, len(built)*4)
	if len(built) == 0 {
		return append(rows, chatRow{kind: chatEmpty, key: "empty"})
	}
	if a.historyMore {
		rows = append(rows, chatRow{kind: chatMore, key: "more"})
	}
	for i := range built {
		turn := &built[i]
		live := i == len(built)-1 && a.live != nil && turn.Status(a.live) == turns.StatusRunning
		first := true
		push := func(row chatRow) {
			row.turn = turn
			row.first = first
			first = false
			rows = append(rows, row)
		}
		for _, note := range turn.Notes {
			push(chatRow{kind: chatNote, entry: note, key: "n" + itoa(note.ID)})
		}
		if turn.User != nil {
			push(chatRow{kind: chatUser, entry: *turn.User, key: "u" + itoa(turn.User.ID)})
		}
		for _, report := range turn.Reports {
			push(chatRow{kind: chatReport, entry: report, key: "r" + itoa(report.ID)})
		}
		// A turn of twelve tool calls is a wall to scroll past when what the
		// reader came for is the answer; past stepsFoldAt steps the finished ones
		// fold into one line that keeps the accounting. The step still running and
		// the ones that failed stay as rows: they are the two reasons to look.
		long := len(turn.Steps) > stepsFoldAt
		open := a.stepsOpen[turn.ID]
		runningID := -1
		if n := runningStep(turn, live); n >= 0 {
			runningID = turn.Steps[n].Entry.ID
		}
		// The steps in reading order, with the runs that fold held together. A
		// failed step and the one still running break a run: those are read where
		// they happened, not from a line about them.
		type foldRun struct {
			steps []turns.Step
			fold  bool
		}
		runs := make([]foldRun, 0, len(turn.Steps))
		for _, group := range stepGroups(turn) {
			// Short turns keep the runs the transcript already had: three calls
			// are one line here, and that line is not the turn's.
			if !long {
				runs = append(runs, foldRun{steps: group})
				continue
			}
			for _, step := range group {
				if step.Failed() || step.Entry.ID == runningID {
					runs = append(runs, foldRun{steps: []turns.Step{step}})
					continue
				}
				if n := len(runs); n > 0 && runs[n-1].fold {
					runs[n-1].steps = append(runs[n-1].steps, step)
					continue
				}
				runs = append(runs, foldRun{steps: []turns.Step{step}, fold: true})
			}
		}
		foldAt, folded := -1, make([]turns.Step, 0, len(turn.Steps))
		for _, run := range runs {
			if !run.fold {
				push(chatRow{kind: chatSteps, steps: run.steps, live: live,
					key: "s" + itoa(run.steps[0].Entry.ID)})
				continue
			}
			if foldAt < 0 {
				foldAt = len(rows)
				push(chatRow{kind: chatFold, key: "d" + itoa(turn.ID)})
			}
			folded = append(folded, run.steps...)
			if !open {
				continue
			}
			// Opened, a folded run is every step of it, one row each: the reader
			// asked for the steps, not for a shorter way to fold them again.
			for _, step := range run.steps {
				push(chatRow{kind: chatSteps, steps: []turns.Step{step}, live: live,
					key: "s" + itoa(step.Entry.ID)})
			}
		}
		if foldAt >= 0 {
			rows[foldAt].folded = folded
		}
		for _, final := range turn.Finals {
			push(chatRow{kind: chatFinal, entry: final, key: "f" + itoa(final.ID)})
		}
		if live {
			push(chatRow{kind: chatLive, key: "l" + itoa(turn.ID)})
		}
		push(chatRow{kind: chatPhase, key: "p" + itoa(turn.ID)})
		// A folded turn's line already carries files, diff and time, so the
		// summary under it would say all of it twice. An opened one gets its
		// summary back, file list included.
		if foldAt < 0 || open {
			push(chatRow{kind: chatSummary, key: "m" + itoa(turn.ID)})
		}
	}
	return rows
}

// chatRow draws one row. The spacing inside a turn is padding on the row, not
// a gap between rows, so the rail the steps hang off runs on unbroken from the
// first step of a turn to its last.
func (a *App) chatRow(c *ui.Context, t theme.Tokens, row chatRow) {
	switch row.kind {
	case chatEmpty:
		a.emptyTranscript(c, t)
		return
	case chatMore:
		ui.Column(c).Padding(16, 4).Children(func() {
			ui.Button(c, i18n.T("olderMessages")).OnClick(func() { a.loadOlder() })
		})
		return
	}
	padTop := float32(4)
	if row.first {
		padTop = 8
	}
	if row.kind == chatSteps || row.kind == chatFold {
		// The rail fills the row, padding included, so consecutive step rows
		// draw one line rather than a dashed one.
		ui.Row(c).Gap(7).Padding(16, 0).Children(func() {
			ui.Column(c).Width(2).Radius(1).Background(t.Border).FillHeight()
			ui.Column(c).Gap(4).Grow(1).Padding(0, padTop, 4, 0).Children(func() {
				if row.kind == chatFold {
					a.stepsFoldLine(c, t, row.turn)
					return
				}
				if len(row.steps) == 1 {
					a.stepRow(c, t, row.steps[0], row.live)
					return
				}
				a.toolRun(c, t, row.turn, row.steps, row.live)
			})
		})
		return
	}
	ui.Column(c).Padding(16, padTop, 4, 16).Children(func() {
		switch row.kind {
		case chatNote:
			a.noteLine(c, t, noteText(row.entry))
		case chatUser:
			a.userBubble(c, t, turns.Text(row.entry), a.historyImages(row.entry), a.isHit(row.entry))
		case chatReport:
			a.reportBlock(c, t, turns.Text(row.entry))
		case chatFinal:
			retry := ""
			if row.turn != nil && row.turn.User != nil {
				retry = turns.Text(*row.turn.User)
			}
			a.answerBlock(c, t, turns.Text(row.entry), retry, a.isHit(row.entry))
		case chatLive:
			a.liveLine(c, t, row.turn)
		case chatPhase:
			a.phaseBar(c, t, row.turn)
		case chatSummary:
			a.summaryLine(c, t, row.turn)
		}
	})
}

// emptyTranscript says why the pane is blank: no chat was picked, or the agent
// is not answering.
func (a *App) emptyTranscript(c *ui.Context, t theme.Tokens) {
	title, hint := i18n.T("noConversation"), i18n.T("noSessionsHint")
	if a.conn == ConnDown {
		title, hint = i18n.T("bridgeDownTitle"), i18n.T("bridgeRetrying")
	}
	// The empty state comes in the way the earlier client's does (#072): a 350 ms
	// fade while it rises 8 px.
	ui.Column(c).Padding(16, 4).Children(func() {
		ui.Column(c).Padding(16, 0).Gap(3).Transition(motion.FadeIn()).Children(func() {
			ui.Text(c, title).FontSize(12).TextColor(t.MutedFg).Wrap()
			if hint != "" {
				ui.Text(c, hint).FontSize(11).TextColor(t.MutedFg).Wrap()
			}
		})
	})
}
