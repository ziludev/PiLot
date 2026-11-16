package app

import (
	"context"
	"os"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// projectBar is the switcher at the top of the rail: the current project, a
// menu of the others, and the two ways to change that list.
func (a *App) projectBar(c *ui.Context, t theme.Tokens) {
	current := a.currentProject()
	name := i18n.T("project")
	if current != nil {
		name = current.Name
	}
	ui.Row(c).Padding(6, 10, 2, 10).Children(func() {
		ui.Button(c, "").FillWidth().Height(24).Padding(2, 6).Background(t.Sidebar).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icons.FolderGit2).Size(12, 12).TextColor(t.MutedFg)
				ui.Text(c, name).FontSize(11.5).TextColor(t.SidebarFg).MaxLines(1)
				ui.Row(c).Grow(1)
				ui.Icon(c, icons.ChevronsUpDown).Size(11, 11).TextColor(t.MutedFg)
			})
		}).OnClick(func() {
			a.apply(func() { a.projectMenu = !a.projectMenu; a.invalidate() })
		})
	})
	if a.projectMenu {
		a.projectMenuBody(c, t, current)
	}
	a.projectDialogs(c, t)
}

// currentProject resolves the selected project, falling back to the first one
// the bridge listed, as the earlier client does.
func (a *App) currentProject() *protocol.Project {
	for i := range a.projects {
		if a.projects[i].ID == a.project {
			return &a.projects[i]
		}
	}
	if len(a.projects) > 0 {
		return &a.projects[0]
	}
	return nil
}

// projectMenuBody lists the projects with the current one checked; a project
// that came with the workspace cannot be removed.
func (a *App) projectMenuBody(c *ui.Context, t theme.Tokens, current *protocol.Project) {
	ui.Column(c).FillWidth().Padding(4, 6).Gap(1).Margin(4, 6, 0, 6).Radius(8).
		Border(1, t.Border).Background(t.Popover).Children(func() {
		for _, project := range a.projects {
			project := project
			active := current != nil && project.ID == current.ID
			ui.Row(c).FillWidth().Gap(6).Padding(4, 6).Radius(4).Background(t.Popover).Children(func() {
				icon := icons.Check
				color := t.Primary
				if !active {
					color = t.Popover
				}
				ui.Icon(c, icon).Size(11, 11).TextColor(color)
				ui.Column(c).Grow(1).Children(func() {
					ui.Text(c, project.Name).FontSize(11).TextColor(t.PopoverFg)
					ui.Text(c, shortPath(project.Path)).FontSize(10).TextColor(t.MutedFg)
				})
				if !project.Builtin {
					ui.Button(c, "").Padding(2, 3).Children(func() {
						ui.Icon(c, icons.Trash).Size(11, 11).TextColor(t.MutedFg)
					}).OnClick(func() { a.askRemoveProject(project) })
				}
			}).OnClick(func() { a.setProject(project.ID) })
		}
		ui.Divider(c).Background(t.Border).MarginY(4)
		ui.Row(c).FillWidth().Gap(6).Padding(4, 6).Children(func() {
			ui.Icon(c, icons.FolderPlus).Size(11, 11).TextColor(t.PopoverFg)
			ui.Text(c, i18n.T("openProject")).FontSize(11).TextColor(t.PopoverFg)
		}).OnClick(func() {
			a.apply(func() {
				a.projectMenu = false
				a.projectPath = ""
				a.invalidate()
			})
			a.chooseProject()
		})
	})
}

// projectDialogs draws the open and remove dialogs, each only when asked for.
func (a *App) projectDialogs(c *ui.Context, t theme.Tokens) {
	open := a.projectOpen
	ui.Modal(c, &open, func() {
		ui.Text(c, i18n.T("openProject")).FontSize(13).FontWeight(600).TextColor(t.Foreground)
		ui.Text(c, i18n.T("openProjectHint")).FontSize(10).TextColor(t.MutedFg)
		ui.TextInput(c, &a.projectPath).Width(320).FontSize(12).AutoFocus().
			Placeholder("~/work/example-service").OnSubmit(func() { a.submitProject() })
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			ui.Row(c).Grow(1)
			ui.Button(c, i18n.T("cancel")).FontSize(11).OnClick(func() {
				a.apply(func() { a.projectOpen = false; a.invalidate() })
			})
			ui.PrimaryButton(c, i18n.T("open")).OnClick(func() { a.submitProject() })
		})
	})
	if a.projectOpen != open {
		a.apply(func() { a.projectOpen = open; a.invalidate() })
	}

	removing := a.projectRemoving
	ui.Modal(c, &removing, func() {
		if target := a.removeTarget; target != nil {
			ui.Text(c, i18n.T("removeProject")+" · "+target.Name).FontSize(13).FontWeight(600).TextColor(t.Foreground)
			ui.Text(c, i18n.T("removeProjectHint")).FontSize(10).TextColor(t.MutedFg)
			ui.Row(c).FillWidth().Gap(6).Children(func() {
				ui.Row(c).Grow(1)
				ui.Button(c, i18n.T("cancel")).FontSize(11).OnClick(func() {
					a.apply(func() { a.projectRemoving = false; a.removeTarget = nil; a.invalidate() })
				})
				ui.Button(c, i18n.T("remove")).FontSize(11).Background(t.Destructive).
					TextColor(t.Background).OnClick(func() { a.confirmRemoveProject() })
			})
		}
	})
	if a.projectRemoving != removing {
		a.apply(func() { a.projectRemoving = removing; a.invalidate() })
	}
}

// setProject switches the selected project and reloads the chat list, since
// the surfaces belong to it.
func (a *App) setProject(id string) {
	a.apply(func() {
		a.project = id
		a.projectMenu = false
		a.invalidate()
	})
	a.goLoad(func() error { return a.loadState(context.Background()) })
}

// submitProject adds the typed path and reports what the bridge made of it.
func (a *App) submitProject() {
	path := strings.TrimSpace(a.projectPath)
	if path == "" {
		return
	}
	a.apply(func() { a.projectOpen = false; a.invalidate() })
	a.goLoad(func() error {
		result, err := a.api.AddProject(context.Background(), path)
		a.apply(func() {
			switch {
			case err != nil:
				a.notice = err.Error()
			case result == nil:
			case result.Status == "added", result.Status == "exists":
				name := path
				if result.Project != nil {
					name = result.Project.Name
				}
				a.notice = i18n.T("projectOpened") + " · " + name
			case result.Status == "not-a-repo":
				a.notice = i18n.T("notARepo")
			case result.Status == "missing":
				a.notice = i18n.T("pathMissing")
			default:
				a.notice = result.Status
			}
			a.invalidate()
		})
		if err != nil {
			return err
		}
		return a.loadState(context.Background())
	})
}

// askRemoveProject shows the confirmation, since removal is not undoable.
func (a *App) askRemoveProject(project protocol.Project) {
	a.apply(func() {
		a.projectMenu = false
		a.projectRemoving = true
		a.removeTarget = &project
		a.invalidate()
	})
}

// confirmRemoveProject unregisters the project and says how many chats moved.
func (a *App) confirmRemoveProject() {
	target := a.removeTarget
	a.apply(func() { a.projectRemoving = false; a.removeTarget = nil; a.invalidate() })
	if target == nil {
		return
	}
	a.goLoad(func() error {
		result, err := a.api.RemoveProject(context.Background(), target.ID)
		a.apply(func() {
			if err != nil {
				a.notice = err.Error()
			} else if result != nil && result.MovedChats > 0 {
				a.notice = i18n.T("projectRemoved") + " · " + itoa(result.MovedChats) + " " + i18n.T("chatsMoved")
			} else {
				a.notice = i18n.T("projectRemoved")
			}
			a.invalidate()
		})
		if err != nil {
			return err
		}
		return a.loadState(context.Background())
	})
}

// shortPath replaces the home prefix with ~, as the earlier client menu does.
func shortPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.HasPrefix(path, home+"/") {
		return "~/" + strings.TrimPrefix(path, home+"/")
	}
	return path
}
