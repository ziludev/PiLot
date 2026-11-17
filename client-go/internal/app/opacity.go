package app

import "github.com/egoist/mygo"

// Window opacity
//
// A window over a code editor is read at a glance, and a solid one hides the
// line underneath it. The setting lives in the preference file so it survives a
// restart, and applies to every window the app opens rather than only the one
// the slider was dragged in.
const (
	// minOpacity keeps the text readable: below this the window is a ghost.
	minOpacity float32 = 0.55
	maxOpacity float32 = 1
)

var (
	// opacityHook is how a window's transparency is set; main.go binds it,
	// because only main creates windows.
	opacityHook func(*mygo.Window, float32)
	// windowOpacity is the value every window was last given. Package level
	// because a second window shares the preference file of the first: it has
	// no prefs of its own to read.
	windowOpacity = maxOpacity
)

// SetOpacityHook lets main.go hand the app a way to set a window's opacity.
func SetOpacityHook(fn func(*mygo.Window, float32)) { opacityHook = fn }

// clampOpacity keeps a hand-edited preference readable.
func clampOpacity(value float32) float32 {
	if value < minOpacity {
		return minOpacity
	}
	if value > maxOpacity {
		return maxOpacity
	}
	return value
}

// setOpacity stores the transparency and puts it on the open windows. UI thread.
func (a *App) setOpacity(value float32) {
	windowOpacity = clampOpacity(value)
	a.prefs.Opacity = windowOpacity
	for _, win := range mygo.Windows() {
		applyOpacity(win)
	}
}

// applyOpacity puts the stored transparency on one window, which Attach calls so
// a window opened later lands at the same transparency as the first.
func applyOpacity(win *mygo.Window) {
	if win == nil || opacityHook == nil {
		return
	}
	opacityHook(win, windowOpacity)
}
