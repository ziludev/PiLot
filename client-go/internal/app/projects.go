package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
	"pilot/internal/icons"
	"pilot/internal/protocol"
	"pilot/internal/theme"
)

// The projects drawer: the repositories a chat can live in. It is the one place
// where the bridge's own catalogue is visible, because a chat's worktree is
// under one of these and no chat can say which without asking.
type projectsPanel struct {
	open   bool
	loaded bool
	seq    int
	failed string
	items  []protocol.Project
	// path is what the footer's field holds; adding is the write in flight.
	path   string
	adding bool
	// busyID is the project whose removal is in flight.
	busyID string
	result string
	bad    bool

	// strays are the checkouts the project's sessions left behind: work no chat
	// is showing, one button away from being adopted by a new one.
	strays     []protocol.Worktree
	straysFor  string
	straysSeq  int
	straysBusy bool
	straysFail string
	strayList  ui.ListState
	// adopting is the directory whose adopt is in flight.
	adopting string
}

func (a *App) toggleProjects() {
	if a.projects.open {
		a.projects.open = false
		a.invalidate()
		return
	}
	a.projects.open = true
	// One drawer on the right at a time: 460 points does not hold two columns
	// of detail.
	a.changes.open = false
	a.mem.open = false
	a.tl.open = false
	a.loadProjects()
	a.loadStrays()
	a.invalidate()
}

func (a *App) loadProjects() {
	seq := a.projects.seq + 1
	a.projects.seq = seq
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		items, err := a.api.Projects(ctx)
		a.post(event{kind: evProjects, seq: seq, projects: items, err: err})
	}()
}

func (a *App) applyProjects(seq int, items []protocol.Project, err error) {
	if seq != a.projects.seq {
		return
	}
	a.projects.loaded = true
	if err != nil {
		a.projects.failed = err.Error()
		a.invalidate()
		return
	}
	a.projects.failed = ""
	a.projects.items = items
	a.invalidate()
}

// addProject registers the path in the footer's field. A refusal says which
// kind it was — not a repo, no such folder, already there — because those need
// different things from the person reading them.
func (a *App) addProject() {
	path := strings.TrimSpace(a.projects.path)
	if path == "" || a.projects.adding {
		return
	}
	a.projects.adding = true
	a.projects.result, a.projects.bad = "", false
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		out, err := a.api.AddProject(ctx, path)
		text, bad := projectAdded(out, path, err)
		a.post(event{kind: evProjectAdd, text: text, writeBad: bad, path: path})
	}()
}

func (a *App) applyProjectAdd(path, text string, bad bool) {
	a.projects.adding = false
	a.projects.result, a.projects.bad = text, bad
	if !bad {
		a.projects.path = ""
		a.loadProjects()
	}
	a.invalidate()
}

func projectAdded(out protocol.ProjectOpen, path string, err error) (string, bool) {
	if err != nil {
		return failed(err), true
	}
	switch out.Status {
	case "added":
		name := path
		if out.Project != nil && out.Project.Name != "" {
			name = out.Project.Name
		}
		return i18n.TF("projectAdded", map[string]any{"name": name}), false
	case "exists":
		name := path
		if out.Project != nil && out.Project.Name != "" {
			name = out.Project.Name
		}
		return i18n.TF("projectExists", map[string]any{"name": name}), false
	case "not-a-repo":
		return i18n.TF("projectNotRepo", map[string]any{"path": path}), true
	case "missing":
		return i18n.TF("projectMissing", map[string]any{"path": path}), true
	}
	return failedString(i18n.T("projectAddFailed")), true
}

// removeProject forgets a project. The chats in it move to the built-in
// sandbox, which is worth saying out loud: they do not disappear, they move.
func (a *App) removeProject(p protocol.Project) {
	if p.Builtin || a.projects.busyID != "" {
		return
	}
	a.projects.busyID = p.ID
	a.projects.result, a.projects.bad = "", false
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		out, err := a.api.RemoveProject(ctx, p.ID)
		text, bad := projectRemoved(p, out, err)
		a.post(event{kind: evProjectRemove, text: text, writeBad: bad})
	}()
}

func projectRemoved(p protocol.Project, out protocol.ProjectRemoved, err error) (string, bool) {
	if err != nil {
		return failed(err), true
	}
	switch out.Status {
	case "removed":
		if out.MovedChats > 0 {
			return i18n.TF("projectRemovedMoved", map[string]any{"name": p.Name, "n": out.MovedChats}), false
		}
		return i18n.TF("projectRemoved", map[string]any{"name": p.Name}), false
	case "builtin":
		return i18n.T("projectRemoveBuiltin"), true
	}
	return i18n.T("projectRemoveMissing"), true
}

func (a *App) applyProjectRemove(text string, bad bool) {
	a.projects.busyID = ""
	a.projects.result, a.projects.bad = text, bad
	if !bad {
		a.loadProjects()
	}
	a.invalidate()
}

// openProjectChat starts a chat in a project's worktree: the whole reason the
// drawer lists them.
func (a *App) openProjectChat(p protocol.Project) {
	a.projects.open = false
	a.newChatIn(p.ID)
}

func (a *App) projectsView(c *ui.Context, t theme.Tokens) {
	ui.Column(c).Width(memWidth).Shrink(1).MinWidth(260).FillHeight().
		Radius(theme.RadiusWindow).Border(1, t.Line).Background(t.Surface).Clip().
		Children(func() {
			a.projectsHeader(c, t)
			ui.Box(c).Width(memWidth).Height(1).Background(t.Line)
			a.projectList(c, t)
			ui.Box(c).FillWidth().Height(1).Background(t.Line)
			a.straysSection(c, t)
			a.projectsFooter(c, t)
		})
}

func (a *App) projectsHeader(c *ui.Context, t theme.Tokens) {
	// DragWindow: the drawer sits at the top too, so its header drags as well.
	ui.Row(c).FillWidth().Height(48).Padding(0, 12).AlignItems(ui.Center).Gap(8).
		DragWindow().Children(func() {
		ui.Text(c, i18n.T("projects")).FontSize(theme.TextTitle).FontWeight(500).TextColor(t.Ink).MaxLines(1)
		ui.Spacer(c)
		reload := ui.ButtonBase(c).Width(20).Height(20).Radius(theme.RadiusChip).Cursor(ui.CursorPointer).
			Tooltip(i18n.T("refresh"))
		reload.Children(func() { ui.Icon(c, icons.Clock).Size(11, 11).TextColor(t.MutedFg) })
		reload.OnClick(func() { a.loadProjects() })
		close := ui.ButtonBase(c).Width(20).Height(20).Radius(theme.RadiusChip).Cursor(ui.CursorPointer)
		close.Children(func() { ui.Text(c, "×").FontSize(theme.TextTitle).TextColor(t.MutedFg) })
		close.OnClick(func() { a.projects.open = false; a.invalidate() })
	})
}

func (a *App) projectList(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Padding(4, 6).Gap(2).Children(func() {
		switch {
		case a.projects.failed != "":
			ui.Text(c, a.projects.failed).FontSize(theme.TextMicro).TextColor(t.Destructive).
				Padding(6, 6).MaxLines(4)
			return
		case !a.projects.loaded:
			ui.Text(c, i18n.T("loading")).FontSize(theme.TextMicro).TextColor(t.MutedFg).Padding(6, 6)
			return
		case len(a.projects.items) == 0:
			ui.Text(c, i18n.T("projectEmpty")).FontSize(theme.TextMicro).TextColor(t.MutedFg).
				Padding(6, 6).MaxLines(4)
			return
		}
		for _, p := range a.projects.items {
			project := p
			a.projectRow(c, t, project)
		}
	})
}

// projectRow is one repository. Clicking it starts a chat there; the × on a
// project that was added by hand forgets it. The built-in sandbox has no ×,
// because a bridge with no project at all has nowhere to put a chat.
func (a *App) projectRow(c *ui.Context, t theme.Tokens, p protocol.Project) {
	row := ui.Row(c).FillWidth().Height(38).Padding(0, 8).Radius(theme.RadiusChip).Gap(6).
		AlignItems(ui.Center).Cursor(ui.CursorPointer)
	// The × only exists under the pointer, so a list of projects does not read
	// as a list of delete buttons.
	hovered := row.Hovered()
	if hovered {
		row = row.Background(t.SidebarAccent)
	}
	row.OnClick(func() { a.openProjectChat(p) })
	row.Children(func() {
		ui.Icon(c, icons.Folder).Size(12, 12).TextColor(t.MutedFg)
		ui.Column(c).Grow(1).Shrink(1).Gap(1).Children(func() {
			name := p.Name
			if p.Builtin {
				name += " · " + i18n.T("projectBuiltin")
			}
			ui.Text(c, name).FontSize(theme.TextSmall).TextColor(t.Foreground).MaxLines(1)
			ui.Text(c, p.Path).FontSize(9.5).TextColor(t.MutedFg).MaxLines(1)
		})
		if p.Builtin {
			return
		}
		if a.projects.busyID == p.ID {
			ui.Icon(c, icons.Clock).Size(10, 10).TextColor(t.MutedFg)
			return
		}
		if !hovered {
			return
		}
		forget := ui.ButtonBase(c).Width(18).Height(18).Radius(theme.RadiusChip).
			Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer).
			Tooltip(i18n.T("projectForget"))
		forget.Children(func() { ui.Text(c, "×").FontSize(theme.TextRow).TextColor(t.MutedFg) })
		forget.OnClick(func() { a.removeProject(p) })
	})
}

func (a *App) projectsFooter(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Padding(8, 12, 10, 12).Gap(6).Children(func() {
		ui.Text(c, i18n.T("projectAddHint")).FontSize(theme.TextMicro).TextColor(t.MutedFg).MaxLines(2)
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			field := ui.TextInputBase(c, &a.projects.path).FontSize(theme.TextSmall).Height(26).
				Padding(0, 8).Radius(theme.RadiusChip).Background(t.SidebarAccent).
				Placeholder(i18n.T("projectAddPlaceholder"))
			field.OnSubmit(func() { a.addProject() })
			a.writeButtonIn(c, t, i18n.T("projectAdd"), a.projects.adding, a.addProject)
		})
		if a.projects.result != "" {
			color := t.MutedFg
			if a.projects.bad {
				color = t.Destructive
			}
			ui.Text(c, a.projects.result).FontSize(theme.TextMicro).TextColor(color).MaxLines(4)
		}
	})
}

// writeButtonIn is a small solid button for a drawer's footer: the same shape
// as the settings dialogs' buttons, sized for a column of text.
func (a *App) writeButtonIn(c *ui.Context, t theme.Tokens, label string, busy bool, fn func()) {
	if busy {
		label = i18n.T("projectAdding")
	}
	btn := ui.ButtonBase(c).Height(26).Padding(0, 10).Radius(theme.RadiusChip).
		Justify(ui.Center).AlignItems(ui.Center)
	if busy {
		btn = btn.Background(t.Secondary)
	} else {
		btn = btn.Background(t.Primary).Cursor(ui.CursorPointer)
	}
	btn.Children(func() {
		color := t.PrimaryFg
		if busy {
			color = t.Foreground
		}
		ui.Text(c, label).FontSize(theme.TextSmall).TextColor(color).MaxLines(1)
	})
	if !busy {
		btn.OnClick(fn)
	}
}
