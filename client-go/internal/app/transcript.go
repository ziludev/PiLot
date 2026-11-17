package app

import (
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"pitta/internal/brand"
	"pitta/internal/diff"
	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/motion"
	"pitta/internal/protocol"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// transcript is the middle pane: one block per turn, oldest first, with the
// live run at the bottom.
func (a *App) transcript(c *ui.Context) {
	t := theme.Current(c)
	built := a.groupedTurns()
	a.turnTotal = len(built)
	// The window keeps the first frame of a 500-entry chat cheap; the button
	// opens the rest, as the transcript's TURN_WINDOW does.
	window := a.turnWindow
	if window <= 0 || window > len(built) {
		window = len(built)
	}
	start := len(built) - window
	// A find hit moves the window instead of the scroll offset: turn heights
	// vary, so the only reliable way to land on a turn is to draw it.
	anchor := a.focusedTurn()
	if anchor < 0 && a.focusEntry > 0 {
		anchor = turnOfEntry(built, a.focusEntry)
	}
	if index := anchor; index >= 0 {
		if start = index - window/2; start < 0 {
			start = 0
		}
		if start+window > len(built) {
			start = len(built) - window
		}
	}
	hidden := start
	end := start + window
	if end > len(built) {
		end = len(built)
	}
	scroll := ui.Scroll(c).Fill().TrackScroll(&a.transcriptScroll)
	a.followScroll()
	if bounds := scroll.Bounds(); bounds.H > 0 {
		a.scrollViewH = bounds.H
	}
	scroll.Children(func() {
		ui.Column(c).Fill().Padding(16, 20).Gap(8).Children(func() {
			if len(built) == 0 {
				// A chat nobody picked is not the same as a quiet one: the earlier client
				// says so instead of drawing an empty pane (the transcript view:340).
				title, hint := i18n.T("noConversation"), i18n.T("noSessionsHint")
				if a.conn == ConnDown {
					title, hint = i18n.T("bridgeDownTitle"), i18n.T("bridgeRetrying")
				}
				// The empty state comes in the way the earlier client's does (#072): a 350 ms
				// fade while it rises 8 px.
				ui.Column(c).Padding(16, 0).Gap(3).Transition(motion.FadeIn()).Children(func() {
					ui.Text(c, title).FontSize(12).TextColor(t.MutedFg).Wrap()
					if hint != "" {
						ui.Text(c, hint).FontSize(11).TextColor(t.MutedFg).Wrap()
					}
				})
				return
			}
			if hidden > 0 {
				ui.Button(c, i18n.T("earlierTurns")).
					OnClick(func() {
						a.apply(func() {
							a.turnWindow += 60
							a.invalidate()
						})
					})
			}
			for i := start; i < end; i++ {
				turn := &built[i]
				live := i == len(built)-1 && a.live != nil && turn.Status(a.live) == turns.StatusRunning
				a.turnBlock(c, t, turn, live, i == anchor)
			}
		})
	})
}

// groupedTurns groups the entries once per change. Grouping parses every
// entry's model, which cost more than a frame's budget on a long chat, and a
// frame that only scrolls or hovers changes none of them.
func (a *App) groupedTurns() []turns.Turn {
	if a.turnStale || a.turnCache == nil {
		a.turnCache = turns.GroupTurns(a.entries, a.waits)
		a.turnStale = false
	}
	return a.turnCache
}

// turnBlock draws one turn: the user's message on the right, the agent's work
// on the left, and a line of accounting under it.
func (a *App) turnBlock(c *ui.Context, t theme.Tokens, turn *turns.Turn, live, focused bool) {
	block := ui.Column(c).Fill().Gap(6)
	if focused {
		// The find bar's current hit gets a frame, so stepping shows where it
		// landed without scrolling the list.
		block = ui.Column(c).Fill().Gap(6).Padding(6, 6).Radius(8).
			Border(1, t.Primary).Transition(motion.Hover())
	}
	block.Children(func() {
		for _, note := range turn.Notes {
			a.noteLine(c, t, noteText(note))
		}
		if turn.User != nil {
			a.userBubble(c, t, turns.Text(*turn.User), a.historyImages(*turn.User), a.isHit(*turn.User))
		}
		for _, report := range turn.Reports {
			a.reportBlock(c, t, turns.Text(report))
		}
		if len(turn.Steps) > 0 {
			// The steps hang off a rounded 2px rail, as the transcript's
			// TurnRail does: a hairline border reads as a table rule instead of
			// a timeline.
			ui.Row(c).Gap(7).Children(func() {
				ui.Column(c).Width(2).Radius(1).Background(t.Border).FillHeight()
				ui.Column(c).Gap(4).Grow(1).Children(func() {
					a.stepList(c, t, turn)
				})
			})
		}
		for _, final := range turn.Finals {
			retry := ""
			if turn.User != nil {
				retry = turns.Text(*turn.User)
			}
			a.answerBlock(c, t, turns.Text(final), retry, a.isHit(final))
		}
		if live {
			a.liveLine(c, t, turn)
		}
		a.phaseBar(c, t, turn)
		ui.Text(c, turnSummary(turn)).FontSize(9).TextColor(t.MutedFg)
	})
}

// phaseBar is TurnDurations at transcript scale: one thin bar where the widths
// follow how long the turn spent thinking, waiting on tools and waiting for an
// approval, so a slow turn is visible without opening anything.
func (a *App) phaseBar(c *ui.Context, t theme.Tokens, turn *turns.Turn) {
	phases := turn.Stats.Phases
	total := phases.Think + phases.Tool + phases.Wait + phases.Error
	if total <= 0 || turn.Stats.MS == nil {
		return
	}
	width := float32(180)
	segment := func(ms int64, color ui.Color) {
		if ms <= 0 {
			return
		}
		ui.Row(c).Width(width * float32(ms) / float32(total)).Height(3).Background(color)
	}
	ui.Row(c).Height(3).Radius(2).Background(t.Muted).Children(func() {
		segment(phases.Think, t.MutedFg)
		segment(phases.Tool, t.Primary)
		segment(phases.Wait, t.Warning)
		segment(phases.Error, t.Destructive)
	})
	ui.Text(c, phaseLegend(turn)).FontSize(9).TextColor(t.MutedFg).MaxLines(1)
}

// phaseLegend names what the bar's colours mean, as the phase bar does.
func phaseLegend(turn *turns.Turn) string {
	phases := turn.Stats.Phases
	parts := make([]string, 0, 3)
	if phases.Think > 0 {
		parts = append(parts, "think "+turns.Clock(phases.Think))
	}
	if phases.Tool > 0 {
		parts = append(parts, "tool "+turns.Clock(phases.Tool))
	}
	if phases.Wait > 0 {
		parts = append(parts, "wait "+turns.Clock(phases.Wait))
	}
	if phases.Error > 0 {
		parts = append(parts, "error "+turns.Clock(phases.Error))
	}
	return strings.Join(parts, " · ")
}

// userBubble is the message the person typed, right aligned as in the earlier client
// transcript.
func (a *App) userBubble(c *ui.Context, t theme.Tokens, text string, images []imagePart, hit bool) {
	if strings.TrimSpace(text) == "" && len(images) == 0 {
		return
	}
	// The actions live under the pointer, as the earlier client's view hides them
	// until the message is hovered, so a quiet transcript stays quiet.
	row := ui.Row(c).Fill().Gap(2).AlignItems(ui.Center)
	hovered := row.Hovered()
	row.Children(func() {
		ui.Row(c).Grow(1).Children(func() {})
		if hovered {
			a.messageActions(c, t, text, text)
		}
		bubble := ui.Column(c).MaxWidth(answerWidth).Padding(8, 12).Gap(6).Radius(12).Background(t.Muted)
		if hit {
			// The line a cross-chat search matched wears a ring, so the jump
			// lands on the sentence and not just on the turn.
			bubble.Border(1, t.Primary)
		}
		bubble.ContextMenu(func(m *ui.Menu) {
			if m.Item(i18n.T("copy")).Shortcut(ui.Cmd, ui.KeyC).Chosen() {
				a.copyText(c, text)
			}
			if m.Item(i18n.T("edit")).Chosen() {
				a.apply(func() { a.draft = text; a.invalidate() })
			}
			if m.Item(i18n.T("retry")).Chosen() {
				a.sendText(text)
			}
		})
		bubble.Children(func() {
			if len(images) > 0 {
				a.imagePreviews(c, t, images)
			}
			if strings.TrimSpace(text) != "" {
				ui.Text(c, text).FontSize(13).TextColor(t.Foreground)
			}
		})
	})
}

// answerWidth caps a reply's card, streaming or settled: the same text must not
// jump to another width when the turn completes.
const answerWidth = 760

// answerLabel names a settled reply's card, and streamLabel the card a reply
// streams into, so the tests can compare the two.
const (
	answerLabel = "answer-block"
	streamLabel = "answer-stream"
)

// answerBlock renders the agent's reply as Markdown, which is what it writes.
func (a *App) answerBlock(c *ui.Context, t theme.Tokens, text string, retryText string, hit bool) {
	if strings.TrimSpace(text) == "" {
		return
	}
	row := ui.Row(c).Fill().Gap(2).AlignItems(ui.Center)
	hovered := row.Hovered()
	row.Children(func() {
		block := ui.Column(c).MaxWidth(answerWidth).Padding(8, 12).Radius(12).Border(1, t.Border).Background(t.Card).
			Label(answerLabel)
		if hit {
			block.BorderColor(t.Primary)
		}
		block.ContextMenu(func(m *ui.Menu) {
			if m.Item(i18n.T("copy")).Shortcut(ui.Cmd, ui.KeyC).Chosen() {
				a.copyText(c, text)
			}
			if retryText != "" && m.Item(i18n.T("retry")).Chosen() {
				a.sendText(retryText)
			}
		})
		// The answer can be dragged into any other app as Markdown, which is
		// what a native client can offer and a page cannot.
		block.DragData(transfer.New(transfer.NewItem(
			transfer.Bytes(transfer.Markdown, []byte(text)),
			transfer.Bytes(transfer.Text, []byte(text)),
		)))
		block.Children(func() {
			markdown.View(c, text, &a.md)
		})
		if hovered {
			a.messageActions(c, t, text, retryText)
		}
		ui.Row(c).Grow(1).Children(func() {})
	})
}

// messageActions is the copy / edit / retry trio of the transcript view:20-46. the earlier client
// shows it on hover; mygo has no group hover, so it stays visible in muted
// grey, which costs a few pixels and hides nothing.
func (a *App) messageActions(c *ui.Context, t theme.Tokens, text string, retryText string) {
	ui.Row(c).Gap(1).Padding(0, 2).Children(func() {
		a.actionIcon(c, t, icons.Copy, i18n.T("copy"), func() {
			a.copyText(c, text)
		})
		a.actionIcon(c, t, icons.Pencil, i18n.T("edit"), func() {
			a.apply(func() { a.draft = retryText; a.invalidate() })
		})
		if retryText != "" {
			a.actionIcon(c, t, icons.RotateCcw, i18n.T("retry"), func() { a.sendText(retryText) })
		}
	})
}

// actionIcon is a borderless icon button, the size MessageActions uses.
func (a *App) actionIcon(c *ui.Context, t theme.Tokens, icon *ui.SVG, label string, onClick func()) {
	button := ui.ButtonBase(c).Size(20, 20).Radius(4).Cursor(ui.CursorPointer).
		Transition(motion.Hover())
	button.Label(label)
	button.Tooltip(label)
	if button.Hovered() {
		button.Background(t.Accent)
	}
	button.Children(func() { ui.Icon(c, icon).Size(11, 11).TextColor(t.MutedFg) })
	button.OnClick(onClick)
}

// reportBlock is an app.feature-report entry: a titled card, as the earlier client
// client draws it with a puzzle emoji header.
func (a *App) reportBlock(c *ui.Context, t theme.Tokens, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	ui.Column(c).Fill().Padding(8, 11).Radius(8).Background(t.Muted).Children(func() {
		ui.Text(c, "Feature report").FontSize(10).TextColor(t.MutedFg)
		ui.Text(c, text).FontSize(12).TextColor(t.Foreground)
	})
}

// noteLine is a bookkeeping line between turns, as TurnRows show notes.
func (a *App) noteLine(c *ui.Context, t theme.Tokens, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	ui.Row(c).Fill().Gap(8).Children(func() {
		ui.Divider(c)
		ui.Text(c, text).FontSize(10).TextColor(t.MutedFg)
		ui.Divider(c)
	})
}

// stepRow is one tool call or answer on the turn's timeline: a line that opens
// into what the tool returned, a patch when it changed files.
func (a *App) stepRow(c *ui.Context, t theme.Tokens, step turns.Step) {
	if name, _ := turns.ToolCall(step.Entry); name == turns.ReportDoneTool {
		if met, ok, why := turns.Verdict(step.Entry); ok {
			a.verdictBand(c, t, met, why)
			return
		}
	}
	open := a.stepOpen[step.Entry.ID]
	label := step.Label
	if label == "" {
		label = "tool"
	}
	ui.Column(c).Gap(4).Children(func() {
		// One line per step: a dot, the chevron that says it opens, the label.
		// All three centre on the same baseline as the transcript's StepRow.
		ui.Row(c).Gap(6).AlignItems(ui.Center).PaddingY(1).Children(func() {
			ui.Row(c).Size(5, 5).Radius(3).Background(stepColor(step, t))
			chevron := icons.ChevronRight
			if open {
				chevron = icons.ChevronDown
			}
			ui.Icon(c, chevron).Size(10, 10).TextColor(t.MutedFg)
			ui.Text(c, label).FontSize(10.5).TextColor(t.MutedFg)
		}).OnClick(func() {
			a.apply(func() {
				a.stepOpen[step.Entry.ID] = !open
				a.invalidate()
			})
		})
		if !open || step.Result == nil {
			return
		}
		text, failed := turns.ToolResult(*step.Result)
		if strings.TrimSpace(text) == "" {
			return
		}
		// TurnRail's accordion: the output grows into place over 350 ms
		// instead of appearing at its full height.
		ui.Column(c).Key(step.Entry.ID).Transition(motion.AccordionTransition()).
			Children(func() { a.stepOutput(c, t, text, failed) })
	})
}

// stepList draws a turn's steps, folding each run of neighbouring tool calls into
// one line. A turn that runs a dozen commands used to put a dozen lines between
// the question and the answer.
func (a *App) stepList(c *ui.Context, t theme.Tokens, turn *turns.Turn) {
	for i := 0; i < len(turn.Steps); {
		if !foldsIntoRun(turn.Steps[i]) {
			a.stepRow(c, t, turn.Steps[i])
			i++
			continue
		}
		j := i + 1
		for j < len(turn.Steps) && foldsIntoRun(turn.Steps[j]) {
			j++
		}
		if j-i == 1 {
			// A lone call says more as itself than as "1 次工具调用".
			a.stepRow(c, t, turn.Steps[i])
		} else {
			a.toolRun(c, t, turn, turn.Steps[i:j])
		}
		i = j
	}
}

// foldsIntoRun says whether a step joins the run of tool calls a turn folds. The
// verdict does not: it is the turn's answer, and it is read, not unfolded.
func foldsIntoRun(step turns.Step) bool {
	if step.Kind != turns.StepTool {
		return false
	}
	name, _ := turns.ToolCall(step.Entry)
	return name != turns.ReportDoneTool
}

// toolRun is the one line a run of tool calls folds into. It opens into the calls
// themselves, each row the same as a single call's, so nothing is lost — only put
// away until it is wanted.
func (a *App) toolRun(c *ui.Context, t theme.Tokens, turn *turns.Turn, steps []turns.Step) {
	key := itoa(turn.ID) + ":" + itoa(steps[0].Entry.ID)
	open := a.stepGroups[key]
	label := i18n.TF("toolCallsCount", map[string]any{"count": len(steps)})
	ui.Column(c).Gap(4).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).PaddingY(1).Children(func() {
			ui.Row(c).Size(5, 5).Radius(3).Background(t.MutedFg)
			chevron := icons.ChevronRight
			if open {
				chevron = icons.ChevronDown
			}
			ui.Icon(c, chevron).Size(10, 10).TextColor(t.MutedFg)
			ui.Text(c, label).FontSize(10.5).TextColor(t.MutedFg)
			// What the run came to, so a folded run still says what it did.
			if last := steps[len(steps)-1].Label; last != "" && !open {
				ui.Text(c, "·").FontSize(10.5).TextColor(t.MutedFg)
				ui.Text(c, last).FontSize(10.5).TextColor(t.MutedFg)
			}
		}).OnClick(func() {
			a.apply(func() {
				a.stepGroups[key] = !open
				a.invalidate()
			})
		})
		if !open {
			return
		}
		ui.Column(c).Key("run-" + key).Transition(motion.AccordionTransition()).Children(func() {
			ui.Column(c).Gap(4).PaddingY(2).Children(func() {
				for _, step := range steps {
					a.stepRow(c, t, step)
				}
			})
		})
	})
}

// verdictBand is what report_done draws in place of a step: the turn's answer to
// whether it met its goal. It sits on the timeline like a step dot but never
// folds — the line under the verdict is the answer, the one saying how it was
// checked, so it reads at body size instead of as a capped log.
func (a *App) verdictBand(c *ui.Context, t theme.Tokens, met bool, why string) {
	accent, title := t.Foreground, i18n.T("goalMet")
	if !met {
		accent, title = t.Destructive, i18n.T("goalNotMet")
	}
	ui.Column(c).Gap(3).PaddingY(2).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Size(5, 5).Radius(3).Background(accent)
			ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(accent)
		})
		if why != "" {
			ui.Text(c, why).FontSize(12.5).TextColor(t.Foreground).Wrap()
		}
	})
}

// stepColor is the dot a step gets: thinking is quiet, a tool call is the
// accent, and a failed one is the danger colour.
func stepColor(step turns.Step, t theme.Tokens) ui.Color {
	switch step.Kind {
	case turns.StepError:
		return t.Destructive
	case turns.StepThink:
		return t.Border
	}
	return t.Primary
}

// stepOutput shows what a tool answered: a patch rendered as a diff when it
// changed files, the text otherwise, both capped so a pasted log cannot push
// the chat away.
func (a *App) stepOutput(c *ui.Context, t theme.Tokens, text string, failed bool) {
	files := diff.Parse(text)
	if added, removed, paths := diff.Stats(files); paths > 0 && added+removed > 0 {
		colors := diff.Light()
		if c.Theme().Dark {
			colors = diff.Dark()
		}
		a.patchView.Render(c, files, diff.Options{Colors: colors})
		return
	}
	color := t.Foreground
	if failed {
		color = t.Destructive
	}
	ui.Column(c).Fill().Padding(6, 8).Radius(6).Background(t.Muted).Children(func() {
		ui.Text(c, truncateLines(text, 40, 4000)).FontSize(10).TextColor(color)
	})
}

// truncateLines keeps the head of a long output and says what it dropped.
func truncateLines(text string, maxLines, maxRunes int) string {
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		text = strings.Join(lines[:maxLines], "\n") + "\n… " + itoa(len(lines)-maxLines) + " more lines"
	}
	return truncate(text, maxRunes)
}

// caretLabel names the streaming caret, which is how the test that it blinks
// finds it among the marks that animate around it.
const caretLabel = "caret"

// liveLine shows what the running turn is doing now: the phase, the tool it
// waits on, and the text as it streams.
func (a *App) liveLine(c *ui.Context, t theme.Tokens, turn *turns.Turn) {
	state := turn.State(a.live)
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(6).Children(func() {
			// The marker is the app's mark drawing itself, where a spinner said
			// only "busy".
			drawnLogo(c, 20, brand.Ink(t.SidebarFg))
			ui.Text(c, stateLabel(state)).FontSize(11).TextColor(t.Primary)
			if state.ToolTail != "" {
				ui.Text(c, truncate(state.ToolTail, 120)).FontSize(10).TextColor(t.MutedFg)
			}
		})
		if strings.TrimSpace(state.Text) != "" {
			// Stream, not View: half-written Markdown must not flicker between
			// frames as the parser re-reads it.
			// No Fill: the card hugs the text it holds and grows with it, which
			// is how the settled card measures itself, so completing a turn
			// changes nothing but the caret.
			// Hug the text, cap at the width a settled card takes. Inside a
			// column a card stretches unless it says otherwise.
			body := ui.Column(c).MaxWidth(answerWidth).AlignSelf(ui.Start).Padding(8, 12).Radius(12).
				Border(1, t.Border).Background(t.Card).Label(streamLabel)
			body.Children(func() {
				markdown.Stream(c, state.Text, &a.md)
				// A caret at the end of the streaming text says the agent is
				// still writing, where a spinner alone only says "busy". It
				// blinks at the speed of a text cursor, and Loop keeps the
				// frames coming while it is built.
				if state.Kind == turns.StateStreaming {
					if blink := body.Loop("caret", 1060*time.Millisecond, ui.Linear); blink < 0.5 {
						// Labelled so a test can find it: the logo beside the
						// phase animates too, so a band of pixels no longer
						// isolates the caret.
						ui.Box(c).Width(6).Height(13).Radius(1).Background(t.Primary).
							AlignSelf(ui.Start).Label(caretLabel)
					}
				}
			})
		}
	})
}

// stateLabel words the live phase with the client's own vocabulary: the tool
// name while one runs, otherwise the shared i18n words.
func stateLabel(state turns.State) string {
	switch state.Kind {
	case turns.StateThinking, turns.StateStreaming, turns.StateTool:
		if state.Kind == turns.StateTool && state.Tool != "" {
			return state.Tool + "…"
		}
		return i18n.T("thinking") + "…"
	case turns.StateSettled:
		return i18n.T("idle")
	}
	return i18n.T("running") + "…"
}

// turnSummary is the accounting line under a turn: tools, files, diff size,
// cost and duration.
func turnSummary(turn *turns.Turn) string {
	parts := make([]string, 0, 5)
	if turn.Stats.Tools > 0 {
		parts = append(parts, plural(turn.Stats.Tools, "tool"))
	}
	if turn.Stats.Files > 0 {
		parts = append(parts, plural(turn.Stats.Files, "file"))
	}
	if turn.Stats.Added > 0 || turn.Stats.Removed > 0 {
		parts = append(parts, "+"+itoa(turn.Stats.Added)+" −"+itoa(turn.Stats.Removed))
	}
	if turn.Stats.Cost > 0 {
		parts = append(parts, "$"+money(turn.Stats.Cost))
	}
	if turn.Stats.MS != nil {
		parts = append(parts, turns.Clock(*turn.Stats.MS))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return itoa(n) + " " + what + "s"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

func money(cost float64) string {
	cents := int(cost*100 + 0.5)
	whole := itoa(cents / 100)
	frac := cents % 100
	if frac < 10 {
		return whole + ".0" + itoa(frac)
	}
	return whole + "." + itoa(frac)
}

// noteText reads the payload of an app.* entry, which says what happened in a
// sentence rather than in messages.
func noteText(entry protocol.Entry) string {
	if len(entry.Data) == 0 {
		return ""
	}
	var payload struct {
		Text    string `json:"text"`
		Message string `json:"message"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(entry.Data, &payload); err != nil {
		return ""
	}
	for _, candidate := range []string{payload.Text, payload.Message, payload.Summary} {
		if strings.TrimSpace(candidate) != "" {
			return candidate
		}
	}
	return ""
}

// jumpBar is the "N new messages / jump to latest" strip the transcript view:331
// floats over the end of the chat: it only appears once the user has scrolled
// away from the newest turn.
func (a *App) jumpBar(c *ui.Context) {
	if a.atBottom || a.currentValue() == 0 {
		return
	}
	label := i18n.T("jumpToLatest")
	if a.newMessages > 0 {
		label = itoa(a.newMessages) + " " + i18n.T("newMessages")
	}
	ui.Row(c).FillWidth().Padding(0, 14, 4, 14).Justify(ui.End).
		Transition(motion.FadeIn()).Children(func() {
		pill := ui.ButtonBase(c).Radius(12).Padding(2, 10).Material(pillGlass()).
			Cursor(ui.CursorPointer)
		pill.Children(func() {
			ui.Text(c, label).FontSize(10).TextColor(theme.Current(c).Primary)
		})
		pill.OnClick(func() {
			a.apply(func() {
				a.transcriptScroll.ScrollTo(0, math.MaxFloat32, ui.Animation{})
				a.newMessages = 0
				a.invalidate()
			})
		})
	})
}

// followScroll keeps the newest turn in view until the user scrolls away, and
// counts what arrives while they are away: the transcript view:231 starts stuck and
// :286 jumps to the end whenever entries grow.
func (a *App) followScroll() {
	state := &a.transcriptScroll
	if a.pageGliding {
		// A page is on its way: let the glide land before deciding whether the
		// transcript follows its end again.
		if state.Scrolling() {
			a.invalidate()
			return
		}
		a.pageGliding = false
	}
	if state.MaxY <= 0 {
		// Content that fits the pane cannot be scrolled away from.
		a.atBottom, a.stickToBottom, a.newMessages = true, true, 0
		a.scrollY, a.scrollMaxY = 0, 0
		return
	}
	if state.Y > state.MaxY+0.5 {
		// The offset is still the sentinel that means "the end", not a place the
		// frame has landed on: the next frame reports where that is. What the
		// reader sees meanwhile is the end, and remembering it is what lets a
		// flick up be read at all when it lands.
		a.scrollY, a.scrollMaxY = state.MaxY, state.MaxY
		return
	}
	if state.Y > 24 {
		a.scrolledFromTop = true
	}
	switch {
	case state.MaxY+0.5 >= a.scrollMaxY && state.Y < a.scrollY-1 && state.Y < a.scrollMaxY-1:
		// The view moved up since the last frame, and sits above the end that
		// frame stood at: the reader scrolled up, however few points. An answer
		// arriving cannot do this — it grows the end, never lifts the view — and
		// this is the case the old "close enough to the end" band swallowed,
		// which slid a ten-point flick straight back down and left the transcript
		// stuck.
		a.atBottom, a.stickToBottom = false, false
	case !a.stickToBottom && state.Y > a.scrollY+1 && state.Y >= state.MaxY-2:
		// Back at the very end: follow again, and drop the count of what arrived
		// while the reader was away. The end, not "close to it": a reader who
		// scrolls up twenty points and stops there used to be yanked the twenty
		// points back down, which read as the view jumping.
		a.atBottom, a.stickToBottom, a.newMessages = true, true, 0
	}
	// At the top of a chat that still holds turns back: let the next sixty in, so
	// a history the reader scrolls through reads as one, and a chat that began
	// "in the middle" — the window drew the newest sixty and a button held the
	// rest — begins where it began.
	if a.scrolledFromTop && state.Y <= 24 && a.turnWindow < a.turnTotal {
		a.turnWindow += 60
		a.invalidate()
	}
	a.scrollY = min(state.Y, state.MaxY)
	a.scrollMaxY = state.MaxY
	if a.stickToBottom {
		// Assigning Y is instant; ScrollTo would glide, leaving the view short
		// of the end for a few frames while an answer streams.
		state.Y = math.MaxFloat32
	}
}

// scrollToNewest parks the transcript at the end, which is where a newly
// selected chat should open.
func (a *App) scrollToNewest() {
	a.apply(func() {
		a.stickToBottom, a.atBottom, a.newMessages = true, true, 0
		a.transcriptScroll.Y = math.MaxFloat32
		a.invalidate()
	})
}

// isHit reports whether the search the reader ran points at this entry: the
// rail's cross-chat hits carry an entry id, so the answer itself can be marked
// instead of only its turn.
func (a *App) isHit(entry protocol.Entry) bool {
	return a.focusEntry != 0 && entry.ID == a.focusEntry
}

// copyText puts an answer on the clipboard as both Markdown source and plain
// text: pasting into a Markdown editor keeps the structure, pasting anywhere
// else reads as prose. A page can only reach the clipboard through the async
// API, which drops the richer form in some browsers.
func (a *App) copyText(c *ui.Context, text string) {
	data := transfer.New(transfer.NewItem(
		transfer.Bytes(transfer.Text, []byte(text)),
		transfer.Bytes(transfer.Markdown, []byte(text)),
	))
	c.Services().WriteClipboardData(data)
	c.Toast(i18n.T("copied"))
}
