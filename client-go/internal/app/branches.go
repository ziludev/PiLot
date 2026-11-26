package app

// The branches tab: every ref in the checkout, how far each stands from the base
// this chat is measured against, and the two moves that belong in a drawer.
//
// It sits beside the patch rather than in its own drawer because both answer
// questions about one worktree, and two 460-point columns would leave the
// transcript a stripe. The footer's base readout used to be read-only on the
// argument that moving a base is a git question; the list is what changed that —
// with the branches in front of you, "measure my work from that one" is a
// sentence the window can carry without guessing which branch you meant.

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
	"pilot/internal/icons"
	"pilot/internal/protocol"
	"pilot/internal/theme"
)

const (
	tabChanges  = "changes"
	tabBranches = "branches"
	tabMerge    = "merge"
	tabPipeline = "pipeline"
	// branchRowHeight leaves room for a name and one line of badges at 12.5 and
	// 11; the set-base button rides the same row.
	branchRowHeight = 44
)

// nameBranch names or renames the chat's own branch: an unpushed one is renamed
// in place, an existing one is attached, and either answer is said in the footer.
func (a *App) nameBranch() {
	conv := a.changes.conv
	name := strings.TrimSpace(a.changes.branchNameDraft)
	if a.api == nil || conv == 0 || name == "" {
		return
	}
	ctx := a.ctx
	client := a.api
	a.changes.branchBusy = true
	a.invalidate()
	go func() {
		got, err := client.SetBranchName(ctx, conv, name)
		a.post(event{kind: evBranchName, convID: conv, branchName: got, err: err})
	}()
}

// applyBranchName says what the name change did and re-reads both halves.
func (a *App) applyBranchName(conv int, got *protocol.BranchName, err error) {
	if conv != a.changes.branchConv {
		return
	}
	a.changes.branchBusy = false
	a.changes.branchRenaming = false
	a.changes.branchNameDraft = ""
	if err != nil {
		a.changes.toast = err.Error()
		a.invalidate()
		return
	}
	key := "branchNameFailed"
	switch got.Status {
	case "renamed":
		key = "branchNameRenamed"
	case "attached":
		key = "branchNameAttached"
	case "unchanged":
		key = "branchNameSame"
	case "pushed":
		key = "branchNamePushed"
	case "exists":
		key = "branchNameTaken"
	case "dirty":
		key = "branchNameDirty"
	case "invalid":
		key = "branchNameInvalid"
	}
	a.changes.toast = i18n.T(key)
	if got.Message != "" && got.Status == "error" {
		a.changes.toast = got.Message
	}
	a.refreshBranches()
	a.refreshChanges()
	a.invalidate()
}

// setChangesTab switches the drawer's half, reading the branches the first time
// they are asked for.
func (a *App) setChangesTab(tab string) {
	a.changes.tab = tab
	a.changes.openFile, a.changes.lines = "", nil
	if tab == tabBranches && len(a.changes.branches) == 0 {
		a.refreshBranches()
	}
	if tab == tabMerge && a.changes.mr.Status == "" {
		a.refreshMerge()
	}
	if tab == tabPipeline && a.changes.pipeline == nil && !a.changes.ciBusy {
		a.refreshPipeline()
	}
	a.invalidate()
}

// refreshBranches reads the checkout's refs. The base it measures from rides the
// reply, so the drawer never has to hold two truths about it.
func (a *App) refreshBranches() {
	if a.active == 0 {
		return
	}
	conv := a.active
	a.changes.branchConv = conv
	a.changes.branchBusy = true
	a.changes.branchFailed = ""
	a.loadWorkflow(conv)
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		list, err := a.api.Branches(ctx, conv)
		a.post(event{kind: evBranches, convID: conv, branches: list, err: err})
	}()
}

// loadWorkflow reads the two branches this project's work walks between. The
// branches tab owns it because it is the tab about branches.
func (a *App) loadWorkflow(conv int) {
	if a.api == nil || conv == 0 {
		return
	}
	a.changes.flowConv = conv
	a.changes.flowBusy = true
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		got, err := a.api.Workflow(ctx, conv)
		a.post(event{kind: evWorkflow, convID: conv, workflow: &got, err: err})
	}()
}

func (a *App) applyWorkflow(conv int, got *protocol.Workflow, err error) {
	if conv != a.changes.flowConv {
		return
	}
	a.changes.flowBusy = false
	if err != nil || got == nil {
		a.invalidate()
		return
	}
	a.changes.flow = *got
	a.invalidate()
}

// saveWorkflow writes one edited branch back. The field stays open until the
// bridge answers, so a refusal can say so under it.
func (a *App) saveWorkflow(key string) {
	project := a.changes.flowProject
	value := strings.TrimSpace(a.changes.flowDraft)
	if a.api == nil || project == "" || value == "" {
		a.changes.flowEdit = ""
		a.invalidate()
		return
	}
	client := a.api
	ctx := a.ctx
	a.changes.flowBusy = true
	a.invalidate()
	go func() {
		got, err := client.SetWorkflow(ctx, project, map[string]any{key: value})
		a.post(event{kind: evWorkflowSet, text: project, workflow: &got, err: err})
	}()
}

func (a *App) applyWorkflowSet(project string, got *protocol.Workflow, err error) {
	if project != a.changes.flowProject {
		return
	}
	a.changes.flowBusy = false
	a.changes.flowEdit = ""
	a.changes.flowDraft = ""
	if err != nil {
		a.changes.branchFailed = err.Error()
		a.invalidate()
		return
	}
	a.changes.branchFailed = ""
	if got != nil {
		a.changes.flow = *got
	}
	// The strip is derived from these branches, so it is asked again.
	if conv := a.changes.flowConv; conv != 0 {
		a.loadStages(conv)
	}
	a.invalidate()
}

func (a *App) applyBranches(convID int, list *protocol.Branches, err error) {
	if convID != a.active {
		return
	}
	a.changes.branchBusy = false
	if list != nil {
		a.changes.flowProject = list.ProjectID
	}
	if err != nil {
		a.changes.branchFailed = err.Error()
		return
	}
	a.changes.branchFailed = list.Error
	a.changes.branches = list.Branches
	a.changes.branchBase = list.Base
	a.changes.branchCur = list.Current
	// The branch header names what the worktree is on; the reply is fresher than
	// the one the patch was read with.
	if list.Current != "" {
		a.changes.branch = list.Current
	}
	if list.Base != "" {
		a.changes.base = list.Base
	}
}

// setBaseTo moves what this chat's work is measured from. A base is a property
// of the chat, not of the branch, so the bridge writes it and the drawer re-reads
// both halves: every count in the other tab moves with it.
func (a *App) setBaseTo(name string) {
	if a.active == 0 || name == "" || name == a.changes.branchBase {
		return
	}
	conv := a.active
	a.changes.branchBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		err := a.api.SetBase(ctx, conv, name)
		a.post(event{kind: evBranchWrite, convID: conv, text: i18n.TF("baseMoved", map[string]any{"branch": name}), err: err})
	}()
}

// createBranch makes a ref without moving this worktree: switching beneath a
// running agent is exactly the surprise a drawer should not spring.
func (a *App) createBranch() {
	name := strings.TrimSpace(a.changes.branchDraft)
	if a.active == 0 || name == "" {
		return
	}
	conv := a.active
	from := a.changes.branchCur
	a.changes.branchBusy = true
	a.changes.branchDraft = ""
	a.changes.branchNaming = false
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		out, err := a.api.CreateBranch(ctx, conv, name, from)
		a.post(event{kind: evBranchWrite, convID: conv, branchOut: &out, err: err})
	}()
}

// applyBranchWrite reads the bridge's answer as a sentence, then re-reads the
// list: a refused move leaves everything where it was, which is what the redraw
// shows.
func (a *App) applyBranchWrite(convID int, text string, out *api.BranchResult, err error) {
	if convID != a.active {
		return
	}
	a.changes.branchBusy = false
	switch {
	case err != nil:
		a.changes.branchFailed = err.Error()
		return
	case out != nil && out.Status == "created":
		a.changes.branchFailed = ""
		a.changes.toast = i18n.TF("branchCreated", map[string]any{"name": out.Name})
	case out != nil && out.Status == "exists":
		a.changes.branchFailed = i18n.TF("branchExists", map[string]any{"name": out.Name})
		return
	case out != nil && out.Status == "invalid":
		a.changes.branchFailed = i18n.T("branchInvalid")
		return
	default:
		a.changes.branchFailed = ""
		a.changes.toast = text
	}
	a.refreshBranches()
	a.refreshChanges()
}

// changesTabs is the drawer's two halves: the patch, and the branches. An
// underline rather than a segmented track, because the two are pages of one
// document and not two values of one setting.
func (a *App) changesTabs(c *ui.Context, t theme.Tokens) {
	tabs := [4]struct{ id, label string }{
		{tabChanges, i18n.T("changesTab")},
		{tabBranches, i18n.T("branchesTab")},
		{tabMerge, i18n.T("mrTab")},
		{tabPipeline, i18n.T("ciTab")},
	}
	ui.Row(c).FillWidth().Height(30).Padding(0, 10).Gap(2).AlignItems(ui.End).Children(func() {
		active := a.changes.tab
		if active == "" {
			active = tabChanges
		}
		for _, tab := range tabs {
			id, label := tab.id, tab.label
			on := active == id
			btn := ui.ButtonBase(c).Height(28).Radius(theme.RadiusChip).Padding(0, 8).
				Cursor(ui.CursorPointer)
			btn.Children(func() {
				color, weight := t.Ink2, 500
				if on {
					color, weight = t.Ink, 600
				}
				ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Text(c, label).FontSize(theme.TextRow).FontWeight(weight).TextColor(color).MaxLines(1)
					bar := t.Surface
					if on {
						bar = t.Brand
					}
					ui.Box(c).Width(16).Height(2).Radius(1).Background(bar)
				})
			})
			btn.OnClick(func() { a.setChangesTab(id) })
		}
	})
}

// branchesBody is the list: one row per ref, base first, the checked-out branch
// next, then the rest by name — the order the bridge already sorts into.
func (a *App) branchesBody(c *ui.Context, t theme.Tokens) {
	if a.changes.branchFailed != "" {
		ui.Column(c).FillWidth().Padding(0, 12).Children(func() {
			ui.Text(c, a.changes.branchFailed).FontSize(theme.TextSmall).TextColor(t.Red).MaxLines(3)
		})
	}
	switch {
	case a.changes.branchBusy && len(a.changes.branches) == 0:
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("loading")).FontSize(theme.TextSmall).TextColor(t.Ink3)
		})
		return
	case len(a.changes.branches) == 0:
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("branchesNone")).FontSize(theme.TextSmall).TextColor(t.Ink3).MaxLines(2)
		})
		return
	}
	rows := a.changes.branches
	ui.Column(c).FillWidth().Grow(1).Padding(0, 8, 8, 8).Gap(6).Children(func() {
		a.workflowTargets(c, t)
		ui.Row(c).Height(22).Padding(0, 4).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("branchesTab")).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Ink3).Grow(1)
			ui.Text(c, i18n.TF("baseChip", map[string]any{"branch": a.changes.branchBase})).
				FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Line)
		ui.List(c, &a.changes.branchList, len(rows), func(index int) {
			a.branchRow(c, t, rows[index])
		}).Grow(1).Width(changesWidth - 16)
		a.branchesFooter(c, t)
	})
}

// workflowTargets is where the work goes: 提测 hands to one branch, the release
// request goes to another. Both are a project's, not a chat's, and both are
// editable here because this is the tab about branches.
func (a *App) workflowTargets(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Gap(1).Children(func() {
		a.workflowTargetRow(c, t, i18n.T("flowTest"), "testBase", a.changes.flow.TestBase)
		a.workflowTargetRow(c, t, i18n.T("flowRelease"), "releaseTarget", a.changes.flow.ReleaseTarget)
	})
	ui.Box(c).FillWidth().Height(1).Background(t.Line)
}

func (a *App) workflowTargetRow(c *ui.Context, t theme.Tokens, label, key, value string) {
	editing := a.changes.flowEdit == key
	row := ui.Row(c).FillWidth().Height(30).Padding(0, 4).Gap(8).AlignItems(ui.Center)
	row.Children(func() {
		ui.Text(c, label).FontSize(theme.TextMicro).TextColor(t.Ink3).Width(60).MaxLines(1)
		if editing {
			ui.Row(c).Grow(1).Height(24).Radius(theme.RadiusChip).Padding(0, 6).
				Background(t.Field).Border(1, t.LineSoft).AlignItems(ui.Center).Children(func() {
				field := ui.TextInputBase(c, &a.changes.flowDraft).FontSize(theme.TextSmall).
					Height(20).Grow(1)
				field.OnSubmit(func() { a.saveWorkflow(key) })
			})
			return
		}
		ui.Text(c, value).FontSize(theme.TextSmall).FontWeight(500).TextColor(t.Ink).
			Grow(1).MaxLines(1)
		edit := ui.ButtonBase(c).Size(22, 22).Radius(theme.RadiusChip).Justify(ui.Center).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("flowEditTip"))
		if edit.Hovered() {
			edit = edit.Background(t.Field)
		}
		edit.Children(func() { ui.Icon(c, icons.Pencil).Size(11, 11).TextColor(t.Ink3) })
		edit.OnClick(func() {
			a.changes.flowEdit = key
			a.changes.flowDraft = value
			a.invalidate()
		})
	})
}

func (a *App) branchRow(c *ui.Context, t theme.Tokens, row protocol.BranchRef) {
	bg := t.Surface
	if row.Current {
		bg = t.Field
	}
	line := ui.Row(c).FillWidth().Height(branchRowHeight).Padding(2, 8).Gap(8).AlignItems(ui.Center).
		Radius(theme.RadiusControl).Background(bg)
	line.Children(func() {
		tint := t.Ink3
		if row.Current {
			tint = t.Brand
		}
		ui.Icon(c, icons.GitBranch).Size(12, 12).TextColor(tint)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			color, weight := t.Ink2, 500
			if row.Current || row.Base {
				color, weight = t.Ink, 600
			}
			ui.Text(c, row.Name).FontSize(theme.TextRow).FontWeight(weight).TextColor(color).MaxLines(1)
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				if row.Current {
					ui.Text(c, i18n.T("branchCurrent")).FontSize(theme.TextMicro).TextColor(t.Brand)
				}
				if row.Base {
					ui.Text(c, i18n.T("branchBase")).FontSize(theme.TextMicro).TextColor(t.Ink3)
				}
				if row.Owner != nil {
					ui.Text(c, i18n.TF("branchOwner", map[string]any{"conv": row.Owner.ConversationID})).
						FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
				}
				if row.Ahead > 0 {
					ui.Text(c, "+"+itoa(row.Ahead)).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Green)
				}
				if row.Behind > 0 {
					ui.Text(c, "−"+itoa(row.Behind)).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Ink3)
				}
			})
		})
		// A base is the one branch the button cannot offer: it is already the
		// answer to the question it asks.
		if !row.Base && !row.Current {
			name := row.Name
			btn := ui.ButtonBase(c).Height(22).Radius(theme.RadiusChip).Padding(0, 6).
				Cursor(ui.CursorPointer).Tooltip(i18n.T("branchSetBaseTip"))
			btn.Children(func() {
				ui.Text(c, i18n.T("branchSetBase")).FontSize(theme.TextMicro).FontWeight(500).TextColor(t.Ink3)
			})
			btn.OnClick(func() { a.setBaseTo(name) })
		}
	})
}

// branchesFooter is where a branch is made: a name, and a key.
func (a *App) branchesFooter(c *ui.Context, t theme.Tokens) {
	if a.changes.toast != "" {
		ui.Text(c, a.changes.toast).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(2).Padding(2, 8)
	}
	ui.Box(c).FillWidth().Height(1).Background(t.Line)
	ui.Column(c).FillWidth().Padding(8, 8, 0, 8).Gap(6).Children(func() {
		if a.changes.branchNaming {
			ui.Row(c).FillWidth().Height(34).Radius(theme.RadiusControl).Padding(0, 6).Gap(6).
				AlignItems(ui.Center).Background(t.Field).Border(1, t.LineSoft).Children(func() {
				field := ui.TextInputBase(c, &a.changes.branchDraft).FontSize(theme.TextRow).Height(24).Grow(1).
					Placeholder(i18n.T("branchPlaceholder")).Font(monoFont)
				field.OnSubmit(func() { a.createBranch() })
				field.AutoFocus()
			})
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.TF("branchFrom", map[string]any{"branch": a.changes.branchCur})).
					FontSize(theme.TextMicro).TextColor(t.Ink3).Grow(1).MaxLines(1)
				cancel := ui.ButtonBase(c).Height(24).Radius(theme.RadiusChip).Padding(0, 8).
					Cursor(ui.CursorPointer)
				cancel.Children(func() {
					ui.Text(c, i18n.T("cancel")).FontSize(theme.TextMicro).TextColor(t.Ink3)
				})
				cancel.OnClick(func() {
					a.changes.branchNaming = false
					a.changes.branchDraft = ""
				})
				create := ui.ButtonBase(c).Height(24).Radius(theme.RadiusChip).Padding(0, 10).
					Cursor(ui.CursorPointer).Background(t.Ink)
				create.Children(func() {
					ui.Text(c, i18n.T("branchCreate")).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Surface)
				})
				create.OnClick(func() { a.createBranch() })
			})
			return
		}
		if a.changes.branchRenaming {
			ui.Row(c).FillWidth().Height(34).Radius(theme.RadiusControl).Padding(0, 6).Gap(6).
				AlignItems(ui.Center).Background(t.Field).Border(1, t.LineSoft).Children(func() {
				field := ui.TextInputBase(c, &a.changes.branchNameDraft).FontSize(theme.TextRow).Height(24).Grow(1).
					Placeholder(i18n.T("branchNamePlaceholder")).Font(monoFont)
				field.OnSubmit(func() { a.nameBranch() })
				field.AutoFocus()
			})
			ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("branchNameHint")).FontSize(theme.TextMicro).TextColor(t.Ink3).Grow(1).MaxLines(2)
				cancel := ui.ButtonBase(c).Height(24).Radius(theme.RadiusChip).Padding(0, 8).
					Cursor(ui.CursorPointer)
				cancel.Children(func() {
					ui.Text(c, i18n.T("cancel")).FontSize(theme.TextMicro).TextColor(t.Ink3)
				})
				cancel.OnClick(func() {
					a.changes.branchRenaming = false
					a.changes.branchNameDraft = ""
				})
				apply := ui.ButtonBase(c).Height(24).Radius(theme.RadiusChip).Padding(0, 10).
					Cursor(ui.CursorPointer).Background(t.Ink)
				apply.Children(func() {
					ui.Text(c, i18n.T("branchNameApply")).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Surface)
				})
				apply.OnClick(func() { a.nameBranch() })
			})
			return
		}
		nameRow := ui.ButtonBase(c).FillWidth().Height(28).Radius(theme.RadiusControl).
			Padding(0, 8).Gap(6).Justify(ui.Center).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Background(t.Field).Border(1, t.LineSoft)
		nameRow.Children(func() {
			ui.Icon(c, icons.GitBranch).Size(12, 12).TextColor(t.Ink2)
			ui.Text(c, i18n.T("branchNameOpen")).FontSize(theme.TextRow).FontWeight(500).TextColor(t.Ink2)
		})
		nameRow.OnClick(func() {
			a.changes.branchRenaming = true
			a.changes.branchNameDraft = a.changes.branchCur
			a.invalidate()
		})
		newBranch := ui.ButtonBase(c).FillWidth().Height(28).Radius(theme.RadiusControl).
			Padding(0, 8).Gap(6).Justify(ui.Center).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Background(t.Field).Border(1, t.LineSoft)
		newBranch.Children(func() {
			ui.Icon(c, icons.Plus).Size(12, 12).TextColor(t.Ink2)
			ui.Text(c, i18n.T("branchNew")).FontSize(theme.TextRow).FontWeight(500).TextColor(t.Ink2)
		})
		newBranch.OnClick(func() {
			a.changes.branchNaming = true
			a.changes.branchDraft = ""
			a.invalidate()
		})
	})
}
