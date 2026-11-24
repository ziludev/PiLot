package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// The stage strip is the one place the window says where the work stands: develop
// this branch, hand it to test, ask for release, ship. The bridge derives every
// step from git and GitLab, so a step can never claim a state the repository does
// not have — the client only draws what came back, and says what stops the next one.

const stageTimeout = 15 * time.Second

// loadStages reads the pipeline for a conversation, off the frame thread.
func (a *App) loadStages(convID int) {
	if a.api == nil || convID == 0 {
		a.stageStrip = nil
		return
	}
	client := a.api
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, stageTimeout)
		defer cancel()
		got, err := client.Stages(ctx, convID)
		a.post(event{kind: evStages, convID: convID, stageReport: got, err: err})
	}()
}

// applyStages keeps the answer only if it is about the chat on screen.
func (a *App) applyStages(convID int, got *protocol.Stages, err error) {
	if convID != a.active {
		return
	}
	if err != nil || got == nil {
		// No strip is better than a stale one: a wrong "blocked" would stop work.
		a.stageStrip = nil
		a.invalidate()
		return
	}
	a.stageStrip = got
	a.invalidate()
}

// stageStateTone is the colour a step's dot takes, and how loud its label is.
func stageStateTone(t theme.Tokens, state string) (ui.Color, ui.Color, int) {
	switch state {
	case "done":
		return t.Green, t.Ink2, 500
	case "active":
		return t.Brand, t.Ink, 600
	case "blocked":
		return t.Red, t.Ink, 600
	default:
		return t.Ink3, t.Ink3, 500
	}
}

// stageStrip draws the steps in one row, plus the sentence that explains the step
// that is blocking or moving.
func (a *App) stageBar(c *ui.Context, t theme.Tokens) {
	if a.stageStrip == nil || len(a.stageStrip.Stages) == 0 {
		return
	}
	// Two rows, not one: the steps are the strip, and the sentence that says what
	// to do about them is a second thought. On one row a narrow pane squeezed the
	// labels into one character per line — 提测到 release/test became a column of
	// hanzi — because a row shrinks whatever does not fit.
	// The strip's height is spelled out: a Column that only knows its children
	// laid out at zero height and let the transcript draw over it.
	note := stageNote(a.stageStrip)
	stage, askable := stageAttention(a.stageStrip)
	askable = askable && stage.State == "active"
	second := note != "" || a.stageStrip.Rounds > 0 || askable
	height := float32(30)
	if second {
		height += 22
	}
	ui.Column(c).FillWidth().Height(height+1).Background(t.Surface).Children(func() {
		ui.Row(c).FillWidth().Height(30).Padding(0, 14).Gap(10).AlignItems(ui.Center).
			Children(func() {
				for index, stage := range a.stageStrip.Stages {
					if index > 0 {
						ui.Text(c, "›").FontSize(theme.TextMicro).TextColor(t.Ink3)
					}
					dot, label, weight := stageStateTone(t, stage.State)
					if stage.State == "active" {
						ui.Spinner(c).Size(9, 9).TextColor(dot)
					} else {
						ui.Box(c).Width(6).Height(6).Radius(3).Background(dot)
					}
					ui.Text(c, stage.Label).FontSize(theme.TextRow).FontWeight(weight).
						TextColor(label).MaxLines(1)
					if stage.Detail != "" {
						ui.Text(c, stage.Detail).FontSize(theme.TextMicro).TextColor(t.Ink3).
							MaxLines(1)
					}
				}
			})
		if !second {
			ui.Box(c).FillWidth().Height(1).Background(t.Line)
			return
		}
		ui.Row(c).FillWidth().Height(22).Padding(0, 14, 4, 14).Gap(8).
			AlignItems(ui.Center).Children(func() {
				if note != "" {
					ui.Text(c, note).FontSize(theme.TextMicro).TextColor(t.Ink3).
						MaxLines(1).Grow(1)
				} else {
					ui.Spacer(c)
				}
				if a.stageStrip.Rounds > 0 {
					label := i18n.TF("stageRounds", map[string]any{"n": a.stageStrip.Rounds, "max": a.stageStrip.RoundLimit})
					colour := t.Ink3
					if a.stageStrip.Rounds >= a.stageStrip.RoundLimit {
						colour = t.Red
					}
					ui.Text(c, label).FontSize(theme.TextMicro).FontWeight(600).
						TextColor(colour).MaxLines(1)
				}
				if askable {
					ask := ui.ButtonBase(c).Height(20).Padding(0, 8).Radius(theme.RadiusChip).
						Justify(ui.Center).AlignItems(ui.Center).
						Cursor(ui.CursorPointer).Tooltip(i18n.T("stageAskTip"))
					if ask.Hovered() {
						ask = ask.Background(t.Field)
					}
					ask.Children(func() {
						ui.Text(c, i18n.T("stageAsk")).FontSize(theme.TextMicro).FontWeight(500).
							TextColor(t.Brand).MaxLines(1)
					})
					ask.OnClick(a.stageAsk)
				}
			})
		ui.Box(c).FillWidth().Height(1).Background(t.Line)
	})
}

// stageAttention is the step the strip is about: the one blocked, or else the
// one running. A done strip has nothing to ask for.
func stageAttention(got *protocol.Stages) (protocol.Stage, bool) {
	for _, stage := range got.Stages {
		if stage.State == "blocked" || stage.State == "active" {
			return stage, true
		}
	}
	return protocol.Stage{}, false
}

// stageAsk writes the step into the composer. It stops there on purpose: the
// sentence is a proposal a person reads before it is sent, not a command queued
// behind their back — and a merge is exactly the kind of thing to read twice.
func (a *App) stageAsk() {
	got := a.stageStrip
	if got == nil {
		return
	}
	stage, ok := stageAttention(got)
	if !ok {
		return
	}
	vars := map[string]any{"branch": got.Branch, "base": got.Base, "target": got.Target}
	switch stage.Key {
	case "dev":
		a.draft = i18n.TF("stageAskDev", vars)
	case "test":
		a.draft = i18n.TF("stageAskTest", vars)
	case "release":
		a.draft = i18n.TF("stageAskRelease", vars)
	case "ship":
		a.draft = i18n.TF("stageAskShip", vars)
	default:
		a.draft = stage.NextAction
	}
	a.focusComposer = true
	a.invalidate()
}

// stageNote is the sentence for the step that needs attention: the blocked one
// first, because that is why nothing moves.
func stageNote(got *protocol.Stages) string {
	// For a blocked step the sentence is what a person can do about it; the cap
	// itself is already the red count on the right, and saying it twice reads as
	// an alarm rather than an instruction.
	for _, stage := range got.Stages {
		if stage.State == "blocked" && stage.NextAction != "" {
			return stage.NextAction
		}
		if stage.State == "blocked" && stage.BlockedBy != "" {
			return stage.BlockedBy
		}
	}
	for _, stage := range got.Stages {
		if stage.State == "active" && stage.NextAction != "" {
			return stage.NextAction
		}
	}
	return ""
}
