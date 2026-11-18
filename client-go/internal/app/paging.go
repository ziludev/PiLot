package app

import (
	"pitta/internal/protocol"
)

// oldestEntryID is the id the next page ends before: 0 when nothing is loaded.
func (a *App) oldestEntryID() int {
	if len(a.entries) == 0 {
		return 0
	}
	return a.entries[0].ID
}

// prependOlder puts a page of older entries above the ones held. Entries the
// client already has are dropped: a frame can arrive while the request is in
// flight, and that frame's window already overlaps the page.
func prependOlder(held, older []protocol.Entry) ([]protocol.Entry, int) {
	if len(older) == 0 {
		return held, 0
	}
	first := 0
	if len(held) > 0 {
		first = held[0].ID
	}
	fresh := make([]protocol.Entry, 0, len(older))
	for _, entry := range older {
		if first > 0 && entry.ID >= first {
			continue
		}
		fresh = append(fresh, entry)
	}
	if len(fresh) == 0 {
		return held, 0
	}
	merged := make([]protocol.Entry, 0, len(fresh)+len(held))
	merged = append(merged, fresh...)
	return append(merged, held...), len(fresh)
}

// loadOlder asks the bridge for the entries above the transcript's oldest one.
// Without it a chat only ever shows the newest page, and 200 entries is a
// handful of turns once tool calls and their output are in it.
func (a *App) loadOlder() {
	if a.historyLoading || !a.historyMore || a.api == nil {
		return
	}
	conversation := a.currentValue()
	oldest := a.oldestEntryID()
	if conversation == 0 || oldest == 0 {
		return
	}
	a.historyLoading = true
	ctx := a.ctx
	go func() {
		page, err := a.api.ConversationBefore(ctx, conversation, oldest)
		a.apply(func() {
			a.historyLoading = false
			if err != nil {
				// A bridge that cannot page (an older one ignores `before`) must
				// not be asked again on every frame.
				a.historyMore = false
				a.fail("load older entries of %d: %v", conversation, err)
				return
			}
			if a.current != conversation {
				return
			}
			merged, added := prependOlder(a.entries, page.Entries)
			a.historyMore = page.HasMore
			if added == 0 {
				a.invalidate()
				return
			}
			// The page is drawn above what is on screen, so the frame after it is
			// laid out moves the offset down by the height it added.
			a.anchorMaxY, a.anchorArmed = a.transcriptScroll.MaxY, true
			a.entries = merged
			a.turnStale = true
			// The page lands above the window rather than in it: a chat whose
			// turns carry forty tool calls each costs about a second a frame
			// when every one of them is drawn, so what the window shows is the
			// reader's call, made with the button it already had.
			a.invalidate()
		})
	}()
}
