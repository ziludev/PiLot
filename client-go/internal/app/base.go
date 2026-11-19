package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// The comparison base: what a chat's work is measured against. It is not a
// preference — it decides which files count as changed, which commits count as
// unmerged, and where 合并 lands, so it gets a chip of its own in the drawer's
// footer rather than a line in a settings page.

func (a *App) openBasePicker() {
	a.changes.baseOpen = true
	a.changes.baseErr = ""
	if len(a.changes.branches) == 0 {
		a.refreshChanges()
	}
	a.invalidate()
}

func (a *App) closeBasePicker() {
	a.changes.baseOpen = false
	a.invalidate()
}

// setBase moves the base. The bridge records it; the drawer then re-reads the
// worktree, because every number on screen is measured from it.
func (a *App) setBase(branch string) {
	if branch == "" || branch == a.changes.base || a.changes.baseBusy {
		return
	}
	conv := a.active
	a.changes.baseBusy = true
	a.changes.baseErr = ""
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		out, err := a.api.SetBase(ctx, conv, branch)
		reason := ""
		switch {
		case err != nil:
			reason = err.Error()
		case !out.OK:
			reason = out.Error
			if reason == "" {
				reason = i18n.T("baseFailed")
			}
		}
		a.post(event{kind: evBase, convID: conv, path: branch, text: reason})
	}()
}

func (a *App) applyBase(convID int, branch, reason string) {
	a.changes.baseBusy = false
	if convID != a.active {
		a.invalidate()
		return
	}
	if reason != "" {
		a.changes.baseErr = reason
		a.invalidate()
		return
	}
	a.changes.base = branch
	a.changes.baseErr = ""
	a.changes.baseOpen = false
	// The branch's commits were counted from the old base; they have to be
	// counted from the new one.
	a.changes.commits = nil
	a.refreshChanges()
}

// baseChip is the footer's first line: the base, and the way to change it.
func (a *App) baseChip(c *ui.Context, t theme.Tokens) {
	chip := ui.ButtonBase(c).Height(20).Padding(0, 6).Radius(5).Gap(4).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).
		Tooltip(i18n.T("baseTooltip"))
	chip.OnClick(a.openBasePicker)
	chip.Children(func() {
		ui.Text(c, i18n.TF("baseChip", map[string]any{"branch": a.changes.base})).
			FontSize(10).TextColor(t.MutedFg).MaxLines(1)
		ui.Text(c, "▾").FontSize(8).TextColor(t.MutedFg)
	})
}

// baseDialog offers the branches the repository has. A base can also be a
// branch that is not checked out anywhere local — a release branch, a remote —
// so the list is a shortcut, not the limit.
func (a *App) baseDialog(c *ui.Context, t theme.Tokens) {
	if !a.changes.baseOpen {
		return
	}
	ui.Modal(c, &a.changes.baseOpen, func() {
		ui.Column(c).Width(360).Padding(16).Gap(10).Radius(10).
			Background(t.Popover).Border(1, t.Border).Children(func() {
			ui.Text(c, i18n.T("baseTitle")).FontSize(12.5).FontWeight(600).TextColor(t.Foreground)
			ui.Text(c, i18n.T("baseHint")).FontSize(10.5).TextColor(t.MutedFg).MaxLines(3)
			a.baseList(c, t)
			if a.changes.baseErr != "" {
				ui.Text(c, a.changes.baseErr).FontSize(10.5).TextColor(t.Destructive).MaxLines(3)
			}
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				a.dialogButton(c, t, i18n.T("cancel"), false, a.closeBasePicker)
			})
		})
	})
}

func (a *App) baseList(c *ui.Context, t theme.Tokens) {
	if len(a.changes.branches) == 0 {
		ui.Text(c, i18n.T("baseNoBranches")).FontSize(10.5).TextColor(t.MutedFg).MaxLines(2)
		return
	}
	// Only as tall as the branches it holds, up to where scrolling starts: a
	// two-branch repository should not open a box of empty space.
	height := float32(len(a.changes.branches) * 28)
	if height > 220 {
		height = 220
	}
	ui.Column(c).FillWidth().Height(height).Children(func() {
		ui.List(c, &a.changes.baseList, len(a.changes.branches), func(row int) {
			a.baseRow(c, t, a.changes.branches[row])
		})
	})
}

// baseRow is one branch. The one in use says so instead of offering itself
// again: a list where the current item looks clickable is a list that invites
// a click that does nothing.
func (a *App) baseRow(c *ui.Context, t theme.Tokens, b protocol.Branch) {
	current := b.Name == a.changes.base
	row := ui.Row(c).FillWidth().Height(28).Padding(0, 8).Radius(5).Gap(6).
		AlignItems(ui.Center)
	if current {
		row = row.Background(t.SidebarAccent)
	} else {
		row = row.Cursor(ui.CursorPointer)
		row.OnClick(func() { a.setBase(b.Name) })
	}
	row.Children(func() {
		ui.Text(c, b.Name).FontSize(11.5).MaxLines(1).
			TextColor(t.Foreground)
		if b.Track != "" {
			ui.Text(c, b.Track).FontSize(9.5).TextColor(t.MutedFg).MaxLines(1)
		}
		ui.Spacer(c)
		switch {
		case a.changes.baseBusy && !current:
			ui.Icon(c, icons.Clock).Size(10, 10).TextColor(t.MutedFg)
		case current:
			ui.Text(c, i18n.T("baseCurrent")).FontSize(9.5).TextColor(t.MutedFg).MaxLines(1)
		}
	})
}
