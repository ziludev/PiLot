package main

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo"

	"pitta/internal/app"
)

// trayIcon is the menu bar's copy of the app icon, 32 px for a Retina bar.
//
//go:embed resources/tray.png
var trayIcon []byte

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
			item("Export chat…", "CmdOrCtrl+Shift+E", a.MenuExportChat),
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
			item("Keyboard shortcuts", "CmdOrCtrl+/", a.MenuShortcuts),
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

// installTray puts the app in the menu bar. Its menu reaches the same actions
// as the window's, so the agent stays usable while the window is behind
// everything else; the title carries the number of pending approvals, and the
// running chats are listed so one click lands in them.
func installTray(a *app.App, win *mygo.Window) *mygo.Tray {
	item := func(label string, click func()) *mygo.MenuItem {
		return &mygo.MenuItem{Label: label, Click: func(*mygo.MenuItem, *mygo.Window) { click() }}
	}
	show := func() { win.Show(); win.Focus() }
	build := func(state app.TrayState) *mygo.Menu {
		items := []*mygo.MenuItem{
			item("Show window", show),
			item("New chat", func() { a.MenuNewChat(); show() }),
			item("Switch chat…", func() { a.MenuSwitcher(); show() }),
		}
		if len(state.Approvals) > 0 {
			items = append(items, mygo.Separator())
			for _, approval := range state.Approvals {
				approval := approval
				items = append(items, item("⚠ "+approval.Tool+" — "+approval.Reason, func() {
					show()
					if approval.ConversationID != 0 {
						a.MenuOpenChat(approval.ConversationID)
					}
				}))
			}
		}
		if len(state.Running) > 0 {
			items = append(items, mygo.Separator())
			for _, chat := range state.Running {
				chat := chat
				items = append(items, item("● "+chat.Title, func() {
					show()
					a.MenuOpenChat(chat.ID)
				}))
			}
		}
		items = append(items,
			mygo.Separator(),
			item("Toggle side panel", a.MenuToggleSide),
			mygo.Separator(),
			item("Quit", func() { mygo.App.Quit() }),
		)
		return mygo.NewMenu(items)
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:   "pitta",
		Icon: trayIcon,
		// The bar tints a template icon to its own ink, so the flame is drawn
		// black on transparent and the menu bar decides the colour.
		IconIsTemplate: true,
		ToolTip:        "Pitta",
		Menu:           build(app.TrayState{}),
	})
	if err != nil {
		log.Printf("menu: tray: %v", err)
		return nil
	}
	tray.OnClick(show)
	app.SetTrayHook(func(state app.TrayState) {
		tray.SetMenu(build(state))
	})
	return tray
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
