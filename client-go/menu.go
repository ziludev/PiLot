package main

import (
	"log"

	"github.com/egoist/mygo"

	"pitta/internal/app"
)

// installMenu builds the menu bar. A native client gets the standard menus the
// system expects — Edit's roles make ⌘C/⌘V/⌘A work in every text field, and
// Window's act on the focused window — plus this app's own commands.
func installMenu(a *app.App) {
	item := func(label, accelerator string, click func()) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Accelerator: accelerator, Click: func(*mygo.MenuItem, *mygo.Window) {
			click()
		}}
	}
	role := func(role mygo.MenuRole) *mygo.MenuItem {
		return &mygo.MenuItem{Role: role}
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "File", Submenu: []*mygo.MenuItem{
			item("New chat", "CmdOrCtrl+N", a.MenuNewChat),
			item("Attach image…", "CmdOrCtrl+Shift+A", a.MenuAttachImage),
			item("Open project…", "CmdOrCtrl+Shift+O", a.MenuOpenProject),
			mygo.Separator(),
			item("Restart agent", "CmdOrCtrl+Shift+R", a.MenuRestartBridge),
			item("Back up agent", "", a.MenuBackup),
			mygo.Separator(),
			role(mygo.RoleClose),
		}},
		{Label: "Edit", Submenu: []*mygo.MenuItem{
			role(mygo.RoleUndo), role(mygo.RoleRedo), mygo.Separator(),
			role(mygo.RoleCut), role(mygo.RoleCopy), role(mygo.RolePaste),
			role(mygo.RolePasteAndMatchStyle), role(mygo.RoleDelete), role(mygo.RoleSelectAll),
			mygo.Separator(),
			item("Find in chat", "CmdOrCtrl+F", a.MenuFind),
		}},
		{Label: "View", Submenu: []*mygo.MenuItem{
			item("Switch chat…", "CmdOrCtrl+K", a.MenuSwitcher),
			item("Toggle side panel", "CmdOrCtrl+B", a.MenuToggleSide),
			mygo.Separator(),
			item("Toggle theme", "CmdOrCtrl+Shift+T", a.MenuToggleTheme),
			item("Toggle language", "CmdOrCtrl+Shift+L", a.MenuToggleLanguage),
			mygo.Separator(),
			role(mygo.RoleToggleFullScreen),
		}},
		{Label: "Window", Submenu: []*mygo.MenuItem{
			role(mygo.RoleMinimize), role(mygo.RoleZoom), role(mygo.RoleFront),
		}},
	})
	mygo.App.SetMenu(menu)
}

// registerHotkey binds a global shortcut that brings the window back from
// behind other apps, the one thing a menu bar cannot do.
func registerHotkey(win *mygo.Window) {
	if err := mygo.GlobalShortcut.Register("CmdOrCtrl+Shift+Space", func() {
		win.Show()
		win.Focus()
	}); err != nil {
		log.Printf("menu: global shortcut: %v", err)
	}
}
