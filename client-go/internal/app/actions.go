package app

import (
	"log"
	"os/exec"
)

// The exported actions below are what the application menu calls. They hop to
// the UI thread through apply, so the menu can live on the main thread while
// the state stays where the view owns it.

// MenuNewChat starts a chat in the current project.
func (a *App) MenuNewChat() {
	a.apply(func() { a.newConversation() })
}

// MenuAttachImage opens the panel that attaches pictures.
func (a *App) MenuAttachImage() { a.chooseImages() }

// MenuOpenProject asks for a workspace folder.
func (a *App) MenuOpenProject() { a.chooseProject() }

// MenuToggleSide folds or unfolds the side panel.
func (a *App) MenuToggleSide() {
	a.apply(func() {
		a.toggleSide()
		a.invalidate()
	})
}

// MenuToggleTheme walks system, light and dark.
func (a *App) MenuToggleTheme() {
	a.apply(func() {
		a.toggleTheme()
		a.invalidate()
	})
}

// MenuToggleLanguage switches between English and Chinese.
func (a *App) MenuToggleLanguage() {
	a.apply(func() {
		a.toggleLang()
		a.invalidate()
	})
}

// MenuFind opens the in-chat find bar.
func (a *App) MenuFind() {
	a.apply(func() {
		a.findOpen = true
		a.invalidate()
	})
}

// MenuSwitcher opens the ⌘K jump list.
func (a *App) MenuSwitcher() {
	a.apply(func() { a.openSwitcher() })
}

// MenuRestartBridge restarts the local agent, as the rail's button does.
func (a *App) MenuRestartBridge() { a.control("restart") }

// MenuBackup runs the agent's backup script.
func (a *App) MenuBackup() { a.control("backup") }

// previewFile opens the file in the system's preview window, which reads PDFs,
// images and Office documents that the line view cannot show. qlmanage is the
// Quick Look helper every macOS install carries; elsewhere the file opens in
// whatever handles it.
func (a *App) previewFile() {
	if a.filePath == "" {
		return
	}
	path := a.filePath
	go func() {
		if err := exec.Command("qlmanage", "-p", path).Start(); err != nil {
			log.Printf("app: preview %s: %v", path, err)
		}
	}()
}

// MenuShortcuts opens the cheat sheet.
func (a *App) MenuShortcuts() {
	a.apply(func() {
		a.shortcutsOpen = true
		a.invalidate()
	})
}
