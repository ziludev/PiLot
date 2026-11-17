package app

// Widths a window can be laid out in. A saved rail wider than the window left
// the header drawing clipped slivers of the app's name instead of words.
const (
	minRailWidth = 180
	minMainWidth = 280
	minPaneWidth = 240
	minFrameW    = 640
	minFrameH    = 400
	defaultW     = 1180
	defaultH     = 820
)

// fitPanes clamps the rail and the side panel to the window that is actually
// there. A window can end up narrower than the saved widths — a display that
// went away, a move onto a smaller screen, a resize while the screen was
// locked — and then a fixed 260 px rail covers most of it.
func (a *App) fitPanes(width float32) {
	if width <= 0 {
		return
	}
	maxRail := width * 0.45
	if maxRail < minRailWidth {
		maxRail = minRailWidth
	}
	if a.railWidth > maxRail {
		a.railWidth = maxRail
	}
	if a.railWidth < minRailWidth {
		a.railWidth = minRailWidth
	}
	if !a.sideOpen {
		return
	}
	// The panel gives way first: the transcript is what the window is for.
	room := width - a.railWidth
	maxSide := room - minMainWidth
	if maxSide < minPaneWidth {
		maxSide = minPaneWidth
	}
	if side := a.sideWidthOf(); side > maxSide {
		// Setting mainPane is what resizes the panel; the next frame keeps it.
		a.sideSynced = false
		a.mainPane = room - maxSide
	}
}

// recoverTinyWindow gives a window back its default size when the system
// restored one too small to lay out in — which happens when the window closed,
// or was resized, while the screen was locked.
func (a *App) recoverTinyWindow(width float32) {
	if a.win == nil || a.secondary || a.frameRecovered {
		return
	}
	w, h := a.win.Size()
	if w >= minFrameW && h >= minFrameH {
		a.frameRecovered = true
		return
	}
	a.frameRecovered = true
	a.win.SetSize(defaultW, defaultH)
	a.invalidate()
}

// saneFrame clamps a saved frame to something a window can be.
func saneFrame(width, height, fallbackW, fallbackH float32) (float32, float32) {
	if width < minFrameW || height < minFrameH {
		return fallbackW, fallbackH
	}
	return width, height
}
