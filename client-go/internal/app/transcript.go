package app

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"pilot/internal/i18n"
	"pilot/internal/icons"
	"pilot/internal/theme"
)

// maxContent is how wide the transcript reads: full window width is a line no
// eye can follow, so the column is capped and centred.
// maxContent is the measure the answer reads at. At 13px with a 1.62 line this
// is about 70 characters, which is where prose stops being a scan and starts
// being a read — Beautiful UI keeps its own prose well inside 460px of type.
const maxContent = 680

// transcriptMarkdown is how a message's Markdown is drawn. Code wraps rather
// than scrolling sideways: a log line or a long URL is read to its end or not
// at all, and a sideways scroller inside a message shows no bar at rest, so
// the tail of the line simply vanished.
//
// The palette is the app's own rather than mygo's default: a fenced block that
// comes back in a generic grey sits on the card like a patch from another
// program. One Options per theme, cached, because markdown.View rebuilds its
// state whenever the pointer changes and the pointer must only change when the
// colours do.
func (a *App) transcriptMarkdown(t theme.Tokens) *markdown.Options {
	if a.md.opts != nil && a.md.tokens == t {
		return a.md.opts
	}
	a.md.tokens = t
	a.md.opts = &markdown.Options{
		WrapCode: true,
		// 13 on a 1.62 line: Streaming Text's own measure, which is the only
		// place the library sets an answer's prose.
		Theme: &markdown.Theme{
			FontSize:   theme.TextBody,
			LineHeight: 1.62,
			CodeFont:   monoFont,
			// An em multiple, not DIPs: 0.875 is mygo's own default and what a
			// code span should be against prose.
			CodeSize:            0.84,
			CodeLineHeight:      1.5,
			Text:                t.Ink,
			Muted:               t.Ink3,
			Link:                t.Brand,
			Strong:              t.Ink,
			Code:                t.Ink,
			CodeBackground:      t.Field,
			CodeBlockBackground: t.Inset,
			CodeBlockBorder:     t.LineSoft,
			CodeBlockRadius:     theme.RadiusControl,
			QuoteBar:            t.Line,
			QuoteText:           t.Ink2,
			Rule:                t.Line,
			CodeColors:          codeTheme(t),
		},
	}
	return a.md.opts
}

// codeTheme is the syntax palette of a fenced block: the app's own colours
// rather than GitHub's, so a block does not arrive in red and purple on a page
// of blue and grey. Kinds the palette has nothing to say about keep their
// default weight, which is why the base is a fresh Theme and not DefaultTheme.
func codeTheme(t theme.Tokens) *editor.Theme {
	ct := &editor.Theme{Background: t.Inset, Text: t.Ink}
	set := func(kind editor.TokenKind, color ui.Color) {
		ct.Tokens[kind] = editor.TokenStyle{Color: color}
	}
	set(editor.TokenKeyword, t.Brand)
	set(editor.TokenOperator, t.Ink2)
	set(editor.TokenType, t.Orange)
	set(editor.TokenFunction, t.Ink)
	set(editor.TokenMeta, t.Orange)
	set(editor.TokenProperty, t.Ink)
	set(editor.TokenConstant, t.Orange)
	set(editor.TokenNumber, t.Orange)
	set(editor.TokenAttribute, t.Orange)
	set(editor.TokenString, t.Green)
	set(editor.TokenEscape, t.Orange)
	set(editor.TokenRegexp, t.Green)
	set(editor.TokenTag, t.Brand)
	set(editor.TokenComment, t.Ink3)
	set(editor.TokenHeading, t.Ink)
	set(editor.TokenLink, t.Brand)
	set(editor.TokenCode, t.Ink)
	set(editor.TokenInserted, t.Green)
	set(editor.TokenDeleted, t.Red)
	set(editor.TokenInvalid, t.Red)
	ct.Tokens[editor.TokenComment].Italic = true
	ct.Tokens[editor.TokenHeading].Bold = true
	return ct
}

// mdCache is the last Markdown options built for a theme, with the tokens they
// were built from.
type mdCache struct {
	tokens theme.Tokens
	opts   *markdown.Options
}

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
			ui.Text(c, i18n.T("loadFailed")).FontSize(theme.TextRow).TextColor(t.Red)
			retry := ui.ButtonBase(c).Radius(theme.RadiusChip).Padding(2, 8).Cursor(ui.CursorPointer)
			retry.Children(func() {
				ui.Text(c, i18n.T("retry")).FontSize(theme.TextSmall).FontWeight(500).TextColor(t.Brand)
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
	// A chat longer than one page opens on its newest page; walking up to the
	// top reads the page behind it, so the whole chat is reachable by
	// scrolling and nobody presses a button to read their own messages.
	a.olderLoader(th)
	if len(rows) == 0 {
		ui.Row(c).Grow(1).FillWidth().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("emptyThread")).FontSize(theme.TextRow).TextColor(t.Ink3)
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
	// The scroll bar belongs to the pane's edge, not to the reading column: a
	// capped scroll container leaves the bar floating in the middle of a wide
	// window. So the list spans the pane and every row centres its own capped
	// column inside it — same measure, bar where the wheel is expected.
	ui.Row(c).Grow(1).FillWidth().Children(func() {
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			if th.loadingOlder && !th.prefetching {
				centeredRow(c, func() { a.olderLine(c, t) })
			}
			ui.List(c, &a.vm.threadList, len(rows), func(row int) {
				centeredRow(c, func() {
					if row != divider {
						a.threadRowMarked(c, t, rows[row], a.findMark(row))
						return
					}
					a.newLine(c, t)
					a.threadRowMarked(c, t, rows[row], a.findMark(row))
				})
			}).Grow(1).Padding(8, 0, 12, 0)
			a.jumpToLatest(c, t, len(rows))
		})
	})
}

// centeredRow is one transcript row inside the reading column: the list spans
// the whole pane so its scroll bar sits against the pane's edge, and each row
// puts the capped column back in the middle. The gutter rides inside the cap,
// so a pane narrower than the column keeps its breathing room instead of
// running text up against the rail's divider and the window's edge.
func centeredRow(c *ui.Context, draw func()) {
	ui.Row(c).FillWidth().Justify(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MaxWidth(maxContent+2*gutter).Padding(0, gutter).Children(draw)
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
		ui.Text(c, i18n.T("jumpLatest")).FontSize(theme.TextMicro).TextColor(t.Foreground).MaxLines(1)
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
	ui.Column(c).FillWidth().Radius(theme.RadiusControl).Background(t.Selection).Children(func() {
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
	case rowDay:
		a.dayRow(c, t, row)
	}
}

// dayRow is the rule between two days of a conversation: a hairline either
// side of the date, so the eye reads a break rather than a heading.
func (a *App) dayRow(c *ui.Context, t theme.Tokens, row threadRow) {
	ui.Row(c).FillWidth().Padding(12, 0).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Height(1).Grow(1).Background(t.Line)
		ui.Text(c, row.text).FontSize(theme.TextMicro).FontWeight(500).TextColor(t.Ink3)
		ui.Box(c).Height(1).Grow(1).Background(t.Line)
	})
}

// userRow is the right-aligned bubble: the user's own words, tinted so they are
// not mistaken for the agent's.
func (a *App) userRow(c *ui.Context, t theme.Tokens, row threadRow) {
	outer := ui.Row(c).FillWidth().Justify(ui.End).Padding(6, 0).Gap(9).AlignItems(ui.Start)
	hovered := outer.Hovered()
	outer.Children(func() {
		ui.Column(c).MaxWidth(560).Gap(4).Children(func() {
			// The user's turn is a filled bubble in the same recessed tone as an
			// input: a message about to be sent, not a card of its own.
			bubble := ui.Column(c).Padding(9, 14).Radius(theme.Radius).
				Background(t.Field).Gap(6)
			bubble.Children(func() {
				a.images(c, row.images, 180, 120)
				if row.text != "" {
					// The person's own words get the same markdown as an answer:
					// a quoted bot reply pasted into the box carried its ** and
					// backticks as punctuation otherwise, and a long one pushed
					// the reply it was answering off the screen.
					a.answerBody(c, t, row, t.Field)
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
		if row.lead {
			a.avatarView(c, avatarSize)
		} else {
			ui.Box(c).Width(avatarSize)
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			a.images(c, row.images, 240, 160)
			if row.text != "" {
				a.answerBody(c, t, row, t.Surface)
			}
			a.messageActions(c, t, row, hovered, false)
		})
	})
}

// longAnswerChars is where an answer stops being read and starts being scrolled:
// about a screenful at 13 points in a 680-point measure.
const longAnswerChars = 900

// longAnswerHeight is the preview's height: fourteen lines and a little air, so
// the cut lands inside a paragraph rather than after a heading.
const longAnswerHeight = 300

// answerBody is the model's answer, with the length of it behind a fold. A 2000
// character status report pushes everything after it off the screen, and the
// person reading a chat wants the newest exchange first, not one wall of prose.
func (a *App) answerBody(c *ui.Context, t theme.Tokens, row threadRow, behind ui.Color) {
	if len([]rune(row.text)) <= longAnswerChars {
		markdown.View(c, row.text, a.transcriptMarkdown(t))
		return
	}
	open := a.vm.longOpen[row.id]
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		if open {
			markdown.View(c, row.text, a.transcriptMarkdown(t))
		} else {
			preview := ui.Column(c).FillWidth().MaxHeight(longAnswerHeight).ClipY()
			preview.Children(func() {
				markdown.View(c, row.text, a.transcriptMarkdown(t))
			})
			// The clip lands inside a line whatever height is chosen, and half a
			// glyph reads as a strikethrough. A scrim over the last few points
			// fades it out instead, the way a fold is meant to look.
			ui.Box(c).FillWidth().Height(14).Margin(-14, 0, 0, 0).
				Gradient(behind.Alpha(0), behind, 180)
		}
		fold := ui.ButtonBase(c).Width(96).Height(20).Padding(0, 4).Radius(theme.RadiusChip).
			Justify(ui.Start).AlignItems(ui.Center).Cursor(ui.CursorPointer)
		if fold.Hovered() {
			fold = fold.Background(t.Field)
		}
		fold.Children(func() {
			label := i18n.T("answerMore")
			if open {
				label = i18n.T("answerLess")
			}
			ui.Text(c, label).FontSize(theme.TextSmall).FontWeight(500).TextColor(t.Ink2).
				MaxLines(1)
		})
		fold.OnClick(func() {
			a.vm.longOpen[row.id] = !open
			a.invalidate()
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
	open, seen := a.vm.folds[key]
	if !seen {
		open = row.newest
	}
	// In the answer's own column: a thought belongs to the message it led to,
	// not to the middle of the page.
	ui.Row(c).FillWidth().Padding(6, 0).Gap(9).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Width(avatarSize)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			// ThinkingState's header: a 16px sparkle, the label at 13 medium in
			// ink-2, and a chevron that turns over when the trace opens.
			header := ui.ButtonBase(c).FillWidth().Height(26).Radius(theme.RadiusChip).Padding(0).
				Justify(ui.Start).Cursor(ui.CursorPointer).Gap(5).AlignItems(ui.Center).
				Label(i18n.T("thinking"))
			header.Children(func() {
				if open {
					ui.Icon(c, icons.ChevronDown).Size(14, 14).TextColor(t.Ink3)
				} else {
					ui.Icon(c, icons.ChevronRight).Size(14, 14).TextColor(t.Ink3)
				}
				sparkle := t.Ink3
				if row.newest && a.running() {
					// ThinkingState fills the glyph while the thought is being had
					// and leaves it hollow once the answer is out.
					sparkle = t.Ink2
				}
				ui.Icon(c, icons.Sparkle).Size(16, 16).TextColor(sparkle)
				ui.Text(c, i18n.T("thinking")).FontSize(theme.TextBody).FontWeight(500).TextColor(t.Ink2)
				if !open {
					ui.Text(c, oneLine(row.text)).FontSize(theme.TextRow).TextColor(t.Ink3).
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
				ui.Row(c).FillWidth().Padding(4, 0, 2, 8).Gap(9).AlignItems(ui.Stretch).Children(func() {
					ui.Box(c).Width(2).Radius(1).Background(t.Line)
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						markdown.View(c, row.text, a.transcriptMarkdown(t))
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
	open, seen := a.vm.folds[key]
	if !seen {
		open = row.newest
	}
	ui.Row(c).FillWidth().Padding(6, 0).Gap(9).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Width(avatarSize)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			header := ui.ButtonBase(c).FillWidth().Height(26).Radius(theme.RadiusChip).Padding(0).
				Justify(ui.Start).Cursor(ui.CursorPointer).Gap(5).AlignItems(ui.Center)
			header.Children(func() {
				if open {
					ui.Icon(c, icons.ChevronDown).Size(12, 12).TextColor(t.Ink3)
				} else {
					ui.Icon(c, icons.ChevronRight).Size(12, 12).TextColor(t.Ink3)
				}
				a.stepsStatusIcon(c, t, row.steps)
				ui.Text(c, a.stepsSummary(row)).FontSize(theme.TextBody).FontWeight(500).
					TextColor(t.Ink2).MaxLines(1)
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
					head := ui.ButtonBase(c).FillWidth().Height(26).Radius(theme.RadiusChip).Padding(0).
						Justify(ui.Start).Cursor(ui.CursorPointer).Gap(5).AlignItems(ui.Center).Label(label)
					head.Children(func() {
						if unfolded {
							ui.Icon(c, icons.ChevronDown).Size(12, 12).TextColor(t.Ink3)
						} else {
							ui.Icon(c, icons.ChevronRight).Size(12, 12).TextColor(t.Ink3)
						}
						a.stepsStatusIcon(c, t, group.steps)
						ui.Text(c, label).FontSize(theme.TextSmall).FontWeight(500).
							TextColor(t.Ink2).MaxLines(1)
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
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		for _, s := range steps {
			ui.Column(c).FillWidth().Gap(3).Children(func() {
				ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
					if !s.thinking {
						a.stepsStatusIcon(c, t, []step{s})
					}
					// Steps' own row: the tool at 12.5 medium in ink, what it was
					// handed at 11.5 in ink-3 — primary and secondary, no chip.
					ui.Text(c, s.label).FontSize(theme.TextRow).FontWeight(500).
						TextColor(t.Ink).MaxLines(1)
					lines := 1
					if s.thinking {
						lines = 2
					}
					ui.Text(c, s.body).Grow(1).FontSize(theme.TextSmall).
						TextColor(t.Ink3).MaxLines(lines)
				})
				a.stepArgs(c, t, s)
				a.stepOutput(c, t, s)
			})
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
	ui.Column(c).FillWidth().Gap(2).Padding(6, 8).Radius(theme.RadiusChip).Background(t.Inset).
		Border(1, t.LineSoft).Children(func() {
		ui.Text(c, i18n.T("toolArgs")).FontSize(theme.TextMicro).TextColor(t.Ink3)
		for _, line := range outputLines(args, maxArgLines) {
			ui.Text(c, line.text).Font("SF Mono, Menlo, monospace").
				FontSize(theme.TextSmall).TextColor(t.Ink2).MaxLines(2)
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
	// ThinkingState's Steps trace: a 14px tick for what came back, a red cross
	// for what failed, nothing at all while it is still running.
	switch {
	case failed > 0:
		ui.Icon(c, icons.X).Size(14, 14).TextColor(t.Red)
	case answered > 0:
		ui.Icon(c, icons.Check).Size(14, 14).TextColor(t.Ink3)
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
	box := ui.Column(c).FillWidth().Gap(2).Padding(6, 8).Radius(theme.RadiusChip).
		Background(t.Inset).Border(1, t.LineSoft)
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
		lines := outputLines(step.out, maxOutputLines)
		if file, add, del, ok := diffSummary(step.out); ok {
			// A patch gets Diff Table's bar: the file on the left, the counts on
			// the right, a hairline under it. Without it a patch is just lines
			// that happen to start with + and -, and the reader has to count.
			ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Padding(0, 22, 0, 0).Children(func() {
				name := file
				if name == "" {
					name = step.label
				}
				ui.Text(c, clip(name, 64)).Grow(1).Shrink(1).MinWidth(0).
					FontSize(theme.TextRow).FontWeight(500).TextColor(t.Ink).MaxLines(1)
				ui.Text(c, "+"+itoa(add)).Font("SF Mono, Menlo, monospace").
					FontSize(theme.TextMicro).TextColor(t.Green)
				ui.Text(c, "−"+itoa(del)).Font("SF Mono, Menlo, monospace").
					FontSize(theme.TextMicro).TextColor(t.Red)
			})
			ui.Box(c).FillWidth().Height(1).Background(t.Line)
		}
		// The right padding is the button's column, so a long line ends before
		// the icon rather than under it.
		for _, line := range lines {
			ui.Text(c, line.text).Font("SF Mono, Menlo, monospace").
				FontSize(theme.TextSmall).TextColor(outputColor(t, line.kind)).MaxLines(2).
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
			ui.Text(c, row.text).FontSize(theme.TextSmall).TextColor(t.Destructive)
			// The failure usually ends a turn the reader still wants: one click
			// sends the same words again instead of making them retype.
			retry := ui.ButtonBase(c).Radius(theme.RadiusChip).Padding(1, 6).Cursor(ui.CursorPointer).
				Tooltip(i18n.T("retry"))
			retry.Children(func() {
				ui.Text(c, i18n.T("retry")).FontSize(theme.TextSmall).TextColor(t.Primary)
			})
			if retry.Clicked() {
				a.retryTurn(row.turn)
			}
		})
	})
}

// liveRowView is the turn in flight: the tool it is running, and the answer as
// it is written. Without it a turn that thinks for a minute looks stalled.
// loaderGrid is LoadingState's loader: nine 4px cells in a 3x3 grid, lit in a
// chevron sweep — the corner distance of each cell sets when it fires, so the
// light runs down the middle and out to the edges. Delays are the library's own
// ((column + |row - 1|) * 90ms) over a 650ms round; the view asks for another
// frame while it is on screen, which is only while a turn is running.
func (a *App) loaderGrid(c *ui.Context, t theme.Tokens) {
	const cell, gap = float32(4), float32(1.5)
	const round = float32(650)
	phase := float32(c.Now().UnixMilli()%int64(round)) / round
	ui.Column(c).Gap(gap).Children(func() {
		for row := 0; row < 3; row++ {
			ui.Row(c).Gap(gap).Children(func() {
				for col := 0; col < 3; col++ {
					// Each cell runs its own 650ms loop, offset by its delay.
					shift := float32(col+absInt(row-1)) * 90 / round
					local := phase - shift
					for local < 0 {
						local++
					}
					// A tent: dark at both ends of the round, full in the middle.
					lit := float32(1) - absFloat(2*local-1)
					ui.Box(c).Size(cell, cell).Radius(1).
						Background(t.Ink.Alpha(0.15 + 0.85*lit))
				}
			})
		}
	})
	c.After(60 * time.Millisecond)
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func absFloat(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// streamCaret is where the next word will land while an answer arrives. A
// block glyph rather than an animated box: at 13px a terminal-style bar reads as
// a caret, and markdown.View already owns the line it has to sit at the end of.
const streamCaret = "\u258c"

func (a *App) liveRowView(c *ui.Context, t theme.Tokens, row threadRow) {
	ui.Row(c).FillWidth().Padding(6, 0).Gap(9).AlignItems(ui.Start).Children(func() {
		a.avatarView(c, avatarSize)
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			// Loading State is the shape of "nothing has come back yet": the 3x3
			// pixel grid sweeping in a chevron, the label beside it, and the
			// elapsed clock in tabular figures. The thought it is chasing goes on
			// the next line rather than into the label.
			if row.text == "" && row.liveStep == "" {
				ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(7).Children(func() {
					a.loaderGrid(c, t)
					ui.Text(c, i18n.T("thinkingRunning")).FontSize(theme.TextBody).
						FontWeight(500).TextColor(t.Ink2).MaxLines(1)
					// The clock is the status strip's job: one running turn, one
					// place that says how long it has been going.

				})
			}
			if row.liveThinking != "" && row.text == "" {
				ui.Text(c, lastLineText(row.liveThinking)).
					FontSize(theme.TextSmall).TextColor(t.Ink3).MaxLines(2)
			}
			if row.liveStep != "" {
				ui.Text(c, i18n.TF("runningStep", map[string]any{"name": liveStepName(row.liveStep, row.liveHint)})).
					FontSize(theme.TextSmall).TextColor(t.MutedFg)
			}
			if row.text != "" {
				// View, not Stream: the bytes already arrive in chunks, and a
				// typewriter on top of that is animation for its own sake. The
				// caret is the one thing Streaming Text adds — a bar where the
				// next word will land, so the answer reads as still arriving.
				markdown.View(c, row.text+streamCaret, a.transcriptMarkdown(t))
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
	// A turn that has said nothing yet is still a row: it is where Loading
	// State's loader goes. Only the absence of a run hides it, and that was
	// checked above.
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
			shot := ui.ButtonBase(c).Size(w, h).Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
				Tooltip(i18n.T("openImage"))
			shot.Children(func() {
				ui.Image(c, bitmap).Size(w, h).Radius(theme.RadiusControl).Clip()
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

// olderLoader starts reading the page behind the first one when the reader has
// walked up to it. Following the end means they are at the bottom, so the check
// waits until they are not: opening a chat must not fetch two pages for one.
func (a *App) olderLoader(th *threadStore) {
	if th == nil || !th.hasMore || th.loadingOlder {
		return
	}
	first, last := a.vm.threadList.Visible()
	// A screen early: the page is read while the reader is still moving
	// towards it, so it lands like the next message rather than like a load.
	if last < 0 || first > 10 {
		// Not laid out yet, or the top of this page is not on screen.
		return
	}
	if a.vm.threadList.AtEnd() {
		// Still reading the end of the chat: the page behind this one can
		// wait until the reader walks up to it.
		return
	}
	if !th.olderTried.IsZero() && time.Since(th.olderTried) < 2*time.Second {
		return
	}
	th.olderTried = time.Now()
	a.loadOlder(th.convID)
}

// olderLine says a page is being read, at the top of where it will land, so a
// slow bridge looks like reading rather than like a chat that ends there.
func (a *App) olderLine(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Padding(8, 0).Justify(ui.Center).Children(func() {
		ui.Text(c, i18n.T("loadingOlder")).FontSize(theme.TextMicro).TextColor(t.MutedFg)
	})
}
