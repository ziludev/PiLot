package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
	"pilot/internal/theme"
)

// The new-chat dialog exists because a chat's branch is now a person's choice: one
// requirement can span several projects, and the name is what ties them together.
// The default stays one click away — an empty field keeps the derived branch.

type newChatState struct {
	open    bool
	project string
	branch  string
	busy    bool
	failed  string
}

// openNewChat shows the dialog with the built-in workspace already chosen.
func (a *App) openNewChat() {
	a.newChatPanel.open = true
	a.newChatPanel.branch = ""
	a.newChatPanel.failed = ""
	a.newChatPanel.busy = false
	if !a.projects.loaded {
		a.loadProjects()
	}
	a.invalidate()
}

// createNewChat starts the chat the dialog describes, then closes it.
func (a *App) createNewChat() {
	if a.newChatPanel.busy || a.api == nil {
		return
	}
	project := a.newChatPanel.project
	branch := strings.TrimSpace(a.newChatPanel.branch)
	a.newChatPanel.busy = true
	a.newChatPanel.failed = ""
	a.invalidate()
	client := a.api
	ctx := a.ctx
	go func() {
		call, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		convID, _, err := client.NewConversationWithBranch(call, project, branch)
		a.post(event{kind: evNewChat, convID: convID, err: err})
	}()
}

// applyNewChat closes the dialog and opens the chat it made.
func (a *App) applyNewChat(convID int, err error) {
	a.newChatPanel.busy = false
	if err != nil {
		a.newChatPanel.failed = err.Error()
		a.invalidate()
		return
	}
	a.newChatPanel.open = false
	a.newChatPanel.branch = ""
	a.requestRefresh()
	a.post(event{kind: evSelect, convID: convID})
	a.invalidate()
}

// newChatDialog draws the dialog: which project, which branch, and the two answers.
func (a *App) newChatDialog(c *ui.Context, t theme.Tokens) {
	if !a.newChatPanel.open {
		return
	}
	ui.Modal(c, &a.newChatPanel.open, func() {
		ui.Column(c).Width(400).Padding(16).Gap(10).Radius(theme.RadiusWindow).
			Background(t.Surface).Border(1, t.Line).Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.18)).
			Children(func() {
				ui.Text(c, i18n.T("newChatTitle")).FontSize(theme.TextTitle).FontWeight(600).TextColor(t.Ink)
				ui.Text(c, i18n.T("newChatProject")).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Ink3)
				ui.Column(c).FillWidth().Gap(2).Children(func() {
					a.newChatProjectRow(c, t, "", i18n.T("newChatSandbox"))
					for _, project := range a.projects.items {
						a.newChatProjectRow(c, t, project.ID, project.Name)
					}
				})
				ui.Text(c, i18n.T("newChatBranch")).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Ink3)
				ui.Row(c).FillWidth().Height(34).Radius(theme.RadiusControl).Padding(0, 8).
					AlignItems(ui.Center).Background(t.Field).Border(1, t.LineSoft).Children(func() {
					field := ui.TextInputBase(c, &a.newChatPanel.branch).FontSize(theme.TextRow).
						Height(24).Grow(1).Placeholder(i18n.T("branchNamePlaceholder")).Font(monoFont)
					field.OnSubmit(func() { a.createNewChat() })
				})
				ui.Text(c, i18n.T("newChatBranchHint")).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(2)
				if a.newChatPanel.failed != "" {
					ui.Text(c, a.newChatPanel.failed).FontSize(theme.TextMicro).TextColor(t.Red).MaxLines(3)
				}
				ui.Row(c).FillWidth().Justify(ui.End).AlignItems(ui.Center).Gap(8).Children(func() {
					if a.newChatPanel.busy {
						ui.Spinner(c).Size(12, 12).TextColor(t.Ink3)
					}
					a.dialogButton(c, t, i18n.T("cancel"), false, func() { a.newChatPanel.open = false })
					a.dialogButton(c, t, i18n.T("newChatCreate"), true, a.createNewChat)
				})
			})
	})
}

// newChatProjectRow is one choice of project, with the chosen one filled in.
func (a *App) newChatProjectRow(c *ui.Context, t theme.Tokens, id, label string) {
	on := a.newChatPanel.project == id
	row := ui.ButtonBase(c).FillWidth().Height(28).Radius(theme.RadiusChip).Padding(0, 8).
		AlignItems(ui.Center).Justify(ui.Start).Cursor(ui.CursorPointer)
	if on {
		row = row.Background(t.Field)
	} else if row.Hovered() {
		row = row.Background(t.Hover)
	}
	row.OnClick(func() {
		a.newChatPanel.project = id
		a.invalidate()
	})
	row.Children(func() {
		colour, weight := t.Ink2, 500
		if on {
			colour, weight = t.Ink, 600
		}
		ui.Text(c, label).FontSize(theme.TextRow).FontWeight(weight).TextColor(colour).MaxLines(1)
	})
}
