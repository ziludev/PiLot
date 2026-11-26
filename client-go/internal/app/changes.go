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

	"pilot/internal/i18n"
	"pilot/internal/icons"
	"pilot/internal/protocol"
	"pilot/internal/theme"
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

	// flow is where this project's work walks (提测 target, release target) and
	// flowEdit is which of the two the footer's field is editing.
	flow        protocol.Workflow
	flowConv    int
	flowProject string
	flowBusy    bool
	flowEdit    string
	flowDraft   string

	// countW is the 改动 column's width, and dragFrom is where a drag on its
	// edge began: the header's handle writes here, the rows read it.
	countW   float32
	dragFrom float32

	// tab is which half of the drawer is showing: the patch, or the branches.
	// Both answer questions about the same worktree, and a 460-point drawer has
	// room for one at a time.
	tab string

	// branches is the branch list behind the second tab: the checkout's refs,
	// each measured from base, plus the two moves the drawer offers.
	branches     []protocol.BranchRef
	branchBase   string
	branchCur    string
	branchBusy   bool
	branchFailed string
	branchNaming bool
	branchDraft  string
	branchList   ui.ListState
	// branchConv is the chat the list was read for; a refresh for another chat
	// must not land in this one's drawer.
	branchConv int
	// toast is the last write's outcome, shown once above the branch list.
	toast string

	// mr is the open request behind the third tab, read from GitLab through the
	// bridge. mrConv is the chat it was read for, so a late answer for another
	// chat cannot land in this drawer.
	mr           protocol.MRStatus
	mrConv       int
	mrBusy       bool
	mrFailed     string
	mrTitle      string
	mrDraftTitle bool
	mrSquash     bool

	// pipeline is the CI tab's last read, and the job log under it. ciConv is
	// the chat it was read for, like mrConv.
	pipeline    *protocol.Pipeline
	ciConv      int
	ciBusy      bool
	ciFailed    string
	ciList      ui.ListState
	ciWriteBusy bool
	openJob     int
	ciLog       string
	ciLogCut    bool
	ciLogBusy   bool
	ciLogFailed string
	logList     ui.ListState

	// branchRenaming is the chat's own branch being named: an unpushed branch is
	// renamed in place, one that exists is attached.
	branchRenaming  bool
	branchNameDraft string

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
		a.tl.open = false
		a.refreshChanges()
		if a.changes.tab == tabBranches {
			a.refreshBranches()
		}
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
		Radius(theme.RadiusWindow).Border(1, t.Line).Background(t.Surface).Clip().
		Children(func() {
			a.changesHeader(c, t)
			a.changesTabs(c, t)
			ui.Box(c).Width(changesWidth).Height(1).Background(t.Line)
			if a.changes.failed != "" {
				ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
					ui.Text(c, a.changes.failed).FontSize(theme.TextSmall).TextColor(t.Destructive).MaxLines(4)
				})
			}
			if a.changes.tab == tabBranches {
				a.branchesBody(c, t)
				return
			}
			if a.changes.tab == tabMerge {
				a.mergeBody(c, t)
				return
			}
			if a.changes.tab == tabPipeline {
				a.pipelineBody(c, t)
				return
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
	// DragWindow: the drawer sits at the top too, so its header drags as well.
	ui.Row(c).FillWidth().Height(48).Padding(0, 12).AlignItems(ui.Center).Gap(8).
		DragWindow().Children(func() {
		// The workspace, not the tab: the tabs below already say which half this
		// is, and naming the repository is what tells a person where they are.
		title := a.changes.workspace
		if title == "" {
			title = i18n.T("changesTab")
		}
		ui.Text(c, title).FontSize(theme.TextTitle).FontWeight(500).TextColor(t.Ink).MaxLines(1)
		if branch := a.changes.branch; branch != "" {
			ui.Text(c, branch).Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
		}
		ui.Spacer(c)
		refresh := ui.ButtonBase(c).Size(20, 20).Radius(theme.RadiusChip).Justify(ui.Center).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Tooltip(i18n.T("refresh"))
		refresh.Children(func() {
			ui.Icon(c, icons.RefreshCw).Size(11, 11).TextColor(t.Ink3)
		})
		refresh.OnClick(func() {
			a.refreshChanges()
			if a.changes.tab == tabBranches {
				a.refreshBranches()
			}
		})
		// The way out of the diff: the files themselves, in the editor the user
		// already has open for everything else.
		editor := ui.ButtonBase(c).Size(20, 20).Radius(theme.RadiusChip).Justify(ui.Center).AlignItems(ui.Center).
			Cursor(ui.CursorPointer).Tooltip(i18n.T("openInEditor"))
		editor.Children(func() {
			ui.Icon(c, icons.Code).Size(11, 11).TextColor(t.MutedFg)
		})
		editor.OnClick(func() { a.OpenInEditor() })
		close := ui.ButtonBase(c).Size(20, 20).Radius(theme.RadiusChip).Justify(ui.Center).AlignItems(ui.Center).
			Cursor(ui.CursorPointer)
		close.Children(func() {
			ui.Text(c, "×").FontSize(theme.TextTitle).TextColor(t.MutedFg)
		})
		close.OnClick(func() { a.changes.open = false })
	})
}

// changesFileHead is the row above a patch: the way back, and what is open.
func (a *App) changesFileHead(c *ui.Context, t theme.Tokens, title string) {
	ui.Row(c).FillWidth().Height(32).Padding(0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		back := ui.ButtonBase(c).Height(20).Radius(theme.RadiusChip).Padding(0, 6).Cursor(ui.CursorPointer)
		back.Children(func() {
			ui.Text(c, i18n.T("back")).FontSize(theme.TextMicro).TextColor(t.MutedFg)
		})
		back.OnClick(func() {
			a.changes.openFile, a.changes.lines = "", nil
		})
		ui.Text(c, title).FontSize(theme.TextMicro).TextColor(t.Foreground).MaxLines(1).Grow(1)
		// A file has text behind it; the patch is what the drawer opened on, so
		// the switch back and forth is always offered here.
		if a.changes.openFile != "" {
			for index, label := range [2]string{i18n.T("changesPatch"), i18n.T("changesWhole")} {
				at, text := index == 1, label
				on := a.changes.whole == at
				btn := ui.ButtonBase(c).Height(20).Radius(theme.RadiusChip).Padding(0, 6).
					Cursor(ui.CursorPointer)
				if on {
					btn = btn.Background(t.Secondary)
				}
				btn.Children(func() {
					color, weight := t.MutedFg, 500
					if on {
						color, weight = t.Foreground, 600
					}
					ui.Text(c, text).FontSize(theme.TextMicro).FontWeight(weight).TextColor(color).MaxLines(1)
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
			ui.Text(c, i18n.T("loading")).FontSize(theme.TextSmall).TextColor(t.MutedFg)
		})
		return
	case len(a.changes.text) == 0:
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("fileEmpty")).FontSize(theme.TextSmall).TextColor(t.MutedFg).MaxLines(2)
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
				Font(monoFont).FontSize(theme.TextMicro).TextColor(t.Foreground).
				MaxLines(1).Padding(0, 4)
		}).Grow(1).Width(changesWidth - 16)
	})
	if a.changes.textCut {
		ui.Text(c, i18n.T("fileTruncated")).FontSize(theme.TextMicro).TextColor(t.MutedFg).Padding(4, 12)
	}
}

func (a *App) changesBody(c *ui.Context, t theme.Tokens) {
	if len(a.changes.files) == 0 {
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("changesNone")).FontSize(theme.TextSmall).TextColor(t.MutedFg).MaxLines(2)
		})
		return
	}
	ui.Column(c).FillWidth().Padding(0, 12).Grow(1).Children(func() {
		// The header is what turns the list into a table: a column of counts
		// with no name over it is a column the reader has to decode. Its metrics
		// are Records Table's: 35 tall, 12.5 at 500, the second ink.
		ui.Row(c).FillWidth().Height(35).Padding(0, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("changeFiles")).FontSize(theme.TextRow).FontWeight(500).
				TextColor(t.Ink2).Grow(1)
			a.countResizeHandle(c, t)
			ui.Row(c).Width(a.countColWidth()).Justify(ui.End).Children(func() {
				ui.Text(c, i18n.T("changeStat")).FontSize(theme.TextRow).FontWeight(500).
					TextColor(t.Ink2).MaxLines(1)
			})
		})
		ui.Box(c).FillWidth().Height(1).Background(t.Line)
		ui.List(c, &a.changes.fileList, len(a.changes.files), func(row int) {
			a.changeFileRow(c, t, a.changes.files[row])
		}).Grow(1).Width(changesWidth - 24)
	})
	a.changesTotals(c, t)
	if a.changes.truncated {
		ui.Text(c, i18n.T("changesTruncated")).FontSize(theme.TextMicro).TextColor(t.MutedFg).Padding(4, 12)
	}
}

// The 改动 column: 64 to begin with, dragged from its header edge, and never so
// narrow that "+128" wraps or so wide that the file names lose their room.
const (
	changeCountWidth = 64
	changeCountMin   = 48
	changeCountMax   = 200
)

// countColWidth is that width. The zero value means nobody has dragged it.
func (a *App) countColWidth() float32 {
	if a.changes.countW <= 0 {
		return changeCountWidth
	}
	return a.changes.countW
}

// countResizeHandle is the 12-point grab strip on the column's left edge —
// Records Table's resize handle, and dragging left widens the column.
func (a *App) countResizeHandle(c *ui.Context, t theme.Tokens) {
	handle := ui.ButtonBase(c).Width(12).Height(35).Cursor(ui.CursorResizeColumn)
	if handle.Hovered() || handle.Dragging() {
		handle = handle.Children(func() {
			ui.Box(c).Width(2).FillHeight().Radius(1).Background(t.LineStrong)
		})
	}
	if handle.Dragging() {
		if a.changes.dragFrom == 0 {
			a.changes.dragFrom = a.countColWidth()
		}
		width := a.changes.dragFrom - handle.DragOffset().X
		if width < changeCountMin {
			width = changeCountMin
		}
		if width > changeCountMax {
			width = changeCountMax
		}
		a.changes.countW = width
	} else if a.changes.dragFrom != 0 {
		a.changes.dragFrom = 0
	}
}

// changesTotals is the table's foot: how many files, and what they add up to.
// A Records Table carries a summary row; without one the counts are per row and
// never summed, which is the question a reader asks first.
func (a *App) changesTotals(c *ui.Context, t theme.Tokens) {
	added, removed := 0, 0
	for _, file := range a.changes.files {
		added += file.Added
		removed += file.Removed
	}
	ui.Box(c).FillWidth().Height(1).Background(t.Line)
	ui.Row(c).FillWidth().Height(35).Padding(0, 12).Gap(6).AlignItems(ui.Center).
		Background(t.Inset).Children(func() {
		ui.Text(c, i18n.TF("changeSum", map[string]any{"files": len(a.changes.files)})).
			FontSize(theme.TextTitle).FontWeight(500).TextColor(t.Ink2).Grow(1).MaxLines(1)
		if added > 0 {
			ui.Text(c, "+"+strconv.Itoa(added)).FontSize(theme.TextRow).TextColor(t.Green)
		}
		if removed > 0 {
			ui.Text(c, "−"+strconv.Itoa(removed)).FontSize(theme.TextRow).TextColor(t.Red)
		}
	})
}

func (a *App) changeFileRow(c *ui.Context, t theme.Tokens, file protocol.FileChange) {
	// A Records Table cell is 35 tall with 8 of side padding, 13 at 500.
	row := ui.Row(c).FillWidth().Height(35).Gap(8).Padding(0, 8).AlignItems(ui.Center).
		Cursor(ui.CursorPointer)
	if row.Hovered() {
		row = row.Background(t.Hover)
	}
	row.Children(func() {
		ui.Text(c, changeLetter(file.Status)).FontSize(theme.TextRow).FontWeight(500).
			TextColor(changeColor(t, file.Status)).Width(12)
		ui.Text(c, file.Path).FontSize(theme.TextBody).FontWeight(500).TextColor(t.Ink).
			MaxLines(1).Grow(1)
		counts := ui.Row(c).Width(a.countColWidth()).Gap(4).Justify(ui.End).Children(func() {
			// Added and removed are two facts, not a fraction: the colour says
			// which is which without a slash between them.
			if file.Added > 0 {
				ui.Text(c, "+"+strconv.Itoa(file.Added)).FontSize(theme.TextRow).
					TextColor(t.Green).MaxLines(1)
			}
			if file.Removed > 0 {
				ui.Text(c, "−"+strconv.Itoa(file.Removed)).FontSize(theme.TextRow).
					TextColor(t.Red).MaxLines(1)
			}
		})
		_ = counts
	})
	path := file.Path
	row.OnClick(func() { a.openChangeFile(path) })
}

// diffView is the patch itself: one line per row, colour by what the line does.
// The list virtualises, because a re-generated lock file is thousands of lines.
func (a *App) diffView(c *ui.Context, t theme.Tokens) {
	if len(a.changes.lines) == 0 {
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("diffNone")).FontSize(theme.TextSmall).TextColor(t.MutedFg).MaxLines(2)
		})
		return
	}
	lines := a.changes.lines
	ui.Column(c).FillWidth().Padding(0, 8).Grow(1).Children(func() {
		ui.List(c, &a.changes.diffList, len(lines), func(row int) {
			// Diff Table tints the row under the pointer, so a long patch can be
			// read across without following the line with a fingertip.
			line := ui.Row(c).FillWidth().Padding(0, 4).Radius(2)
			if line.Hovered() {
				line = line.Background(t.Hover)
			}
			line.Children(func() {
				ui.Text(c, lines[row]).Font(monoFont).FontSize(theme.TextMicro).
					TextColor(diffColor(t, lines[row])).MaxLines(0)
			})
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
