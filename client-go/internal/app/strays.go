package app

// 遗留工作区: the checkouts a project's sessions left in .worktrees/.
//
// Nothing deletes a worktree while it holds unmerged work, so a reset, a lost
// chat or a botched migration leaves the work sitting on disk with no window
// showing it. The section answers the only two questions worth asking about such
// a directory — whose branch is it, and is the work already merged — and offers
// the one move that costs nothing: a new chat working in that same directory.

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
	"pilot/internal/icons"
	"pilot/internal/protocol"
	"pilot/internal/theme"
)

// straysProject is the project whose leftovers are shown: the one the open chat
// lives in. A chat with no project lands in the bridge's built-in workspace, and
// an empty id is how the endpoint asks for that.
func (a *App) straysProject() string {
	if surface := a.surfaceOf(a.active); surface != nil {
		return surface.ProjectID
	}
	return ""
}

// loadStrays reads them. The drawer asks on open and on the refresh button;
// nothing polls, because a leftover checkout does not appear on its own.
func (a *App) loadStrays() {
	project := a.straysProject()
	seq := a.projects.straysSeq + 1
	a.projects.straysSeq = seq
	a.projects.straysFor = project
	a.projects.straysBusy = true
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		rows, err := a.api.Worktrees(ctx, project)
		a.post(event{kind: evStrays, seq: seq, text: project, worktrees: rows, err: err})
	}()
}

func (a *App) applyStrays(seq int, project string, rows []protocol.Worktree, err error) {
	if seq != a.projects.straysSeq || project != a.projects.straysFor {
		return
	}
	a.projects.straysBusy = false
	if err != nil {
		a.projects.straysFail = err.Error()
		a.invalidate()
		return
	}
	a.projects.straysFail = ""
	a.projects.strays = rows
	a.invalidate()
}

// adoptStray opens a checkout as a chat. The directory and the branch stay where
// they are — the point is the work, not a new copy of it.
func (a *App) adoptStray(row protocol.Worktree) {
	if a.projects.adopting != "" {
		return
	}
	project := a.projects.straysFor
	a.projects.adopting = row.Dir
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 40*time.Second)
		defer cancel()
		out, err := a.api.AdoptWorktree(ctx, project, row.Dir)
		a.post(event{kind: evAdopt, path: row.Dir, convID: out.ConversationID, err: err})
	}()
}

func (a *App) applyAdopt(dir string, convID int, err error) {
	a.projects.adopting = ""
	if err != nil {
		a.projects.failed = err.Error()
		a.invalidate()
		return
	}
	a.projects.failed = ""
	// The new chat has no surface until the bridge's state is read again, so the
	// window learns about it the same way it learns about any other: a refresh,
	// then the selection it was asked to make.
	a.requestRefresh()
	a.post(event{kind: evSelect, convID: convID})
	a.loadStrays()
	a.invalidate()
}

func (a *App) straysSection(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Grow(1).Padding(0, 6).Gap(4).Children(func() {
		ui.Row(c).Height(26).Padding(0, 4).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("straysTitle")).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Ink3)
			if n := len(a.projects.strays); n > 0 {
				ui.Text(c, itoa(n)).FontSize(theme.TextMicro).TextColor(t.Ink3)
			}
			ui.Spacer(c)
			reload := ui.ButtonBase(c).Size(20, 20).Radius(theme.RadiusChip).Justify(ui.Center).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("refresh"))
			reload.Children(func() { ui.Icon(c, icons.RefreshCw).Size(11, 11).TextColor(t.Ink3) })
			reload.OnClick(func() { a.loadStrays() })
		})
		switch {
		case a.projects.straysFail != "":
			ui.Text(c, a.projects.straysFail).FontSize(theme.TextMicro).TextColor(t.Red).
				Padding(2, 4).MaxLines(3)
			return
		case a.projects.straysBusy && len(a.projects.strays) == 0:
			ui.Text(c, i18n.T("loading")).FontSize(theme.TextMicro).TextColor(t.Ink3).Padding(2, 4)
			return
		case len(a.projects.strays) == 0:
			ui.Text(c, i18n.T("straysNone")).FontSize(theme.TextMicro).TextColor(t.Ink3).
				Padding(2, 4).MaxLines(3)
			return
		}
		rows := a.projects.strays
		ui.List(c, &a.projects.strayList, len(rows), func(index int) {
			a.strayRow(c, t, rows[index])
		}).Grow(1).Width(memWidth - 12)
	})
}

// strayRow is one checkout. The branch is the name; the line under it says what
// adopting it would walk into.
func (a *App) strayRow(c *ui.Context, t theme.Tokens, row protocol.Worktree) {
	line := ui.Row(c).FillWidth().Height(44).Padding(2, 6).Gap(8).AlignItems(ui.Center).
		Radius(theme.RadiusControl)
	if line.Hovered() {
		line = line.Background(t.Hover)
	}
	line.Children(func() {
		ui.Icon(c, icons.Folder).Size(12, 12).TextColor(t.Ink3)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, row.Branch).Font(monoFont).FontSize(theme.TextRow).FontWeight(500).
				TextColor(t.Ink).MaxLines(1)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				if row.ConversationID != nil {
					ui.Text(c, i18n.TF("strayOwner", map[string]any{"conv": *row.ConversationID})).
						FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
				} else {
					ui.Text(c, i18n.T("strayOrphan")).FontSize(theme.TextMicro).TextColor(t.Ink3)
				}
				if row.Merged {
					ui.Text(c, i18n.T("strayMerged")).FontSize(theme.TextMicro).TextColor(t.Ink3)
				}
				if row.Dirty {
					ui.Text(c, i18n.T("strayDirty")).FontSize(theme.TextMicro).TextColor(t.Orange)
				}
				if row.Ahead > 0 {
					ui.Text(c, "+"+itoa(row.Ahead)).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Green)
				}
				if row.Behind > 0 {
					ui.Text(c, "−"+itoa(row.Behind)).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Ink3)
				}
			})
		})
		dir := row.Dir
		busy := a.projects.adopting == dir
		label := i18n.T("strayAdopt")
		if busy {
			label = i18n.T("loading")
		}
		btn := ui.ButtonBase(c).Height(22).Radius(theme.RadiusChip).Padding(0, 6).
			Cursor(ui.CursorPointer).Tooltip(i18n.T("strayAdoptTip"))
		btn.Children(func() {
			ui.Text(c, label).FontSize(theme.TextMicro).FontWeight(500).TextColor(t.Ink3)
		})
		if !busy {
			btn.OnClick(func() { a.adoptStray(row) })
		}
	})
}
