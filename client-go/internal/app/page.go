package app

// scrollPage moves the transcript by most of a screen, which is how a long chat
// is read. Turns are not virtualized, so their offsets are only exact for the
// ones drawn, and a page is the measurement that always holds.
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
	// Paging back stops the transcript from following new answers.
	a.stickToBottom = false
	a.atBottom = false
	// The offset moves directly rather than gliding: followScroll runs on the
	// next frame and would read an unchanged Y as "still at the bottom" and
	// snap back.
	a.transcriptScroll.Y = target
}
