package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
	"pitta/internal/side"
	"pitta/internal/theme"
)

// sideChanges is the changes tab: the worktree's files, the review notes the
// next message carries, the commit dialog and the branch's commits.
func (a *App) sideChanges(c *ui.Context, t theme.Tokens) {
	notes := make([]side.ReviewNote, 0, len(a.review))
	for _, note := range a.review {
		notes = append(notes, side.ReviewNote{Path: note.Path, Line: note.Line, Note: note.Note})
	}
	var picked *side.ReviewNote
	if a.reviewPick != nil {
		pick := side.ReviewNote{Path: a.reviewPick.Path, Line: a.reviewPick.Line, Note: a.reviewPick.Note}
		picked = &pick
	}
	side.ChangesView(c, side.ChangesProps{
		Changes:  a.changes,
		Loading:  a.changesLoading,
		Review:   notes,
		Merge:    a.merge,
		Note:     &a.reviewDraft,
		Picked:   picked,
		OnReload: func() { a.reloadGit() },
		OnOpenFile: func(path string) {
			a.openFile(path)
		},
		OnStageNote: func(path, line, note string) {
			a.stageReviewNote(path, line, note)
		},
		OnDropNote:   func(index int) { a.dropReviewNote(index) },
		OnClearNotes: func() { a.clearReview() },
		OnSendReview: func() { a.sendReview() },
		OnCommit:     func() { a.openCommitDialog() },
		OnMerge:      func() { a.mergeBranch() },
	})
	side.CommitsView(c, side.CommitsProps{
		Git:      a.git,
		Expanded: a.commitExpanded,
		Diff:     a.commitDiffs[a.commitExpanded],
		PushNote: a.pushNote,
		OnOpen:   func(hash string) { a.openCommit(hash) },
		OnClose:  func() { a.apply(func() { a.commitExpanded = ""; a.invalidate() }) },
		OnPush:   func(dryRun bool) { a.pushBranch(dryRun) },
	})
	side.CommitDialog(c, side.CommitProps{
		Message:   a.commitMsg,
		Open:      a.commitDialogOpen,
		Busy:      a.commitBusy,
		OnMessage: func(message string) { a.apply(func() { a.commitMsg = message }) },
		OnCancel:  func() { a.apply(func() { a.commitDialogOpen = false; a.invalidate() }) },
		OnSubmit:  func() { a.submitCommit() },
	})
}

// reloadGit refreshes the change list and the commit list together, since one
// usually invalidates the other.
func (a *App) reloadGit() {
	a.goLoad(func() error {
		if err := a.loadChanges(context.Background()); err != nil {
			return err
		}
		return a.loadGit(context.Background())
	})
}

// stageReviewNote appends one line comment; the same path and line replace an
// older note, so re-commenting a line does not stack copies.
func (a *App) stageReviewNote(path string, line, note string) {
	a.apply(func() {
		for i, existing := range a.review {
			if existing.Path == path && existing.Line == line {
				a.review[i].Note = note
				a.reviewDraft = ""
				a.invalidate()
				return
			}
		}
		a.review = append(a.review, ReviewNote{Path: path, Line: line, Note: note})
		a.reviewDraft = ""
		a.invalidate()
	})
}

// dropReviewNote removes one staged comment by index.
func (a *App) dropReviewNote(index int) {
	a.apply(func() {
		if index >= 0 && index < len(a.review) {
			a.review = append(a.review[:index], a.review[index+1:]...)
		}
		a.invalidate()
	})
}

// clearReview drops every staged comment.
func (a *App) clearReview() {
	a.apply(func() {
		a.review = nil
		a.reviewPick = nil
		a.invalidate()
	})
}

// sendReview hands the staged comments to the agent as one message, the way
// React's sendReview builds its prompt.
func (a *App) sendReview() {
	if len(a.review) == 0 {
		return
	}
	var text strings.Builder
	text.WriteString("Please address these review comments:\n")
	for _, note := range a.review {
		text.WriteString("\n- " + note.Path)
		if note.Line != "" {
			text.WriteString(":" + note.Line)
		}
		text.WriteString(" — " + note.Note)
	}
	a.clearReview()
	a.sendText(text.String())
}

// openFile shows one workspace file in place of the transcript.
func (a *App) openFile(path string) {
	a.apply(func() {
		a.view = ViewFile
		a.filePath = path
		a.file = nil
		a.fileErr = ""
		a.invalidate()
	})
	conversationID := a.currentValue()
	a.goLoad(func() error {
		file, err := a.api.File(context.Background(), path, conversationID)
		a.apply(func() {
			if err != nil {
				a.fileErr = err.Error()
			} else {
				a.file = file
			}
			a.invalidate()
		})
		if err != nil {
			return err
		}
		return nil
	})
}

// closeFile returns to the transcript.
func (a *App) closeFile() {
	a.apply(func() {
		a.view = ViewTranscript
		a.filePath = ""
		a.file = nil
		a.fileErr = ""
		a.invalidate()
	})
}

// fileView is the read-only file pane.
func (a *App) fileView(c *ui.Context) {
	side.FileView(c, side.FileProps{
		Path:    a.filePath,
		File:    a.file,
		Err:     a.fileErr,
		OnClose: func() { a.closeFile() },
		OnComment: func(line string) {
			a.apply(func() {
				a.reviewDraft = ""
				a.reviewPick = &ReviewNote{Path: a.filePath, Line: line}
				a.invalidate()
			})
		},
	})
}

// openCommitDialog clears the message and shows the dialog.
func (a *App) openCommitDialog() {
	a.apply(func() {
		a.commitDialogOpen = true
		a.commitMsg = a.defaultCommitMessage()
		a.invalidate()
	})
}

// defaultCommitMessage starts from the file count, as a hint the user can
// overwrite.
func (a *App) defaultCommitMessage() string {
	if a.changes != nil && len(a.changes.Files) > 0 {
		return "Update " + strconv.Itoa(len(a.changes.Files)) + " file(s)"
	}
	return ""
}

// submitCommit commits the worktree and refreshes what the commit changed.
func (a *App) submitCommit() {
	message := strings.TrimSpace(a.commitMsg)
	if message == "" {
		return
	}
	conversationID := a.currentValue()
	a.apply(func() { a.commitBusy = true; a.invalidate() })
	a.goLoad(func() error {
		hash, err := a.api.Commit(context.Background(), conversationID, message)
		a.apply(func() {
			a.commitBusy = false
			if err == nil {
				a.commitDialogOpen = false
				a.notice = "committed " + hash
			}
			a.invalidate()
		})
		if err != nil {
			return err
		}
		return a.loadGit(context.Background())
	})
}

// mergeBranch merges the base branch into this worktree and reports what the
// bridge answered.
func (a *App) mergeBranch() {
	conversationID := a.currentValue()
	a.goLoad(func() error {
		result, err := a.api.Merge(context.Background(), conversationID)
		a.apply(func() {
			if err == nil {
				a.merge = result
			}
			a.invalidate()
		})
		if err != nil {
			return err
		}
		return a.loadGit(context.Background())
	})
}

// openCommit loads one commit's patch, caching it for the second click.
func (a *App) openCommit(hash string) {
	if a.commitExpanded == hash {
		a.apply(func() { a.commitExpanded = ""; a.invalidate() })
		return
	}
	a.apply(func() { a.commitExpanded = hash; a.invalidate() })
	if _, ok := a.commitDiffs[hash]; ok {
		return
	}
	conversationID := a.currentValue()
	a.goLoad(func() error {
		patch, err := a.api.CommitDiff(context.Background(), conversationID, hash)
		if err != nil {
			return err
		}
		a.apply(func() {
			a.commitDiffs[hash] = patch
			a.invalidate()
		})
		return nil
	})
}

// pushBranch pushes the worktree's branch, or only asks whether it would.
func (a *App) pushBranch(dryRun bool) {
	conversationID := a.currentValue()
	a.goLoad(func() error {
		result, err := a.api.Push(context.Background(), conversationID, dryRun)
		a.apply(func() {
			switch {
			case err != nil:
				a.pushNote = err.Error()
			case result == nil:
				a.pushNote = ""
			default:
				a.pushNote = pushSummary(result, dryRun)
			}
			a.invalidate()
		})
		if err != nil {
			return err
		}
		return a.loadGit(context.Background())
	})
}

// pushSummary words what the bridge reported; the states mirror the React
// notice strings.
func pushSummary(result *protocol.PushResult, dryRun bool) string {
	if result.Message != "" {
		return result.Message
	}
	if result.Status == "ok" {
		if dryRun {
			return "would push " + result.Branch
		}
		return "pushed " + result.Branch
	}
	if result.Status != "" {
		return result.Status
	}
	return "push failed"
}

// atoi is the tolerant number reader the review flow needs: a line number out
// of a string field becomes zero when it is not a number.
func atoi(text string) int {
	number, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0
	}
	return number
}
