package app

import (
	"encoding/json"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/theme"
)

// maxContent is how wide the transcript reads: full window width is a line no
// eye can follow, so the column is capped and centred.
const maxContent = 820

// transcriptView draws the open chat. It is read-only: the only things a click
// does here are unfold a run's steps and scroll.
func (a *App) transcriptView(c *ui.Context, t theme.Tokens) {
	th := a.threads[a.active]
	if th == nil || (!th.loaded && !th.failed) {
		// Still reading: the page arrives in a frame or two, and a spinner for
		// that long would only flicker.
		ui.Box(c).Grow(1)
		return
	}
	if th.failed {
		convID := a.active
		ui.Row(c).Grow(1).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Gap(6).Children(func() {
			ui.Text(c, i18n.T("loadFailed")).FontSize(11.5).TextColor(t.Destructive)
			retry := ui.ButtonBase(c).Radius(6).Padding(2, 8).Cursor(ui.CursorPointer)
			retry.Children(func() {
				ui.Text(c, i18n.T("retry")).FontSize(11).TextColor(t.Primary)
			})
			retry.OnClick(func() { a.loadThread(convID) })
		})
		return
	}
	rows := a.threadRows(a.active)
	if live, ok := a.liveRow(); ok {
		// Copied, because the projection is cached and this row changes every
		// frame: appending in place would grow the cache with streaming text.
		rows = append(rows[:len(rows):len(rows)], live)
	}
	if len(rows) == 0 {
		ui.Row(c).Grow(1).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("emptyThread")).FontSize(11.5).TextColor(t.MutedFg)
		})
		return
	}
	// FollowEnd keeps the newest turn in view until the reader scrolls away,
	// without the view having to remember where the end was.
	// Following the end and holding a hit are two answers to the same question,
	// and a search wins: otherwise the newest turn pulls the view back down.
	a.vm.threadList.FollowEnd = !a.findBrowsing()
	divider := a.dividerRow(a.active, rows)
	a.vm.threadList.Key = func(row int) any {
		if row < 0 || row >= len(rows) {
			return row
		}
		return rows[row].id
	}
	ui.Row(c).Grow(1).FillWidth().Justify(ui.Center).Children(func() {
		ui.Column(c).Grow(1).FillHeight().MaxWidth(maxContent).Children(func() {
			ui.List(c, &a.vm.threadList, len(rows), func(row int) {
				if row != divider {
					a.threadRowMarked(c, t, rows[row], a.findMark(row))
					return
				}
				ui.Column(c).FillWidth().Children(func() {
					a.newLine(c, t)
					a.threadRowMarked(c, t, rows[row], a.findMark(row))
				})
			}).Grow(1).Padding(8, 0, 12, 0)
			a.jumpToLatest(c, t, len(rows))
		})
	})
}

// dividerRow is where the line marking new entries goes: above the first row
// written after the user last had this chat open. -1 means no line — nothing
// arrived since, or this is the first time they have opened it, in which case
// everything is new and a line would say nothing.
func (a *App) dividerRow(convID int, rows []threadRow) int {
	from := a.vm.newFrom[convID]
	if from == 0 {
		return -1
	}
	for i, row := range rows {
		if row.entry > from {
			// A line above everything separates nothing: the whole page is new,
			// and the rail's dot already said so.
			if i == 0 {
				return -1
			}
			return i
		}
	}
	return -1
}

// newLine is the rule between what the reader has seen and what arrived while
// they were in another chat.
func (a *App) newLine(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Padding(10, 0).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Grow(1).Height(1).Background(t.Destructive.Alpha(0.45))
		ui.Text(c, i18n.T("newHere")).FontSize(9).TextColor(t.Destructive)
		ui.Box(c).Grow(1).Height(1).Background(t.Destructive.Alpha(0.45))
	})
}

// jumpToLatest is the pill that appears once the reader has scrolled up: the
// transcript follows a running turn, so the only way to read an older answer is
// to leave the end, and then the only way back is a button.
func (a *App) jumpToLatest(c *ui.Context, t theme.Tokens, rows int) {
	if rows == 0 || a.findBrowsing() {
		return
	}
	_, last := a.vm.threadList.Visible()
	if last >= rows-1 {
		return
	}
	pill := ui.ButtonBase(c).Absolute().Right(24).Bottom(12).Height(26).Radius(13).
		Padding(0, 10).Gap(4).AlignItems(ui.Center).Background(t.Card).
		Border(1, t.Border).Cursor(ui.CursorPointer).Tooltip(i18n.T("jumpLatest"))
	pill.Children(func() {
		ui.Icon(c, icons.ChevronDown).Size(11, 11).TextColor(t.Foreground)
		ui.Text(c, i18n.T("jumpLatest")).FontSize(10.5).TextColor(t.Foreground).MaxLines(1)
	})
	pill.OnClick(a.scrollToLatest)
}

// scrollToLatest puts the reader back at the newest words and keeps them there
// as the answer arrives. It is what the pill does and what ⌘J does, so the two
// cannot disagree.
func (a *App) scrollToLatest() {
	a.vm.threadList.FollowEnd = true
	a.vm.threadList.ScrollToEnd()
	a.invalidate()
}

// threadRowMarked tints the row the find bar is standing on, and leaves the
// rest alone: the bar counts the other hits, and a row tinted "somewhere else"
// only competes with the one the reader is about to read — which is how every
// browser's find bar does it too.
func (a *App) threadRowMarked(c *ui.Context, t theme.Tokens, row threadRow, mark int) {
	if mark != 1 {
		a.threadRow(c, t, row)
		return
	}
	ui.Column(c).FillWidth().Radius(8).Background(t.Selection).Children(func() {
		a.threadRow(c, t, row)
	})
}

func (a *App) threadRow(c *ui.Context, t theme.Tokens, row threadRow) {
	switch row.kind {
	case rowUser:
		a.userRow(c, t, row)
	case rowAssistant:
		a.assistantRow(c, t, row)
	case rowThinking:
		a.thinkingRow(c, t, row)
	case rowSteps:
		a.stepsRow(c, t, row)
	case rowError:
		a.errorRow(c, t, row)
	case rowLive:
		a.liveRowView(c, t, row)
	}
}

// userRow is the right-aligned bubble: the user's own words, tinted so they are
// not mistaken for the agent's.
func (a *App) userRow(c *ui.Context, t theme.Tokens, row threadRow) {
	outer := ui.Row(c).FillWidth().Justify(ui.End).Padding(6, 0).Gap(9).AlignItems(ui.Start)
	hovered := outer.Hovered()
	outer.Children(func() {
		ui.Column(c).MaxWidth(560).Gap(4).Children(func() {
			bubble := ui.Column(c).Padding(8, 12).Radius(12).
				Background(t.Secondary).Gap(6)
			bubble.Children(func() {
				a.images(c, row.images, 180, 120)
				if row.text != "" {
					ui.Text(c, row.text).FontSize(12.5).TextColor(t.SecondFg)
				}
			})
			a.messageActions(c, t, row, hovered, true)
		})
		a.userAvatarView(c, t, avatarSize)
	})
}

// assistantRow is full width and has no bubble: the answer belongs to the page,
// not to a card.
func (a *App) assistantRow(c *ui.Context, t theme.Tokens, row threadRow) {
	outer := ui.Row(c).FillWidth().Padding(6, 0).Gap(9).AlignItems(ui.Start)
	hovered := outer.Hovered()
	outer.Children(func() {
		a.avatarView(c, avatarSize)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			a.images(c, row.images, 240, 160)
			if row.text != "" {
				markdown.View(c, row.text, nil)
			}
			a.messageActions(c, t, row, hovered, false)
		})
	})
}

// lastLineText is where a thought currently is: the start has already been
// read, and one line is all the live row has room for.
func lastLineText(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return clip(oneLine(line), 140)
		}
	}
	return ""
}

// thinkingRow is the model's reasoning as a block of its own. Folded it is a
// line with the first thing it thought; open it is the whole trace, markdown
// and all. The tool calls stay a separate fold, the way assistant-ui keeps
// Reasoning and ToolGroup apart: "4 步" should mean four calls.
func (a *App) thinkingRow(c *ui.Context, t theme.Tokens, row threadRow) {
	key := row.id
	open := a.vm.folds[key]
	// In the answer's own column: a thought belongs to the message it led to,
	// not to the middle of the page.
	ui.Row(c).FillWidth().Padding(6, 0).Gap(9).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Width(avatarSize)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			header := ui.ButtonBase(c).FillWidth().Height(22).Radius(5).Padding(0).
				Justify(ui.Start).Cursor(ui.CursorPointer).Gap(4).AlignItems(ui.Center).
				Label(i18n.T("thinking"))
			header.Children(func() {
				if open {
					ui.Icon(c, icons.ChevronDown).Size(10, 10).TextColor(t.MutedFg)
				} else {
					ui.Icon(c, icons.ChevronRight).Size(10, 10).TextColor(t.MutedFg)
				}
				ui.Text(c, i18n.T("thinking")).FontSize(11.5).TextColor(t.MutedFg)
				if !open {
					ui.Text(c, oneLine(row.text)).FontSize(10.5).TextColor(t.MutedFg).
						MaxLines(1).Grow(1)
				}
			})
			header.OnClick(func() {
				a.vm.folds[key] = !open
				a.invalidate()
			})
			if open {
				// A rule down the side, the way a quoted passage is marked: the
				// trace is read as background, not as the answer.
				ui.Row(c).FillWidth().Padding(2, 0).Gap(8).AlignItems(ui.Stretch).Children(func() {
					ui.Box(c).Width(2).Radius(1).Background(t.Border)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						markdown.View(c, row.text, nil)
					})
				})
			}
		})
	})
}

// stepsRow folds one run's tool calls into a line the reader can open when they
// want the detail. It shares the answer's column, so the run reads as part of
// the message rather than as a caption under it.
func (a *App) stepsRow(c *ui.Context, t theme.Tokens, row threadRow) {
	key := row.id
	open := a.vm.folds[key]
	ui.Row(c).FillWidth().Padding(6, 0).Gap(9).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Width(avatarSize)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			header := ui.ButtonBase(c).FillWidth().Height(22).Radius(5).Padding(0).
				Justify(ui.Start).Cursor(ui.CursorPointer).Gap(4).AlignItems(ui.Center)
			header.Children(func() {
				if open {
					ui.Icon(c, icons.ChevronDown).Size(10, 10).TextColor(t.MutedFg)
				} else {
					ui.Icon(c, icons.ChevronRight).Size(10, 10).TextColor(t.MutedFg)
				}
				a.stepsStatusIcon(c, t, row.steps)
				ui.Text(c, a.stepsSummary(row)).FontSize(11.5).TextColor(t.MutedFg).MaxLines(1)
			})
			header.OnClick(func() {
				a.vm.folds[key] = !open
				a.invalidate()
			})
			if !open {
				return
			}
			groups := groupSteps(row.steps)
			ui.Column(c).FillWidth().Padding(2, 0, 2, 6).Gap(3).Children(func() {
				// Four calls read at a glance and a table of contents for them is
				// noise. Past that the run opens on what it used, because `bash
				// ×16` says more than the sixteen calls under it do — and the last
				// tool keeps its calls unfolded, since it is the one that just
				// answered the question.
				if len(row.steps) <= stepGroupThreshold {
					a.stepLines(c, t, row.steps)
					return
				}
				for i, group := range groups {
					gkey := key + "/" + group.label
					unfolded, seen := a.vm.groups[gkey]
					if !seen {
						unfolded = i == len(groups)-1
					}
					label := stepGroupLabel(group)
					head := ui.ButtonBase(c).FillWidth().Height(22).Radius(5).Padding(0).
						Justify(ui.Start).Cursor(ui.CursorPointer).Gap(4).AlignItems(ui.Center).Label(label)
					head.Children(func() {
						if unfolded {
							ui.Icon(c, icons.ChevronDown).Size(10, 10).TextColor(t.MutedFg)
						} else {
							ui.Icon(c, icons.ChevronRight).Size(10, 10).TextColor(t.MutedFg)
						}
						a.stepsStatusIcon(c, t, group.steps)
						ui.Text(c, label).FontSize(10.5).FontWeight(500).TextColor(t.MutedFg).MaxLines(1)
					})
					head.OnClick(func() {
						a.vm.groups[gkey] = !unfolded
						a.invalidate()
					})
					if unfolded {
						a.stepLines(c, t, group.steps)
					}
				}
			})
		})
	})
}

// stepGroupThreshold is how many calls a run can have before they are listed by
// tool instead of one per line.
const stepGroupThreshold = 4

// stepGroup is one tool's calls within a run. A turn runs bash a dozen times to
// answer a question that bash itself is not the answer to, so the count is kept
// and the calls are put behind it.
type stepGroup struct {
	label string
	steps []step
}

// groupSteps folds a run's steps by consecutive tool: a stretch of bash is one
// line, and read after it is the next. Consecutive, not by name everywhere —
// the order of the calls is the story of the turn, and sorting them by tool
// into a table of contents loses which came first.
func groupSteps(steps []step) []stepGroup {
	groups := make([]stepGroup, 0, len(steps))
	for _, s := range steps {
		if n := len(groups); n > 0 && groups[n-1].label == s.label {
			groups[n-1].steps = append(groups[n-1].steps, s)
			continue
		}
		groups = append(groups, stepGroup{label: s.label, steps: []step{s}})
	}
	return groups
}

// stepGroupLabel is the tool and how many times it ran: the count is what the
// group is for.
func stepGroupLabel(g stepGroup) string {
	return g.label + " ×" + itoa(len(g.steps))
}

// stepLines draws each call, and under it what came back. The call reads as a
// line with its verdict in front, the way a tool call does in assistant-ui's
// ToolFallback; everything a reader opens the fold for is in the blocks below.
func (a *App) stepLines(c *ui.Context, t theme.Tokens, steps []step) {
	ui.Column(c).FillWidth().Gap(3).Children(func() {
		for _, s := range steps {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Start).Children(func() {
				if !s.thinking {
					a.stepsStatusIcon(c, t, []step{s})
				}
				ui.Text(c, s.label).FontSize(10.5).FontWeight(500).
					TextColor(t.MutedFg).MaxLines(1).MaxWidth(160)
				lines := 1
				if s.thinking {
					lines = 2
				}
				ui.Text(c, s.body).Grow(1).FontSize(10.5).
					TextColor(t.MutedFg).MaxLines(lines)
			})
			a.stepArgs(c, t, s)
			a.stepOutput(c, t, s)
		}
	})
}

// maxArgLines bounds the arguments block. A call's JSON is short by nature; a
// wall of it means a heredoc or a base64 payload, and the result is what was
// asked for anyway.
const maxArgLines = 8

// stepArgs is what the tool was handed, in full. It sits above the result the
// way the request sits above the response, and it is skipped when the header's
// one-line summary already said all of it.
func (a *App) stepArgs(c *ui.Context, t theme.Tokens, s step) {
	args := argsDetail(s.args)
	if args == "" {
		return
	}
	ui.Column(c).FillWidth().Gap(1).Padding(4, 7).Radius(5).Background(t.Muted).Children(func() {
		ui.Text(c, i18n.T("toolArgs")).FontSize(9.5).TextColor(t.MutedFg)
		for _, line := range outputLines(args, maxArgLines) {
			ui.Text(c, line.text).Font("SF Mono, Menlo, monospace").
				FontSize(10).TextColor(t.MutedFg).MaxLines(2)
		}
	})
}

// stepsStatusIcon marks a call (or a folded run of them) without opening it: a
// check when every call answered, an x when one did not, a clock while nothing
// has come back yet. It is what makes a failure visible with the fold shut.
func (a *App) stepsStatusIcon(c *ui.Context, t theme.Tokens, steps []step) {
	failed, answered := 0, 0
	for _, s := range steps {
		if s.thinking {
			continue
		}
		if s.failed {
			failed++
		}
		if s.out != "" {
			answered++
		}
	}
	switch {
	case failed > 0:
		ui.Icon(c, icons.X).Size(10, 10).TextColor(t.Destructive)
	case answered > 0:
		ui.Icon(c, icons.Check).Size(10, 10).TextColor(t.MutedFg)
	}
}

// stepOutput is what the tool gave back, as the thing it is: a diff, a log, a
// stack trace, a document. This is what was being asked for when the fold was
// opened, so it is not summarized again here.
//
// It carries its own copy button: the stack or the JSON a reader wants is one
// paragraph of a whole message, and the message's copy takes all of it with the
// answer attached.
func (a *App) stepOutput(c *ui.Context, t theme.Tokens, step step) {
	if step.out == "" {
		return
	}
	box := ui.Column(c).FillWidth().Gap(1).Padding(5, 7).Radius(5).Background(t.Muted)
	hovered := box.Hovered()
	box.Children(func() {
		// Held at the corner rather than in the flow: a copy button that takes a
		// line of its own would be bigger than most outputs here.
		copyBtn := ui.ButtonBase(c).Absolute().Top(3).Right(4).Width(18).Height(18).
			Radius(4).Cursor(ui.CursorPointer).Tooltip(i18n.T("copy")).
			Label(i18n.T("copy") + " " + step.label)
		if hovered {
			copyBtn = copyBtn.Opacity(1)
		} else {
			copyBtn = copyBtn.Opacity(0.4)
		}
		copyBtn.OnClick(func() { a.copyText(c, step.out) })
		copyBtn.Children(func() {
			ui.Icon(c, icons.Copy).Size(10.5, 10.5).TextColor(t.MutedFg)
		})
		// The right padding is the button's column, so a long line ends before
		// the icon rather than under it.
		for _, line := range outputLines(step.out, maxOutputLines) {
			ui.Text(c, line.text).Font("SF Mono, Menlo, monospace").
				FontSize(10).TextColor(outputColor(t, line.kind)).MaxLines(2).
				Padding(0, 22, 0, 0)
		}
	})
}

// stepsSummary is the folded line: how many steps, how many failed, and how
// long they took when the entries carry timestamps that say.
func (a *App) stepsSummary(row threadRow) string {
	failed := stepFailures(row.steps)
	var summary string
	if failed > 0 {
		summary = i18n.TF("stepsFailed", map[string]any{"n": len(row.steps), "f": failed})
	} else {
		key := "steps"
		if len(row.steps) == 1 {
			key = "stepsOne"
		}
		summary = i18n.TF(key, map[string]any{"n": len(row.steps)})
	}
	if !row.noSpan {
		summary += " · " + shortDuration(row.span)
	}
	return summary
}

// stepFailures counts the calls that came back as errors.
func stepFailures(steps []step) int {
	n := 0
	for _, s := range steps {
		if s.failed {
			n++
		}
	}
	return n
}

// outputColor says what a line of tool output is by its color: a removal is
// red, an addition green, a warning amber, a failure red and bold-ish by being
// the only red at full strength.
func outputColor(t theme.Tokens, kind outKind) ui.Color {
	switch kind {
	case outAdd:
		return t.Success
	case outDel, outError:
		return t.Destructive
	case outWarn:
		return t.Warning
	case outSkip:
		return t.MutedFg
	}
	return t.Foreground
}

func (a *App) errorRow(c *ui.Context, t theme.Tokens, row threadRow) {
	ui.Row(c).FillWidth().Padding(4, 0).Gap(8).AlignItems(ui.Stretch).Children(func() {
		ui.Box(c).Width(2).AlignSelf(ui.Stretch).Radius(1).Background(t.Destructive)
		ui.Row(c).Grow(1).Padding(2, 0).Gap(5).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, icons.AlertTriangle).Size(11, 11).TextColor(t.Destructive)
			ui.Text(c, row.text).FontSize(11.5).TextColor(t.Destructive)
			// The failure usually ends a turn the reader still wants: one click
			// sends the same words again instead of making them retype.
			retry := ui.ButtonBase(c).Radius(5).Padding(1, 6).Cursor(ui.CursorPointer).
				Tooltip(i18n.T("retry"))
			retry.Children(func() {
				ui.Text(c, i18n.T("retry")).FontSize(11).TextColor(t.Primary)
			})
			if retry.Clicked() {
				a.retryTurn(row.turn)
			}
		})
	})
}

// liveRowView is the turn in flight: the tool it is running, and the answer as
// it is written. Without it a turn that thinks for a minute looks stalled.
func (a *App) liveRowView(c *ui.Context, t theme.Tokens, row threadRow) {
	ui.Row(c).FillWidth().Padding(6, 0).Gap(9).AlignItems(ui.Start).Children(func() {
		a.avatarView(c, avatarSize)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			if row.liveThinking != "" && row.text == "" {
				ui.Text(c, i18n.T("thinkingRunning")+" "+lastLineText(row.liveThinking)).
					FontSize(11).TextColor(t.MutedFg).MaxLines(2)
			}
			if row.liveStep != "" {
				ui.Text(c, i18n.TF("runningStep", map[string]any{"name": liveStepName(row.liveStep, row.liveHint)})).
					FontSize(11).TextColor(t.MutedFg)
			}
			if row.text != "" {
				// View, not Stream: the bytes already arrive in chunks, and a
				// typewriter on top of that is animation for its own sake.
				markdown.View(c, row.text, nil)
			}
		})
	})
}

// liveRow is the turn in flight, read from pi.live: what it is running and what
// it has said so far. Entries land when a turn commits, so nothing else would
// reach the page while one is thinking.
func (a *App) liveRow() (threadRow, bool) {
	th, ok := a.threads[a.active]
	if !ok || !a.running() {
		return threadRow{}, false
	}
	row := threadRow{kind: rowLive, id: "live"}
	var generation struct {
		Message struct {
			Content []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				Thinking string `json:"thinking"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(th.live.Generation, &generation) == nil {
		var said, thought []string
		for _, part := range generation.Message.Content {
			if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
				said = append(said, part.Text)
			}
			if part.Type == "thinking" && strings.TrimSpace(part.Thinking) != "" {
				thought = append(thought, part.Thinking)
			}
		}
		row.text = strings.Join(said, "")
		row.liveThinking = strings.Join(thought, "\n\n")
	}
	// The last slot that is still running is the one to name; a round queues
	// its calls up front, so the earlier ones read pending or done.
	for _, raw := range th.live.Tools {
		var slot struct {
			Name    string          `json:"name"`
			Status  string          `json:"status"`
			Output  string          `json:"output"`
			Details json.RawMessage `json:"details"`
		}
		if json.Unmarshal(raw, &slot) == nil && slot.Status == "running" && slot.Name != "" {
			row.liveStep = slot.Name
			row.liveHint = liveStepHint(slot.Output, slot.Details)
		}
	}
	if row.text == "" && row.liveStep == "" && row.liveThinking == "" {
		return threadRow{}, false
	}
	return row, true
}

// liveStepName is the tool's own name plus what it is doing, kept to one line.
// The tool keeps its name untranslated; the hint is what tells two `bash` calls
// apart.
func liveStepName(name, hint string) string {
	if hint == "" {
		return name
	}
	return name + " · " + hint
}

// liveStepHint reads what the running call is up to: the command or path it
// reports, else the tail of the output it has streamed so far. Nothing is
// guessed when both are empty, so a bare tool name stays a bare tool name.
func liveStepHint(output string, details json.RawMessage) string {
	if len(details) > 0 {
		var object map[string]any
		if json.Unmarshal(details, &object) == nil {
			for _, key := range []string{"command", "path", "file", "query", "url"} {
				if text, ok := object[key].(string); ok {
					if hint := oneLine(text); hint != "" {
						return clip(hint, liveHintRunes)
					}
				}
			}
		}
	}
	// Output streams a chunk at a time; the last line drawn is the newest.
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if hint := oneLine(lines[i]); hint != "" {
			return clip(hint, liveHintRunes)
		}
	}
	return ""
}

// liveHintRunes is the room a hint gets on the row before it would crowd out
// the answer being written below it.
const liveHintRunes = 60

// images draws thumbnails; a pick that will not decode is skipped rather than
// leaving a hole.
func (a *App) images(c *ui.Context, images []imageRef, w, h float32) {
	if len(images) == 0 {
		return
	}
	ui.Row(c).FillWidth().Gap(6).Children(func() {
		// By index, so the decoded bitmap is stored on the row's own attachment
		// and not on a copy of it.
		for i := range images {
			bitmap := images[i].bitmap()
			if bitmap == nil {
				continue
			}
			// The thumbnail is also the way in: a screenshot at 180 points is a
			// placeholder for the picture, not the picture.
			picture := images[i]
			shot := ui.ButtonBase(c).Size(w, h).Radius(8).Cursor(ui.CursorPointer).
				Tooltip(i18n.T("openImage"))
			shot.Children(func() {
				ui.Image(c, bitmap).Size(w, h).Radius(8).Clip()
			})
			shot.OnClick(func() { a.zoomImage(picture) })
		}
	})
}

// bitmap decodes an attachment once and keeps it for the frames after.
func (img *imageRef) bitmap() *ui.Bitmap {
	if img.decoded {
		return img.cache
	}
	img.decoded = true
	bitmap, err := ui.DecodeBitmapScaled(img.data, 480)
	if err != nil {
		return nil
	}
	img.cache = bitmap
	return bitmap
}
