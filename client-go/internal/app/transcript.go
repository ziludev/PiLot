package app

import (
	"encoding/json"
	"path/filepath"
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

// transcript is the middle pane: one row per part of a turn, oldest first,
// with the live run at the bottom. The list builds only the rows in view, so a
// chat of a thousand turns costs what the screen shows.
func (a *App) transcript(c *ui.Context) {
	t := theme.Current(c)
	built := a.groupedTurns()
	a.turnTotal = len(built)
	rows := a.buildChatRows(built)
	a.chatRows, a.chatBuilt = rows, built
	if a.jumpRow(rows, built) {
		// The find bar's hit is a row: the list lands on it exactly, however
		// tall the turns above it are.
		a.turnList.ScrollTo(a.jumpTarget, ui.Center)
	}
	a.turnList.Key = func(row int) any {
		if row < 0 || row >= len(rows) {
			return row
		}
		return rows[row].key
	}
	ui.List(c, &a.turnList, len(rows), func(row int) {
		a.chatRow(c, t, rows[row])
	})
	// AtEnd reports the last frame, which is the one the reader is looking at:
	// a frame of lag on a chip is not a frame of lag on the chat.
	a.atBottom = a.turnList.AtEnd()
	if a.atBottom {
		a.newMessages = 0
	}
	// The top of what is loaded is where the next page is read from. Visible is
	// read only while a page is waiting, so the extra frame it asks for is a
	// frame the reader asked for first.
	if a.historyMore && !a.historyLoading {
		if first, last := a.turnList.Visible(); last >= 0 && first <= 0 {
			a.loadOlder()
		}
	}
}

// jumpRow finds the row the reader asked for: a search hit points at an entry,
// the find bar at a turn. The list scrolls there once per ask, not every frame.
func (a *App) jumpRow(rows []chatRow, built []turns.Turn) bool {
	var want func(chatRow) bool
	key := ""
	switch target := a.focusEntry; {
	case target != 0:
		key = "entry:" + itoa(target)
		want = func(row chatRow) bool {
			steps := row.steps
			switch row.kind {
			case chatFold:
				// A hit inside a folded turn lands on the line that holds it.
				steps = row.folded
			case chatSteps:
			default:
				return row.entry.ID == target
			}
			for _, step := range steps {
				if step.Entry.ID == target || (step.Result != nil && step.Result.ID == target) {
					return true
				}
			}
			return false
		}
	case a.focusedTurn() >= 0 && a.focusedTurn() < len(built):
		turn := a.focusedTurn()
		key = "turn:" + itoa(turn)
		want = func(row chatRow) bool { return row.turn == &built[turn] }
	}
	if key == "" || key == a.jumpKey {
		if key == "" {
			a.jumpKey, a.jumpTarget = "", -1
		}
		return false
	}
	a.jumpKey = key
	for i, row := range rows {
		if want(row) {
			a.jumpTarget = i
			return true
		}
	}
	a.jumpTarget = -1
	return false
}

// stepGroups folds each run of neighbouring tool calls into one group, which is
// the line the chat shows for them: a turn that runs a dozen commands used to
// put a dozen lines between the question and the answer.
func stepGroups(turn *turns.Turn) [][]turns.Step {
	groups := make([][]turns.Step, 0, len(turn.Steps))
	for i := 0; i < len(turn.Steps); {
		if !foldsIntoRun(turn.Steps[i]) {
			groups = append(groups, turn.Steps[i:i+1])
			i++
			continue
		}
		j := i + 1
		for j < len(turn.Steps) && foldsIntoRun(turn.Steps[j]) {
			j++
		}
		groups = append(groups, turn.Steps[i:j])
		i = j
	}
	return groups
}

// groupedTurns groups the entries once per change. Grouping parses every
// entry's model, which cost more than a frame's budget on a long chat, and a
// frame that only scrolls or hovers changes none of them.
func (a *App) groupedTurns() []turns.Turn {
	if a.turnStale || a.turnCache == nil {
		a.turnCache = turns.GroupTurns(a.entries)
		a.turnStale = false
	}
	return a.turnCache
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

// delegationRow is who a delegate call ran: a line per child, with a way into the
// child's own conversation. It sits above the output because the child's answer is
// long, and who gave it is what a reader looks for first — and it stays visible
// when the call is folded.
func (a *App) delegationRow(c *ui.Context, t theme.Tokens, result protocol.Entry) {
	run, ok := turns.DelegationOf(result)
	if !ok {
		return
	}
	// A batch that recorded no conversations can still say it failed, but there is
	// nobody to hang the failure on: it gets one line for the whole batch.
	openable := false
	for _, child := range run.Children {
		if child.ConversationID != 0 {
			openable = true
		}
	}
	ui.Column(c).Gap(2).Children(func() {
		if !openable && run.Failed > 0 {
			roles := make([]string, len(run.Children))
			for i, child := range run.Children {
				roles[i] = child.Role
			}
			a.childLine(c, t, strings.Join(roles, " / "), run.Failed, 0)
			return
		}
		for _, child := range run.Children {
			failed := 0
			if child.Failed {
				failed = 1
			}
			a.childLine(c, t, child.Role, failed, child.ConversationID)
		}
	})
}

// childLine is one child: its role, whether it failed, and the way into it. The
// whole line is the target, because a word-sized click is a click that misses.
func (a *App) childLine(c *ui.Context, t theme.Tokens, role string, failed, conversationID int) {
	line := ui.Row(c).Gap(6).AlignItems(ui.Center).Padding(0, 2).Radius(4)
	if conversationID != 0 {
		line.Cursor(ui.CursorPointer)
		if line.Hovered() {
			line.Background(t.Accent)
		}
	}
	line.Children(func() {
		ui.Text(c, "→ "+role).FontSize(10.5).TextColor(t.MutedFg)
		if failed > 0 {
			ui.Text(c, "·").FontSize(10.5).TextColor(t.MutedFg)
			ui.Text(c, i18n.TF("toolRunFailed", map[string]any{"count": failed})).
				FontSize(10.5).TextColor(t.Destructive)
		}
		if conversationID != 0 {
			ui.Text(c, "·").FontSize(10.5).TextColor(t.MutedFg)
			ui.Text(c, i18n.T("openChildChat")).FontSize(10.5).TextColor(t.Primary)
		}
	})
	if conversationID != 0 {
		id := conversationID
		line.OnClick(func() { a.openChildChat(id) })
	}
}

// stepRow is one tool call or answer on the turn's timeline: a line that opens
// into what the tool returned, a patch when it changed files.
func (a *App) stepRow(c *ui.Context, t theme.Tokens, step turns.Step, live bool) {
	if name, _ := turns.ToolCall(step.Entry); name == turns.ReportDoneTool {
		if met, ok, why := turns.Verdict(step.Entry); ok {
			a.verdictBand(c, t, met, why)
			return
		}
	}
	// A failed call opens itself: the reason is the part worth reading, and a row
	// that hides it behind a click is a row that hides the failure.
	open, decided := a.stepOpen[step.Entry.ID]
	if !decided && step.Failed() {
		open = true
	}
	label := step.Label
	if label == "" {
		label = "tool"
	}
	ui.Column(c).Gap(4).Children(func() {
		// One line per step: a dot, the chevron that says it opens, the label.
		// All three centre on the same baseline as the transcript's StepRow.
		ui.Row(c).Gap(6).AlignItems(ui.Center).PaddingY(1).Children(func() {
			dot := ui.Row(c).Size(5, 5).Radius(3).Background(stepColor(step, t))
			if live {
				// The running step's dot breathes between two tones of its own
				// colour: the timeline then says where the turn has got to
				// without a spinner on every row.
				beat := dot.Loop("pulse", motion.Pulse, ui.Bounce(ui.EaseInOut))
				dot.Background(stepColor(step, t).Alpha(motion.PulseLow + (1-motion.PulseLow)*beat))
			}
			chevron := icons.ChevronRight
			if open {
				chevron = icons.ChevronDown
			}
			ui.Icon(c, chevron).Size(10, 10).TextColor(t.MutedFg)
			ui.Text(c, label).FontSize(10.5).TextColor(t.MutedFg)
			// How long the call is taking: the running one counts up on every frame,
			// a finished one keeps its time if it was slow enough to notice.
			if ms := step.Elapsed(time.Now().UnixMilli()); live || ms >= slowStepMS {
				ui.Text(c, "· "+turns.Clock(ms)).FontSize(10.5).TextColor(t.MutedFg)
			}
		}).OnClick(func() {
			a.apply(func() {
				a.stepOpen[step.Entry.ID] = !open
				a.invalidate()
			})
		})
		if step.Result != nil {
			a.delegationRow(c, t, *step.Result)
		}
		if !open || step.Result == nil {
			return
		}
		text, failed := turns.ToolResult(*step.Result)
		// The row already said it: a failed call's label is its error text, and
		// printing the same line again under it reads like two failures.
		if strings.TrimSpace(text) == strings.TrimSpace(label) {
			return
		}
		if strings.TrimSpace(text) == "" {
			return
		}
		// TurnRail's accordion: the output grows into place over 350 ms
		// instead of appearing at its full height.
		ui.Column(c).Key(step.Entry.ID).Transition(motion.AccordionTransition()).
			Children(func() { a.stepOutput(c, t, step.Entry.ID, text, failed) })
	})
}

// foldsIntoRun says whether a step joins the run of tool calls a turn folds. The
// verdict does not: it is the turn's answer, and it is read, not unfolded.
func foldsIntoRun(step turns.Step) bool {
	if step.Kind != turns.StepTool && step.Kind != turns.StepError {
		return false
	}
	name, _ := turns.ToolCall(step.Entry)
	// A step that is not a call at all (a protocol error, say) is its own line; a
	// failed call belongs to its run, which counts it and opens itself to show it.
	if name == "" {
		return false
	}
	// A call that stands for a whole other conversation is not one of a run of
	// calls: folding it away would fold the child's identity with it.
	if name == turns.DelegateTool || name == turns.DelegateParallelTool {
		return false
	}
	return name != turns.ReportDoneTool
}

// toolRun is the one line a run of tool calls folds into. It opens into the calls
// themselves, each row the same as a single call's, so nothing is lost — only put
// away until it is wanted.
func (a *App) toolRun(c *ui.Context, t theme.Tokens, turn *turns.Turn, steps []turns.Step, live bool) {
	key := itoa(turn.ID) + ":" + itoa(steps[0].Entry.ID)
	// A run that failed is not one the reader should have to click to find out:
	// it starts open, and only their own click closes it.
	open, decided := a.stepGroups[key]
	failures := turns.Failures(steps)
	if !decided && failures > 0 {
		open = true
	}
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
			// What the run cost and what came back broken, before it is opened.
			if failures > 0 {
				ui.Text(c, "·").FontSize(10.5).TextColor(t.MutedFg)
				ui.Text(c, i18n.TF("toolRunFailed", map[string]any{"count": failures})).
					FontSize(10.5).TextColor(t.Destructive)
			}
			if ms := turns.RunElapsed(steps, time.Now().UnixMilli()); ms >= slowStepMS {
				ui.Text(c, "·").FontSize(10.5).TextColor(t.MutedFg)
				ui.Text(c, turns.Clock(ms)).FontSize(10.5).TextColor(t.MutedFg)
			}
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
					a.stepRow(c, t, step, live)
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
func (a *App) stepOutput(c *ui.Context, t theme.Tokens, entryID int, text string, failed bool) {
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
	// A long answer is capped, never cut in silence: the row under it says how many
	// lines are held back and opens them. Silence is what makes "the output was
	// short" and "the output was cut" look the same.
	all := strings.Split(strings.TrimRight(text, "\n"), "\n")
	open := a.outputOpen[entryID]
	limit := cappedOutputLines
	if open {
		limit = expandedOutputLines
	}
	shown, hidden := all, 0
	if len(all) > limit {
		shown, hidden = all[:limit], len(all)-limit
	}
	for i, line := range shown {
		shown[i] = truncate(line, maxOutputLineRunes)
	}
	ui.Column(c).Fill().Gap(4).Padding(6, 8).Radius(6).Background(t.Muted).Children(func() {
		ui.Text(c, strings.Join(shown, "\n")).FontSize(10).TextColor(color)
		switch {
		case open:
			ui.Text(c, i18n.T("collapseOutput")).FontSize(10).TextColor(t.MutedFg).
				OnClick(func() {
					a.apply(func() {
						delete(a.outputOpen, entryID)
						a.invalidate()
					})
				})
		case len(all) > cappedOutputLines:
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.TF("hiddenLines", map[string]any{"count": hidden})).FontSize(10).TextColor(t.MutedFg)
				ui.Text(c, "·").FontSize(10).TextColor(t.MutedFg)
				ui.Text(c, i18n.T("showAllLines")).FontSize(10).TextColor(t.Primary)
			}).OnClick(func() {
				a.apply(func() {
					a.outputOpen[entryID] = true
					a.invalidate()
				})
			})
		}
		if open && hidden > 0 {
			ui.Text(c, i18n.TF("hiddenLines", map[string]any{"count": hidden})).FontSize(10).TextColor(t.MutedFg)
		}
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

// How much of a tool's answer is worth showing before it is meant to be opened:
// twelve lines read at a glance, and the whole thing capped again at four hundred
// so a pasted log cannot cost a frame per line.
const (
	cappedOutputLines   = 12
	expandedOutputLines = 400
	maxOutputLineRunes  = 400
	// slowStepMS is when a finished call earns its duration on the line: below a
	// second the number is noise.
	slowStepMS = 1000
)

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
// summaryLine is the turn's stats, and the files behind the "2 files" in them:
// the count is what the engine reported, the list is what the calls named, so a
// reader who wants to know which files does not have to open every call.
func (a *App) summaryLine(c *ui.Context, t theme.Tokens, turn *turns.Turn) {
	files := turns.EditedFiles(turn.Steps)
	summary := turnSummary(turn)
	if len(files) == 0 {
		ui.Text(c, summary).FontSize(9).TextColor(t.MutedFg)
		return
	}
	open := a.filesOpen[turn.ID]
	line := ui.Row(c).Gap(4).AlignItems(ui.Center).Radius(3).Padding(0, 2)
	line.Cursor(ui.CursorPointer)
	if line.Hovered() {
		line.Background(t.Accent)
	}
	line.Children(func() {
		ui.Text(c, summary).FontSize(9).TextColor(t.MutedFg)
	})
	line.OnClick(func() {
		a.apply(func() {
			a.filesOpen[turn.ID] = !open
			a.invalidate()
		})
	})
	if !open {
		return
	}
	ui.Text(c, "✎ "+fileList(files)).FontSize(9).TextColor(t.MutedFg).Padding(0, 2)
}

// fileList names the files a turn touched, keeping the line one line: the tail
// becomes a count rather than a second line nobody reads.
func fileList(files []string) string {
	const shown = 4
	names := make([]string, 0, shown)
	for i, file := range files {
		if i == shown {
			break
		}
		names = append(names, filepath.Base(file))
	}
	if rest := len(files) - len(names); rest > 0 {
		return strings.Join(names, ", ") + " +" + itoa(rest)
	}
	return strings.Join(names, ", ")
}

// stepsFoldAt is where a turn stops being read step by step. Three steps is a
// turn whose shape is the whole story; past that the steps are the road rather
// than the answer.
const stepsFoldAt = 3

// runningStep is the step a live turn is on: the last one, whose result has
// not landed. It keeps its own row folded or not — it is the only line that
// says the agent is still working.
func runningStep(turn *turns.Turn, live bool) int {
	if !live || len(turn.Steps) == 0 {
		return -1
	}
	return len(turn.Steps) - 1
}

// stepsFoldLine is what a long turn's finished steps fold into: how many there
// were, what they cost, and the way back into them. The count is the turn's
// steps, not Stats.Tools: that one counts tool calls, so it skips the thinking
// message and the verdict and would undercount the turn this line stands for.
func (a *App) stepsFoldLine(c *ui.Context, t theme.Tokens, turn *turns.Turn) {
	open := a.stepsOpen[turn.ID]
	chevron := icons.ChevronRight
	if open {
		chevron = icons.ChevronDown
	}
	line := ui.Row(c).Gap(6).AlignItems(ui.Center).Radius(3).Padding(0, 2)
	line.Cursor(ui.CursorPointer)
	if line.Hovered() {
		line.Background(t.Accent)
	}
	line.OnClick(func() {
		a.apply(func() {
			a.stepsOpen[turn.ID] = !open
			a.invalidate()
		})
	})
	line.Children(func() {
		ui.Icon(c, chevron).Size(9, 9).TextColor(t.MutedFg)
		ui.Icon(c, icons.Check).Size(10, 10).TextColor(t.Primary)
		ui.Text(c, itoa(len(turn.Steps))+" "+i18n.T("stepsCount")).FontSize(10).TextColor(t.MutedFg)
		// Opened, the file list and the clock are the summary line's job below;
		// repeating them here would make the turn say everything twice.
		if !open {
			if rest := strings.Join(summaryParts(turn, false), " · "); rest != "" {
				ui.Text(c, rest).FontSize(9).TextColor(t.MutedFg)
			}
		}
	})
}

func turnSummary(turn *turns.Turn) string {
	return strings.Join(summaryParts(turn, true), " · ")
}

// summaryParts is what the turn cost, in the order the two callers need it.
// The folded line counts steps instead of tool calls, so it drops that part and
// puts its own count in front.
func summaryParts(turn *turns.Turn, tools bool) []string {
	parts := make([]string, 0, 5)
	if tools && turn.Stats.Tools > 0 {
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
	return parts
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
				a.turnList.ScrollToEnd()
				a.newMessages = 0
				a.invalidate()
			})
		})
	})
}

// scrollToNewest parks the transcript at the end, which is where a newly
// selected chat should open.
func (a *App) scrollToNewest() {
	a.apply(func() {
		a.atBottom, a.newMessages = true, 0
		a.turnList.ScrollToEnd()
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
	// 1.8 s is the toast's own life (motion.ToastLife): short enough for a
	// confirmation that needs no reading twice.
	c.AddToast(ui.Toast{ID: "copied", Title: i18n.T("copied"), Timeout: motion.ToastLife})
}
