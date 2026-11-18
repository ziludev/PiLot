package app

import (
	"github.com/egoist/mygo/ui"
)

// pageRows is how many rows a page moves, which is how a long chat is read: the
// list scrolls by rows, so a page is a few of them rather than a measurement
// the app has to make.
const pageRows = 4

// scrollPage moves the transcript by most of a screen, which is how a long chat
// is read.
func (a *App) scrollPage(direction int) {
	first, last := a.turnList.Visible()
	if last < first {
		return
	}
	// A list that follows its end has no row below the last one: paging down
	// from there is a page that lands on the end.
	target := first + direction*pageRows
	if target < 0 {
		target = 0
	}
	if target >= len(a.chatRows) {
		a.turnList.ScrollToEnd()
		return
	}
	a.atBottom = false
	a.turnList.ScrollTo(target, ui.Start)
}
