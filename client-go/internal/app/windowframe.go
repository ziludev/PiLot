package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// windowAreas is where a window may sit: the work areas of the connected
// displays. A package variable so a test can place a window on a screen of its
// own instead of the one running it.
var windowAreas = func() []ui.Rect {
	var areas []ui.Rect
	for _, d := range mygo.Screen.Displays() {
		areas = append(areas, ui.Rect{X: float32(d.WorkArea.X), Y: float32(d.WorkArea.Y), W: float32(d.WorkArea.Width), H: float32(d.WorkArea.Height)})
	}
	return areas
}

// restoreFrame puts the window back where it was last closed, which is what a
// desktop app owes its owner: the same size and place on the same screen.
//
// The frame mygo keeps under WindowOptions.StateKey already knows to check that
// the saved corner is still on a connected display, and centers the window when
// it is not; prefs are a second record of the same thing, so they follow the
// same rule. Without it a window closed on an external display reopens off the
// edge of the laptop screen after that display is unplugged.
func (a *App) restoreFrame() {
	if a.win == nil || a.secondary {
		return
	}
	p := a.prefs
	width, height := saneFrame(p.WindowW, p.WindowH, 1180, 820)
	a.win.SetSize(int(width), int(height))
	if x, y, ok := frameCorner(p, width, height, windowAreas()); ok {
		a.win.SetPosition(x, y)
	}
}

// frameCorner is the saved top-left corner, if a window of that size would show
// enough of itself there. Zero means "never placed": the window manager's own
// choice is better than the top-left corner.
func frameCorner(p prefs, width, height float32, areas []ui.Rect) (int, int, bool) {
	if p.WindowX == 0 && p.WindowY == 0 {
		return 0, 0, false
	}
	window := ui.Rect{X: p.WindowX, Y: p.WindowY, W: width, H: height}
	for _, area := range areas {
		// As much of the window as mygo's own restore asks for: enough to grab
		// its title bar and drag it back.
		w := min(area.X+area.W, window.X+window.W) - max(area.X, window.X)
		h := min(area.Y+area.H, window.Y+window.H) - max(area.Y, window.Y)
		if w >= min(100, window.W) && h >= min(50, window.H) {
			return int(p.WindowX), int(p.WindowY), true
		}
	}
	return 0, 0, false
}

// rememberFrame records the window's frame for the next launch.
func (a *App) rememberFrame(p *prefs) {
	if a.win == nil || a.secondary {
		return
	}
	if width, height := a.win.Size(); width > 0 && height > 0 {
		p.WindowW, p.WindowH = float32(width), float32(height)
	}
	if x, y := a.win.Position(); x != 0 || y != 0 {
		p.WindowX, p.WindowY = float32(x), float32(y)
	}
}
