package side

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// ReviewNote is one review comment the reader staged on a line of a file.
type ReviewNote struct {
	Path string
	Line string
	Note string
}

// ChangesProps is the changes tab: the worktree's uncommitted files, the
// review notes staged for the agent, and the buttons that commit.
type ChangesProps struct {
	Changes      *protocol.Changes
	Loading      bool
	Review       []ReviewNote
	OnReload     func()
	OnOpenFile   func(path string)
	OnStageNote  func(path, line, note string)
	OnDropNote   func(index int)
	OnClearNotes func()
	OnSendReview func()
	OnCommit     func() // opens the commit dialog

	// Note is the review text being typed and Picked names the line it will
	// be staged on. The app owns both: these views keep no state of their
	// own, and an input bound to a value rebuilt each frame would lose the
	// text between frames.
	Note   *string
	Picked *ReviewNote
}

// ChangesView draws the changes tab: a header with the summary and the
// commit/merge actions, the file list, and the review notes below it.
func ChangesView(c *ui.Context, p ChangesProps) {
	t := theme.Current(c)
	changes := p.Changes
	if changes == nil {
		if p.Loading {
			ui.Text(c, "…").FontSize(11).TextColor(t.MutedFg).Padding(12, 12)
		}
		return
	}
	if !changes.Repo {
		emptyPanel(c, t, i18n.T("noChanges"), i18n.T("noChangesHint"))
		return
	}
	ui.Column(c).Fill().Children(func() {
		changesHeader(c, t, p)
		FilesView(c, p)
		reviewArea(c, t, p)
	})
}

// changesHeader is the summary line and the actions that do not belong to one
// file: reload, commit and merge.
func changesHeader(c *ui.Context, t theme.Tokens, p ChangesProps) {
	changes := p.Changes
	summary := fmt.Sprintf("%d", len(changes.Files)) + " " + i18n.T("files")
	if changes.Truncated {
		summary += " · " + i18n.T("truncated")
	}
	if changes.Branch != "" {
		summary += " · " + changes.Branch
	}
	ui.Row(c).FillWidth().Padding(8, 10).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, summary).FontSize(10).TextColor(t.MutedFg).SingleLine().TruncateMiddle().Grow(1)
		iconButton(c, icons.RotateCw, i18n.T("refresh"), t.MutedFg, p.OnReload)
		if p.OnCommit != nil {
			ui.Button(c, i18n.T("commit")).OnClick(p.OnCommit)
		}
	})
	if changes.Error != "" {
		ui.Text(c, changes.Error).FontSize(10).TextColor(t.Destructive).Wrap().Padding(2, 10)
	}
}

// FilesView draws the changed files as rows: status badge, path and counts. A
// click opens the file.
func FilesView(c *ui.Context, p ChangesProps) {
	t := theme.Current(c)
	if p.Changes == nil {
		return
	}
	files := p.Changes.Files
	if len(files) == 0 {
		emptyPanel(c, t, i18n.T("noChanges"), i18n.T("noChangesHint"))
		return
	}
	st := stateFor(c)
	ui.List(c, &st.files, len(files), func(i int) {
		file := files[i]
		row := ui.Row(c).FillWidth().Height(28).PaddingX(6).Gap(8).Radius(6).Cursor(ui.CursorPointer)
		if row.Hovered() {
			row.Background(c.Theme().SurfaceHover)
		}
		row.Children(func() {
			ui.Badge(c, statusLetter(file.Status)).FontSize(9).
				TextColor(statusTint(c, t, file.Status)).Background(t.Muted).
				PaddingX(4).Height(16).AlignSelf(ui.Center).Tooltip(statusLabel(file.Status))
			ui.Text(c, file.Path).Font("monospace").FontSize(11).TextColor(t.Foreground).
				SingleLine().TruncateMiddle().Grow(1)
			changeStats(c, t, file.Added, file.Removed)
		})
		if row.RowClicked() && p.OnOpenFile != nil {
			p.OnOpenFile(file.Path)
		}
	}).Grow(1).FillWidth()
}

// reviewArea is the notes staged for the agent: the list, the box that types a
// note on the picked line, and the button that sends them.
func reviewArea(c *ui.Context, t theme.Tokens, p ChangesProps) {
	editing := p.Picked != nil && p.Note != nil && p.OnStageNote != nil
	if len(p.Review) == 0 && !editing {
		return
	}
	ui.Column(c).FillWidth().Padding(8, 10).Gap(6).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.Row(c).FillWidth().Children(func() {
			ui.Text(c, i18n.T("review")+" · "+fmt.Sprintf("%d", len(p.Review))).
				FontSize(10).TextColor(t.MutedFg).Grow(1)
			iconButton(c, icons.Trash, i18n.T("clearReview"), t.MutedFg, p.OnClearNotes)
		})
		for i, note := range p.Review {
			i, note := i, note
			row := ui.Row(c).FillWidth().Height(20).PaddingX(2).Gap(4).Radius(4).Cursor(ui.CursorPointer)
			if row.Hovered() {
				row.Background(c.Theme().SurfaceHover)
			}
			row.Children(func() {
				ui.Text(c, note.Path).Font("monospace").FontSize(10).TextColor(t.MutedFg).
					SingleLine().TruncateMiddle().MaxWidth(140)
				ui.Text(c, "→").FontSize(10).TextColor(t.Border)
				ui.Text(c, note.Note).FontSize(10).TextColor(t.Foreground).
					SingleLine().TruncateMiddle().Grow(1)
			})
			if row.RowClicked() && p.OnDropNote != nil {
				p.OnDropNote(i)
			}
		}
		if editing {
			ui.Text(c, p.Picked.Path+" · "+truncateRunes(strings.TrimSpace(p.Picked.Line), 60)).
				Font("monospace").FontSize(10).TextColor(t.MutedFg).SingleLine().TruncateMiddle()
			ui.TextInput(c, p.Note).FillWidth().Height(26).FontSize(11).
				Placeholder(i18n.T("review")).OnTextKey(func(k ui.TextKey) bool {
				// Enter stages the note; Shift+Enter keeps a newline.
				if k.Key != ui.KeyEnter || k.Mods&ui.Shift != 0 {
					return false
				}
				note := strings.TrimSpace(k.Text())
				if note == "" {
					return false
				}
				k.SetText("")
				*p.Note = ""
				p.OnStageNote(p.Picked.Path, p.Picked.Line, note)
				return true
			})
		}
		if len(p.Review) > 0 && p.OnSendReview != nil {
			ui.PrimaryButton(c, i18n.T("sendReview")).FillWidth().OnClick(p.OnSendReview)
		}
	})
}

// emptyPanel says why a list is blank, as the rail's empty states do.
func emptyPanel(c *ui.Context, t theme.Tokens, title, hint string) {
	ui.Column(c).Grow(1).FillWidth().Padding(16, 12).Gap(3).Children(func() {
		ui.Text(c, title).FontSize(11).TextColor(t.MutedFg).Wrap()
		if hint != "" {
			ui.Text(c, hint).FontSize(10).TextColor(t.MutedFg).Wrap()
		}
	})
}

// truncateRunes cuts s to max runes, keeping the ellipsis inside the budget.
func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return fmt.Sprintf("%s…", string(runes[:max]))
}
