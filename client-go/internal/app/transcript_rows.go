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
	live  bool
	first bool
	key   any
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
		for _, group := range stepGroups(turn) {
			push(chatRow{kind: chatSteps, steps: group, live: live,
				key: "s" + itoa(group[0].Entry.ID)})
		}
		for _, final := range turn.Finals {
			push(chatRow{kind: chatFinal, entry: final, key: "f" + itoa(final.ID)})
		}
		if live {
			push(chatRow{kind: chatLive, key: "l" + itoa(turn.ID)})
		}
		push(chatRow{kind: chatPhase, key: "p" + itoa(turn.ID)})
		push(chatRow{kind: chatSummary, key: "m" + itoa(turn.ID)})
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
	if row.kind == chatSteps {
		// The rail fills the row, padding included, so consecutive step rows
		// draw one line rather than a dashed one.
		ui.Row(c).Gap(7).Padding(16, 0).Children(func() {
			ui.Column(c).Width(2).Radius(1).Background(t.Border).FillHeight()
			ui.Column(c).Gap(4).Grow(1).Padding(0, padTop, 4, 0).Children(func() {
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
