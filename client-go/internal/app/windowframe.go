package app

// restoreFrame puts the window back where it was last closed, which is what a
// desktop app owes its owner: the same size and place on the same screen.
func (a *App) restoreFrame() {
	if a.win == nil || a.secondary {
		return
	}
	p := a.prefs
	width, height := saneFrame(p.WindowW, p.WindowH, 1180, 820)
	a.win.SetSize(int(width), int(height))
	// Zero means "never placed"; the window manager's own choice is better than
	// the top-left corner.
	if p.WindowX != 0 || p.WindowY != 0 {
		a.win.SetPosition(int(p.WindowX), int(p.WindowY))
	}
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
