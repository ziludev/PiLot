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
	a.vm.threadList.FollowEnd = true
	a.vm.threadList.Key = func(row int) any {
		if row < 0 || row >= len(rows) {
			return row
		}
		return rows[row].id
	}
	ui.Row(c).Grow(1).FillWidth().Justify(ui.Center).Children(func() {
		ui.Column(c).Grow(1).FillHeight().MaxWidth(maxContent).Children(func() {
			ui.List(c, &a.vm.threadList, len(rows), func(row int) {
				a.threadRow(c, t, rows[row])
			}).Grow(1).Padding(8, 0, 12, 0)
		})
	})
}

func (a *App) threadRow(c *ui.Context, t theme.Tokens, row threadRow) {
	switch row.kind {
	case rowUser:
		a.userRow(c, t, row)
	case rowAssistant:
		a.assistantRow(c, t, row)
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
	ui.Row(c).FillWidth().Justify(ui.End).Padding(6, 0).Gap(9).AlignItems(ui.Start).Children(func() {
		bubble := ui.Column(c).MaxWidth(560).Padding(8, 12).Radius(12).
			Background(t.Secondary).Gap(6)
		bubble.Children(func() {
			a.images(c, row.images, 180, 120)
			if row.text != "" {
				ui.Text(c, row.text).FontSize(12.5).TextColor(t.SecondFg)
			}
		})
		a.userAvatarView(c, t, avatarSize)
	})
}

// assistantRow is full width and has no bubble: the answer belongs to the page,
// not to a card.
func (a *App) assistantRow(c *ui.Context, t theme.Tokens, row threadRow) {
	ui.Row(c).FillWidth().Padding(6, 0).Gap(9).AlignItems(ui.Start).Children(func() {
		a.avatarView(c, avatarSize)
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			a.images(c, row.images, 240, 160)
			if row.text != "" {
				markdown.View(c, row.text, nil)
			}
		})
	})
}

// stepsRow folds one run's thinking and tool calls into a line the reader can
// open when they want the detail.
func (a *App) stepsRow(c *ui.Context, t theme.Tokens, row threadRow) {
	key := row.id
	open := a.vm.folds[key]
	header := ui.ButtonBase(c).FillWidth().Height(24).Radius(5).Padding(0, 4).
		Cursor(ui.CursorPointer).Gap(4).AlignItems(ui.Center)
	header.Children(func() {
		if open {
			ui.Icon(c, icons.ChevronDown).Size(11, 11).TextColor(t.MutedFg)
		} else {
			ui.Icon(c, icons.ChevronRight).Size(11, 11).TextColor(t.MutedFg)
		}
		ui.Text(c, a.stepsSummary(row)).FontSize(12).TextColor(t.MutedFg).MaxLines(1)
	})
	header.OnClick(func() {
		a.vm.folds[key] = !open
		a.invalidate()
	})
	if !open {
		return
	}
	ui.Column(c).FillWidth().Padding(0, 0, 2, 18).Gap(3).Children(func() {
		for _, step := range row.steps {
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Start).Children(func() {
				ui.Text(c, step.label).FontSize(10.5).FontWeight(500).
					TextColor(t.MutedFg).MaxLines(1).MaxWidth(160)
				ui.Text(c, step.body).Grow(1).FontSize(10.5).
					TextColor(t.MutedFg).MaxLines(2)
			})
		}
	})
}

// stepsSummary is the folded line: how many steps, and how long they took when
// the entries carry timestamps that say.
func (a *App) stepsSummary(row threadRow) string {
	key := "steps"
	if len(row.steps) == 1 {
		key = "stepsOne"
	}
	summary := i18n.TF(key, map[string]any{"n": len(row.steps)})
	if !row.noSpan {
		summary += " · " + shortDuration(row.span)
	}
	return summary
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
			if row.liveStep != "" {
				ui.Text(c, i18n.TF("runningStep", map[string]any{"name": row.liveStep})).
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
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(th.live.Generation, &generation) == nil {
		var said []string
		for _, part := range generation.Message.Content {
			if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
				said = append(said, part.Text)
			}
		}
		row.text = strings.Join(said, "")
	}
	// The last slot that is still running is the one to name; a round queues
	// its calls up front, so the earlier ones read pending or done.
	for _, raw := range th.live.Tools {
		var slot struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		}
		if json.Unmarshal(raw, &slot) == nil && slot.Status == "running" && slot.Name != "" {
			row.liveStep = slot.Name
		}
	}
	if row.text == "" && row.liveStep == "" {
		return threadRow{}, false
	}
	return row, true
}

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
			ui.Image(c, bitmap).Size(w, h).Radius(8).Clip()
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
