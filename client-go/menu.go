package main

import (
	"github.com/egoist/mygo"

	"pitta/internal/app"
	"pitta/internal/i18n"
)

// installMenu is the application menu: the window's three verbs under File, the
// platform's own Edit and Window roles, and no View menu — the client has no
// zoom, no dev tools and no palette in P0.
func installMenu(a *app.App) {
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{
			Label: i18n.T("fileMenu"),
			Submenu: []*mygo.MenuItem{
				{
					ID:          "new-chat",
					Label:       i18n.T("newChat"),
					Accelerator: "CmdOrCtrl+N",
					Click:       func(*mygo.MenuItem, *mygo.Window) { a.NewChat() },
				},
				{
					ID:          "attach-image",
					Label:       i18n.T("attachImageMenu"),
					Accelerator: "CmdOrCtrl+Shift+I",
					Click:       func(*mygo.MenuItem, *mygo.Window) { a.AttachImages() },
				},
				{Type: mygo.MenuItemSeparator},
				{
					ID:    "restart-agent",
					Label: i18n.T("restartAgent"),
					Click: func(*mygo.MenuItem, *mygo.Window) { a.RestartAgent() },
				},
			},
		},
		{
			Label: i18n.T("editMenu"),
			Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleUndo},
				{Role: mygo.RoleRedo},
				{Type: mygo.MenuItemSeparator},
				{Role: mygo.RoleCut},
				{Role: mygo.RoleCopy},
				{Role: mygo.RolePaste},
				{Role: mygo.RoleSelectAll},
			},
		},
		{
			Label: i18n.T("windowMenu"),
			Submenu: []*mygo.MenuItem{
				{Role: mygo.RoleMinimize},
				{Role: mygo.RoleZoom},
				{Type: mygo.MenuItemSeparator},
				{Role: mygo.RoleClose},
			},
		},
	})
	mygo.App.SetMenu(menu)
}
