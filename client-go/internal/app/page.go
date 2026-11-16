package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// scrollPage moves the transcript by most of a screen, which is how a long chat
// is read: a page is the measurement that always holds, since turns are only
// measured once they are drawn.
func (a *App) scrollPage(direction int) {
	page := a.scrollViewH * 0.85
	if page <= 0 {
		page = 480
	}
	// A transcript that follows its end parks at MaxFloat32, so the page is
	// measured from the furthest offset the content actually has.
	current := a.transcriptScroll.Y
	if a.scrollMaxY > 0 && current > a.scrollMaxY {
		current = a.scrollMaxY
	}
	target := current + float32(direction)*page
	if target < 0 {
		target = 0
	}
	if a.scrollMaxY > 0 && target > a.scrollMaxY {
		target = a.scrollMaxY
	}
	// Paging back stops the transcript from following new answers, and the
	// glide is why followScroll must stand aside until it lands.
	a.stickToBottom = false
	a.atBottom = false
	a.pageGliding = true
	a.transcriptScroll.ScrollTo(0, target, ui.Animation{Duration: 180 * time.Millisecond, Ease: ui.EaseOut})
}
