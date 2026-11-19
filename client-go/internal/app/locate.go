package app

// Opening the checkout.
//
// Every chat works in its own worktree, so once the diff has been read the next
// step is usually to touch the files by hand — and the window knows where they
// are, which is the whole point of not making anyone copy a path out of it.
//
// Both verbs ask the bridge first when the drawer has not been opened yet: the
// path is only in the window after a /api/changes read, and a menu item that
// works only after visiting another panel is not worth a keystroke.

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

var errNoChat = errors.New("no chat is open")

// worktreePath is where the open chat's files live.
func (a *App) worktreePath() (string, error) {
	if a.active == 0 {
		return "", errNoChat
	}
	if a.changes.conv == a.active {
		if path := strings.TrimSpace(a.changes.workspace); path != "" {
			return path, nil
		}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	// Changes rather than Git: its workspace field is the same one the drawer
	// uses, and it is the cheaper of the two reads.
	changes, err := a.api.Changes(ctx, a.active)
	if err != nil {
		return "", err
	}
	if path := strings.TrimSpace(changes.Workspace); path != "" {
		return path, nil
	}
	if changes.Error != "" {
		return "", errors.New(changes.Error)
	}
	return "", errors.New("this chat has no checkout yet")
}

// Reveal shows the checkout in Finder.
func (a *App) Reveal() {
	path, err := a.worktreePath()
	if err != nil {
		a.post(event{kind: evNotice, notice: err.Error()})
		return
	}
	a.openPath([]string{}, path)
}

// OpenInEditor opens the checkout in the editor the user has. The candidates are
// probed, not configured: macOS already knows which bundles are installed, and
// asking which one is a setting nobody wants to set twice. PILOT_EDITOR wins so
// the choice can still be made explicit.
func (a *App) OpenInEditor() {
	path, err := a.worktreePath()
	if err != nil {
		a.post(event{kind: evNotice, notice: err.Error()})
		return
	}
	a.openPath(a.editorArgs(), path)
}

// editorArgs is how to open a path in the editor, or the Finder's own way when
// none of the known ones is installed — opening the folder is still better than
// a menu item that does nothing.
func (a *App) editorArgs() []string {
	if custom := strings.TrimSpace(os.Getenv("PILOT_EDITOR")); custom != "" {
		return []string{"-a", custom}
	}
	for _, app := range []string{"Zed", "Cursor", "Visual Studio Code", "Sublime Text", "Xcode"} {
		if installed(app) {
			return []string{"-a", app}
		}
	}
	return nil
}

// installed asks LaunchServices whether the app can be found, which also covers
// the ones in ~/Applications without naming every folder.
func installed(app string) bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return exec.Command("open", "-Ra", app).Run() == nil
}

// openPath hands the folder to `open` off the frame thread: LaunchServices can
// take a moment, and no paint should wait for it.
func (a *App) openPath(args []string, path string) {
	go func() {
		cmd := exec.Command("open", append(args, path)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			a.post(event{kind: evNotice, notice: strings.TrimSpace(string(out))})
		}
	}()
}
