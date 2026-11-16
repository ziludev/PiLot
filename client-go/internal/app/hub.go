package app

import (
	"sync"

	"pitta/internal/protocol"
)

// surfaceHub shares the rail between windows. A chat archived in one window,
// or a new chat started in another, has to show up everywhere: the windows
// share one client and one set of surfaces, so they must share one list.
var surfaceHub = struct {
	sync.Mutex
	subs map[*App]bool
}{subs: map[*App]bool{}}

// subscribeSurfaces makes a window hear the surface list of the others.
func subscribeSurfaces(a *App) {
	surfaceHub.Lock()
	defer surfaceHub.Unlock()
	surfaceHub.subs[a] = true
}

// unsubscribeSurfaces stops the hearing, for a window that closed.
func unsubscribeSurfaces(a *App) {
	surfaceHub.Lock()
	defer surfaceHub.Unlock()
	delete(surfaceHub.subs, a)
}

// broadcastSurfaces hands the list a window just loaded to the others. It runs
// on the goroutine that fetched it, so it only posts to each window's loop.
func broadcastSurfaces(from *App, surfaces []protocol.Surface) {
	surfaceHub.Lock()
	listeners := make([]*App, 0, len(surfaceHub.subs))
	for other := range surfaceHub.subs {
		if other != from {
			listeners = append(listeners, other)
		}
	}
	surfaceHub.Unlock()
	for _, other := range listeners {
		other := other
		other.post(func() {
			other.surfaces = surfaces
			// The chat this window shows may have been archived elsewhere;
			// the rail drops it, and so must the window.
			other.syncTitle()
			other.invalidate()
		})
	}
}
