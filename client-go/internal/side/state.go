// Package side draws the change, file and commit panels of the side rail: the
// worktree's files, the review notes staged for the agent, the read-only file
// viewer, the commit dialog and the branch's commits.
//
// The views are plain functions, as the app calls them from its draw pass.
// Their scroll positions and the folds they remember live in one viewState per
// window, because a ui.ListState cannot be rebuilt every frame without losing
// the place the reader scrolled to.
package side

import (
	"sync"

	"github.com/egoist/mygo/ui"

	"pitta/internal/diff"
)

type viewState struct {
	files   ui.ListState
	file    ui.ListState
	commits ui.ListState
	patch   diff.View
	// code holds the tokenized spans of the file in the viewer, so a frame
	// does not re-tokenize the whole file.
	code highlightCache

	// commitOpen tracks the dialog edge so the draft is seeded from Message
	// when it opens and kept while the reader types.
	commitOpen    bool
	commitMessage string
}

var states sync.Map // *ui.Context → *viewState

func stateFor(c *ui.Context) *viewState {
	if s, ok := states.Load(c); ok {
		return s.(*viewState)
	}
	s, _ := states.LoadOrStore(c, &viewState{})
	return s.(*viewState)
}
