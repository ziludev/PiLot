package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// The drawer's write actions. The window only asks; the work is the bridge's,
// because a merge half-done in a client is worse than one the bridge refused.
// Every answer comes back as text to read rather than as state to infer: a
// conflict is a list of files, and it has to stay on screen.
type writeKind string

const (
	writeCommit writeKind = "commit"
	writePush   writeKind = "push"
	writeMerge  writeKind = "merge"
)

type writePanel struct {
	// open is the dialog that asks first, kind is the write it asks about.
	open    bool
	kind    writeKind
	message string
	// running is the write in flight, "" when the worktree is idle.
	running writeKind
	// conv is the chat the result belongs to: a chat's commits are not the
	// next chat's news.
	conv int
	// result is the last answer, kept until the next write.
	result string
	bad    bool
}

// canWrite is whether the drawer offers a write at all: what it would act on
// has to exist, and two writes on one worktree at once is not a thing.
func (a *App) canWrite(kind writeKind) bool {
	if a.active == 0 || a.write.running != "" || a.changes.conv != a.active {
		return false
	}
	switch kind {
	case writeCommit:
		return len(a.changes.files) > 0
	case writePush:
		return a.changes.branch != "" && a.changes.remoteKind != "" && a.changes.remoteKind != "none"
	case writeMerge:
		return a.changes.base != "" && a.changes.base != a.changes.branch
	}
	return false
}

// openWrite puts up the question the write asks first.
func (a *App) openWrite(kind writeKind) {
	if !a.canWrite(kind) {
		return
	}
	a.write.open, a.write.kind = true, kind
	a.write.message = ""
	a.invalidate()
}

func (a *App) closeWrite() {
	a.write.open = false
	a.invalidate()
}

// confirmWrite starts the write the dialog asked about. The dialog closes on
// the spot: a commit takes a second, and a spinner sitting where the files were
// hides the thing being committed.
func (a *App) confirmWrite() {
	kind, message := a.write.kind, strings.TrimSpace(a.write.message)
	if kind == "" || !a.canWrite(kind) {
		return
	}
	if kind == writeCommit && message == "" {
		return
	}
	conv := a.active
	a.write.open = false
	a.write.message = ""
	a.write.running = kind
	a.write.result, a.write.bad = "", false
	go func() {
		// Long enough for a push to a slow remote; the bridge gives up first.
		ctx, cancel := context.WithTimeout(a.ctx, 150*time.Second)
		defer cancel()
		text, bad := a.runWrite(ctx, conv, kind, message)
		a.post(event{kind: evWrite, convID: conv, writeKind: kind, text: text, writeBad: bad})
	}()
	a.invalidate()
}

// runWrite performs one write and turns whatever came back into something a
// person can act on.
func (a *App) runWrite(ctx context.Context, conv int, kind writeKind, message string) (string, bool) {
	switch kind {
	case writeCommit:
		out, err := a.api.Commit(ctx, conv, message)
		if err != nil {
			return failed(err), true
		}
		return i18n.TF("commitDone", map[string]any{"head": head(out.Head)}), false
	case writePush:
		out, err := a.api.Push(ctx, conv, false)
		if err != nil {
			return failed(err), true
		}
		switch out.Status {
		case "pushed":
			return i18n.TF("pushDone", map[string]any{"branch": out.Branch}), false
		case "no-remote":
			return i18n.T("pushNoRemote"), true
		}
		return failedString(i18n.T("pushFailed") + ": " + out.Message), true
	case writeMerge:
		out, err := a.api.Merge(ctx, conv)
		if err != nil {
			return failed(err), true
		}
		return mergeOutcome(out)
	}
	return "", false
}

// applyWrite shows what the bridge answered and, when the worktree moved,
// re-reads it rather than patching the drawer from the reply.
func (a *App) applyWrite(convID int, kind writeKind, text string, bad bool) {
	if a.write.running == kind {
		a.write.running = ""
	}
	if convID != a.active {
		return
	}
	a.write.conv = convID
	a.write.result, a.write.bad = text, bad
	if !bad && kind != writePush {
		// The branch's commits changed with the commit, not before it.
		a.changes.commits = nil
		a.refreshChanges()
	}
}

// mergeOutcome reads the bridge's refusal as the instructions it is.
func mergeOutcome(out api.MergeResult) (string, bool) {
	base := out.Base
	switch out.Status {
	case "merged":
		return i18n.TF("mergeDone", map[string]any{"n": out.Commits, "base": base}), false
	case "up-to-date":
		return i18n.T("mergeUpToDate"), false
	case "dirty":
		return i18n.TF("mergeDirty", map[string]any{"files": list(out.Files)}), true
	case "main-dirty":
		return i18n.TF("baseDirty", map[string]any{"base": base, "files": list(out.Files)}), true
	case "conflict":
		return i18n.TF("mergeConflict", map[string]any{"files": list(out.Conflicts)}), true
	}
	if out.Message == "" {
		out.Message = i18n.T("mergeFailed")
	}
	return failedString(out.Message), true
}

func failed(err error) string { return failedString(err.Error()) }

func failedString(reason string) string {
	return i18n.T("writeFailed") + " · " + reason
}

// list names a handful of files and counts the rest: a branch touching eighty
// files should not push the drawer's buttons off the bottom.
func list(files []string) string {
	if len(files) == 0 {
		return i18n.T("noFiles")
	}
	shown := files
	if len(shown) > 4 {
		shown = shown[:4]
	}
	out := strings.Join(shown, "、")
	if rest := len(files) - len(shown); rest > 0 {
		out += i18n.TF("andMore", map[string]any{"n": rest})
	}
	return out
}

// head is a commit hash as a person quotes it.
func head(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}

// writeFooter is the drawer's row of verbs, and the last answer under them.
func (a *App) writeFooter(c *ui.Context, t theme.Tokens) {
	ui.Box(c).FillWidth().Height(1).Background(t.Border)
	ui.Column(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			a.writeButton(c, t, writeCommit, i18n.T("commitAction"), true)
			a.writeButton(c, t, writePush, i18n.T("pushAction"), false)
			a.writeButton(c, t, writeMerge, i18n.T("mergeAction"), false)
		})
		if a.write.running != "" {
			ui.Text(c, i18n.T("writeRunning")).FontSize(10.5).TextColor(t.MutedFg).MaxLines(2)
		}
		if a.write.conv == a.active && a.write.result != "" {
			color := t.MutedFg
			if a.write.bad {
				color = t.Destructive
			}
			ui.Text(c, a.write.result).FontSize(10.5).TextColor(color).MaxLines(8)
		}
	})
}

// writeButton is one verb. A verb whose work is not there stays visible but
// quiet: the row reads the same in every chat, and a missing button is a
// question about whether it was ever there.
func (a *App) writeButton(c *ui.Context, t theme.Tokens, kind writeKind, label string, primary bool) {
	enabled := a.canWrite(kind) && a.write.running == ""
	button := ui.ButtonBase(c).Grow(1).Height(26).Radius(6).
		Justify(ui.Center).AlignItems(ui.Center)
	if primary && enabled {
		button = button.Background(t.Primary)
	} else {
		button = button.Background(t.SidebarAccent)
	}
	if enabled {
		button = button.Cursor(ui.CursorPointer)
	}
	button.Children(func() {
		color := t.MutedFg
		if enabled {
			color = t.Foreground
		}
		if primary && enabled {
			color = t.PrimaryFg
		}
		ui.Text(c, label).FontSize(11).FontWeight(500).TextColor(color).MaxLines(1)
	})
	if enabled {
		at := kind
		button.OnClick(func() { a.openWrite(at) })
	}
}

// writeDialog asks the one question each write has.
func (a *App) writeDialog(c *ui.Context, t theme.Tokens) {
	if !a.write.open {
		return
	}
	kind := a.write.kind
	ui.Modal(c, &a.write.open, func() {
		ui.Column(c).Width(360).Padding(16).Gap(10).Radius(10).
			Background(t.Popover).Border(1, t.Border).Children(func() {
			ui.Text(c, a.writeTitle(kind)).
				FontSize(12.5).FontWeight(600).TextColor(t.Foreground)
			ui.Text(c, a.writeHint(kind)).FontSize(10.5).TextColor(t.MutedFg).MaxLines(3)
			if kind == writeCommit {
				input := ui.TextInputBase(c, &a.write.message).FontSize(12).Height(30).
					Padding(0, 8).Radius(6).Background(t.SidebarAccent).
					Placeholder(i18n.T("commitPlaceholder"))
				input.OnSubmit(func() { a.confirmWrite() })
			}
			ui.Row(c).FillWidth().Justify(ui.End).Gap(8).Children(func() {
				a.dialogButton(c, t, i18n.T("cancel"), false, a.closeWrite)
				a.dialogButton(c, t, a.writeLabel(kind), true, a.confirmWrite)
			})
		})
	})
}

func (a *App) writeTitle(kind writeKind) string {
	switch kind {
	case writeCommit:
		return i18n.T("commitTitle")
	case writePush:
		return i18n.T("pushTitle")
	}
	return i18n.T("mergeTitle")
}

func (a *App) writeLabel(kind writeKind) string {
	switch kind {
	case writeCommit:
		return i18n.T("commitAction")
	case writePush:
		return i18n.T("pushAction")
	}
	return i18n.T("mergeAction")
}

// writeHint says what the write would do, in the words of this worktree: the
// file it would take, the branch it would send, where it would land.
func (a *App) writeHint(kind writeKind) string {
	switch kind {
	case writeCommit:
		return i18n.TF("commitHint", map[string]any{
			"branch": a.changes.branch,
			"n":      len(a.changes.files),
		})
	case writePush:
		return i18n.TF("pushHint", map[string]any{"branch": a.changes.branch})
	}
	return i18n.TF("mergeHint", map[string]any{
		"branch": a.changes.branch,
		"base":   a.changes.base,
	})
}
