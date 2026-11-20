package app

// The changes drawer: what the agent changed in the chat's worktree, and the
// patch behind it.
//
// It answers one question in two depths: the file list says what moved, the
// patch says how. Picking a file replaces the list with its lines and a way
// back — a 460-point drawer has no room for two columns. The branch's commit
// log used to sit here too; it answered a different question ("what already
// left") that git log and the transcript already answer, so it went.
//
// Read-only by design. Committing, pushing and merging are writes that need the
// approval path, and the rail's chat menu is where those belong; a panel that
// silently rewrote the worktree would be the wrong first version.

import (
	"context"

	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

const (
	// changesWidth fits a diff line at 10.5pt and still leaves the transcript
	// more than half the window.
	changesWidth = 460
	// gitLimit is how much of the branch's log to ask for. The drawer wants the
	// branch, the base and the remote out of that reply, not the history: one
	// commit is enough to make the bridge's own read small.
	gitLimit = 1
)

// monoFont is what the transcript's code blocks use; a diff is code.
const monoFont = "SF Mono, Menlo, monospace"

type changesPanel struct {
	open   bool
	conv   int
	busy   bool
	failed string

	branch string
	// base and remote are what the write verbs would act on: where a merge
	// lands, and whether there is anything to push to.
	base       string
	remoteKind string
	workspace  string
	files      []protocol.FileChange
	patch      string
	truncated  bool

	// openFile is the file whose patch fills the drawer, "" for the list.
	openFile string
	lines    []string

	// whole is whether the drawer shows the file rather than its patch. The
	// patch answers "what moved"; the file answers "what does it say now", and
	// reading a diff for that is guesswork.
	whole    bool
	textPath string
	text     []string
	textCut  bool
	textBusy bool

	// gitConv is the chat whose branch and base were read; 0 for none. The
	// drawer needs them for the write verbs, and they outlive a refresh.
	gitConv int

	fileList ui.ListState
	diffList ui.ListState
}

// toggleChanges opens the drawer, or closes it. The two drawers are the same
// right edge, so opening one shuts the other: two columns of 460 in a 1180-point
// window would leave the transcript a stripe.
func (a *App) toggleChanges() {
	a.changes.open = !a.changes.open
	if a.changes.open {
		// One right edge, one drawer.
		a.mem.open = false
		a.projects.open = false
		a.refreshChanges()
	}
	a.invalidate()
}

// refreshChanges reads the worktree patch and, for a chat it has not seen yet,
// the branch the write verbs act on.
func (a *App) refreshChanges() {
	if a.active == 0 {
		a.changesClear("")
		return
	}
	conv := a.active
	a.changes.conv = conv
	a.changes.failed = ""
	a.changes.busy = true
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		changes, err := a.api.Changes(ctx, conv)
		a.post(event{kind: evChanges, convID: conv, changes: changes, err: err})
	}()
	if a.changes.gitConv != conv {
		a.changes.gitConv = conv
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
			defer cancel()
			git, err := a.api.Git(ctx, conv, gitLimit)
			a.post(event{kind: evGit, convID: conv, git: git, err: err})
		}()
	}
}

// changesClear empties the drawer. The reason, when there is one, is shown in
// place of the list rather than swallowed.
func (a *App) changesClear(reason string) {
	a.changes.busy = false
	a.changes.failed = reason
	a.changes.files = nil
	a.changes.patch = ""
	a.changes.openFile = ""
	a.changes.lines = nil
}

func (a *App) applyChanges(convID int, changes *protocol.Changes, err error) {
	if convID != a.active {
		return
	}
	a.changes.busy = false
	if err != nil {
		a.changes.failed = err.Error()
		return
	}
	a.changes.failed = changes.Error
	a.changes.branch = changes.Branch
	a.changes.workspace = changes.Workspace
	a.changes.files = changes.Files
	a.changes.patch = changes.Diff
	a.changes.truncated = changes.Truncated
	// A patch's open file can vanish between reads (the turn committed it, or
	// the agent moved it); fall back to the list rather than an empty pane.
	if a.changes.openFile != "" && filePatch(changes.Diff, a.changes.openFile) == nil {
		a.changes.openFile, a.changes.lines = "", nil
	}
	if a.changes.openFile != "" {
		a.changes.lines = filePatch(changes.Diff, a.changes.openFile)
	}
}

func (a *App) applyGit(convID int, git *protocol.Git, err error) {
	if convID != a.active {
		return
	}
	if err != nil {
		// Let the next refresh ask again: the write verbs need the base, and a
		// drawer opened while the bridge was down would otherwise stay base-less.
		a.changes.gitConv = 0
		if a.changes.failed == "" {
			a.changes.failed = err.Error()
		}
		return
	}
	if git.Branch != "" {
		a.changes.branch = git.Branch
	}
	a.changes.base = git.Base
	a.changes.remoteKind = git.Remote.Kind
}

// openChangeFile shows one file's patch.
func (a *App) openChangeFile(path string) {
	lines := filePatch(a.changes.patch, path)
	a.changes.openFile = path
	a.changes.lines = lines
	a.changes.whole = false
	a.changes.text, a.changes.textPath = nil, ""
	a.changes.diffList = ui.ListState{}
	a.invalidate()
}

// showWholeFile switches the drawer between a file's patch and the file itself,
// reading the text the first time it is asked for.
func (a *App) showWholeFile(whole bool) {
	a.changes.whole = whole
	a.changes.diffList = ui.ListState{}
	if whole && a.changes.openFile != "" && a.changes.textPath != a.changes.openFile {
		a.loadFileText(a.changes.openFile)
	}
	a.invalidate()
}

// loadFileText reads one file from the workspace. textPath is set before the
// request so a second click on the same file does not start a second read.
func (a *App) loadFileText(path string) {
	conv := a.changes.conv
	a.changes.textPath = path
	a.changes.text = nil
	a.changes.textCut = false
	a.changes.textBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		file, err := a.api.File(ctx, conv, path)
		a.post(event{kind: evFile, convID: conv, path: path, fileText: file.Content, err: err, truncated: file.Truncated})
	}()
}

// applyFileText fills the preview, unless the drawer moved to another file or
// another chat while the text was in flight.
func (a *App) applyFileText(convID int, path, text string, truncated bool, err error) {
	if convID != a.changes.conv || path != a.changes.textPath {
		return
	}
	a.changes.textBusy = false
	if err != nil {
		a.changes.failed = err.Error()
		return
	}
	a.changes.failed = ""
	a.changes.text = strings.Split(strings.TrimRight(text, "\n"), "\n")
	a.changes.textCut = truncated
}

// changesView is the drawer.
func (a *App) changesView(c *ui.Context, t theme.Tokens) {
	// Shrinks on a narrow window: 260 of rail plus 460 of drawer would leave the
	// transcript nothing at the window's smallest width.
	ui.Column(c).Width(changesWidth).Shrink(1).MinWidth(300).FillHeight().
		Background(t.Sidebar).Children(func() {
		a.changesHeader(c, t)
		ui.Box(c).Width(changesWidth).Height(1).Background(t.SidebarBorder)
		if a.changes.failed != "" {
			ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
				ui.Text(c, a.changes.failed).FontSize(11).TextColor(t.Destructive).MaxLines(4)
			})
		}
		if a.changes.openFile != "" {
			a.changesFileHead(c, t, a.changes.openFile)
			if a.changes.whole {
				a.fileTextView(c, t)
			} else {
				a.diffView(c, t)
			}
		} else {
			a.changesBody(c, t)
			a.writeFooter(c, t)
		}
	})
}

func (a *App) changesHeader(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(44).Padding(0, 12).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, i18n.T("changesTab")).FontSize(12.5).FontWeight(600).TextColor(t.Foreground).MaxLines(1)
		if branch := a.changes.branch; branch != "" {
			ui.Text(c, branch).FontSize(10).TextColor(t.MutedFg).MaxLines(1)
		}
		ui.Spacer(c)
		refresh := ui.ButtonBase(c).Size(20, 20).Radius(4).Justify(ui.Center).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Tooltip(i18n.T("refresh"))
		refresh.Children(func() {
			ui.Icon(c, icons.Clock).Size(11, 11).TextColor(t.MutedFg)
		})
		refresh.OnClick(func() { a.refreshChanges() })
		// The way out of the diff: the files themselves, in the editor the user
		// already has open for everything else.
		editor := ui.ButtonBase(c).Size(20, 20).Radius(4).Justify(ui.Center).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Tooltip(i18n.T("openInEditor"))
		editor.Children(func() {
			ui.Icon(c, icons.Code).Size(11, 11).TextColor(t.MutedFg)
		})
		editor.OnClick(func() { a.OpenInEditor() })
		close := ui.ButtonBase(c).Size(20, 20).Radius(4).Justify(ui.Center).AlignItems(ui.Center).
			Cursor(ui.CursorPointer)
		close.Children(func() {
			ui.Text(c, "×").FontSize(14).TextColor(t.MutedFg)
		})
		close.OnClick(func() { a.changes.open = false })
	})
}

// changesFileHead is the row above a patch: the way back, and what is open.
func (a *App) changesFileHead(c *ui.Context, t theme.Tokens, title string) {
	ui.Row(c).FillWidth().Height(32).Padding(0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		back := ui.ButtonBase(c).Height(20).Radius(4).Padding(0, 6).Cursor(ui.CursorPointer)
		back.Children(func() {
			ui.Text(c, i18n.T("back")).FontSize(10.5).TextColor(t.MutedFg)
		})
		back.OnClick(func() {
			a.changes.openFile, a.changes.lines = "", nil
		})
		ui.Text(c, title).FontSize(10.5).TextColor(t.Foreground).MaxLines(1).Grow(1)
		// A file has text behind it; the patch is what the drawer opened on, so
		// the switch back and forth is always offered here.
		if a.changes.openFile != "" {
			for index, label := range [2]string{i18n.T("changesPatch"), i18n.T("changesWhole")} {
				at, text := index == 1, label
				on := a.changes.whole == at
				btn := ui.ButtonBase(c).Height(20).Radius(4).Padding(0, 6).
					Cursor(ui.CursorPointer)
				if on {
					btn = btn.Background(t.Secondary)
				}
				btn.Children(func() {
					color, weight := t.MutedFg, 500
					if on {
						color, weight = t.Foreground, 600
					}
					ui.Text(c, text).FontSize(10).FontWeight(weight).TextColor(color).MaxLines(1)
				})
				btn.OnClick(func() { a.showWholeFile(at) })
			}
		}
	})
}

// fileTextView is the file itself, numbered: a patch beside it says what
// changed, and the numbers are what a person quotes back to the agent.
func (a *App) fileTextView(c *ui.Context, t theme.Tokens) {
	switch {
	case a.changes.textBusy && a.changes.text == nil:
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("loading")).FontSize(11).TextColor(t.MutedFg)
		})
		return
	case len(a.changes.text) == 0:
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("fileEmpty")).FontSize(11).TextColor(t.MutedFg).MaxLines(2)
		})
		return
	}
	lines := a.changes.text
	gutter := len(strconv.Itoa(len(lines)))
	if gutter < 3 {
		gutter = 3
	}
	ui.Column(c).FillWidth().Padding(0, 8).Grow(1).Children(func() {
		ui.List(c, &a.changes.diffList, len(lines), func(row int) {
			// The number lives in the same string as the line: a text laid out
			// beside a growing sibling inside a list row is not painted, so a
			// separate gutter column would be invisible.
			number := strconv.Itoa(row + 1)
			// Right-aligned with a figure space: plain spaces at the start of a
			// line are trimmed away, which would leave the numbers hugging the
			// file's own indentation.
			number = strings.Repeat("\u2007", gutter-len(number)) + number
			ui.Text(c, number+"  "+lines[row]).
				Font(monoFont).FontSize(10.5).TextColor(t.Foreground).
				MaxLines(1).Padding(0, 4)
		}).Grow(1).Width(changesWidth - 16)
	})
	if a.changes.textCut {
		ui.Text(c, i18n.T("fileTruncated")).FontSize(10).TextColor(t.MutedFg).Padding(4, 12)
	}
}

func (a *App) changesBody(c *ui.Context, t theme.Tokens) {
	if len(a.changes.files) == 0 {
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("changesNone")).FontSize(11).TextColor(t.MutedFg).MaxLines(2)
		})
		return
	}
	ui.Column(c).FillWidth().Padding(0, 12).Grow(1).Children(func() {
		ui.List(c, &a.changes.fileList, len(a.changes.files), func(row int) {
			a.changeFileRow(c, t, a.changes.files[row])
		}).Grow(1).Width(changesWidth - 24)
	})
	if a.changes.truncated {
		ui.Text(c, i18n.T("changesTruncated")).FontSize(10).TextColor(t.MutedFg).Padding(4, 12)
	}
}

func (a *App) changeFileRow(c *ui.Context, t theme.Tokens, file protocol.FileChange) {
	row := ui.Row(c).FillWidth().Height(28).Gap(8).Padding(0, 4).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Radius(5)
	row.Children(func() {
		ui.Text(c, changeLetter(file.Status)).FontSize(10).FontWeight(600).
			TextColor(changeColor(t, file.Status)).Width(12)
		ui.Text(c, file.Path).FontSize(11).TextColor(t.Foreground).MaxLines(1).Grow(1)
		if file.Added > 0 || file.Removed > 0 {
			ui.Text(c, changeCounts(file)).FontSize(10).TextColor(t.MutedFg).MaxLines(1)
		}
	})
	path := file.Path
	row.OnClick(func() { a.openChangeFile(path) })
}

// diffView is the patch itself: one line per row, colour by what the line does.
// The list virtualises, because a re-generated lock file is thousands of lines.
func (a *App) diffView(c *ui.Context, t theme.Tokens) {
	if len(a.changes.lines) == 0 {
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("diffNone")).FontSize(11).TextColor(t.MutedFg).MaxLines(2)
		})
		return
	}
	lines := a.changes.lines
	ui.Column(c).FillWidth().Padding(0, 8).Grow(1).Children(func() {
		ui.List(c, &a.changes.diffList, len(lines), func(row int) {
			ui.Text(c, lines[row]).Font(monoFont).FontSize(10.5).
				TextColor(diffColor(t, lines[row])).MaxLines(0).Padding(0, 4)
		}).Grow(1).Width(changesWidth - 16)
	})
}

// diffColor is the one thing a diff has to say without words: added, removed,
// or the file's name and hunk headers, which are neither.
func diffColor(t theme.Tokens, line string) ui.Color {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"),
		strings.HasPrefix(line, "diff --git"), strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "new file"), strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "rename "), strings.HasPrefix(line, "similarity "):
		return t.MutedFg
	case strings.HasPrefix(line, "@@"):
		return t.Accent
	case strings.HasPrefix(line, "+"):
		return t.Success
	case strings.HasPrefix(line, "-"):
		return t.Destructive
	}
	return t.Foreground
}

// splitPatch cuts the worktree patch into per-file slices, keyed by the path
// after "diff --git a/… b/…". Untracked files arrive the same way: the bridge
// synthesises their sections with diff --no-index.
func splitPatch(patch string) map[string][]string {
	sections := map[string][]string{}
	var current string
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			current = patchPath(line)
			sections[current] = nil
		}
		if current != "" {
			sections[current] = append(sections[current], line)
		}
	}
	return sections
}

// patchPath is the b-side path of a diff header: the file as it is now.
func patchPath(line string) string {
	fields := strings.Fields(line)
	for index := len(fields) - 1; index >= 0; index-- {
		if strings.HasPrefix(fields[index], "b/") {
			return strings.TrimPrefix(fields[index], "b/")
		}
	}
	return ""
}

// filePatch is one file's slice, or nil when the patch does not mention it.
func filePatch(patch, path string) []string {
	for name, lines := range splitPatch(patch) {
		if name == path {
			return lines
		}
	}
	return nil
}

// changeLetter is git's one-letter summary of a file's state.
func changeLetter(status string) string {
	switch status {
	case "modified":
		return "M"
	case "added":
		return "A"
	case "deleted":
		return "D"
	case "renamed":
		return "R"
	case "untracked":
		return "?"
	case "conflicted":
		return "!"
	}
	if status == "" {
		return "·"
	}
	return strings.ToUpper(status[:1])
}

func changeColor(t theme.Tokens, status string) ui.Color {
	switch status {
	case "added", "untracked":
		return t.Success
	case "deleted", "conflicted":
		return t.Destructive
	}
	return t.MutedFg
}

// changeCounts is the +3 −1 every diff list shows, and nothing when both are 0:
// a binary file has no lines to count.
func changeCounts(file protocol.FileChange) string {
	switch {
	case file.Added > 0 && file.Removed > 0:
		return i18n.TF("changeCounts", map[string]any{"add": file.Added, "del": file.Removed})
	case file.Added > 0:
		return "+" + strconv.Itoa(file.Added)
	case file.Removed > 0:
		return "−" + strconv.Itoa(file.Removed)
	}
	return ""
}
