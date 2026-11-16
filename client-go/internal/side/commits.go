package side

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// CommitsProps is the branch's commit list, its compare link and the push
// button, with the patch of the open commit below the list.
type CommitsProps struct {
	Git      *protocol.Git
	Expanded string // hash whose diff is open
	Diff     string // that commit's patch
	OnOpen   func(hash string)
	OnClose  func()
	OnPush   func(dryRun bool)
	PushNote string
	// OnSetBase changes the branch the compare link and the patch count are
	// measured against (Commits.tsx:71).
	OnSetBase func(branch string)
}

// CommitsView draws the commits of the branch. A row opens its patch and
// closes it again on the next click.
func CommitsView(c *ui.Context, p CommitsProps) {
	t := theme.Current(c)
	git := p.Git
	if git == nil {
		return
	}
	ui.Column(c).Fill().Children(func() {
		commitsHeader(c, t, git, p)
		if len(git.Commits) == 0 {
			emptyPanel(c, t, i18n.T("noCommits"), i18n.T("noCommitsHint"))
			return
		}
		list := ui.List(c, &stateFor(c).commits, len(git.Commits), func(i int) {
			commitRow(c, t, git.Commits[i], p)
		}).FillWidth()
		if p.Expanded != "" {
			// Leave the list half the room while the patch is open, so both
			// stay reachable without closing the diff.
			list.Height(180)
			ui.Scroll(c).Grow(1).FillWidth().Children(func() {
				renderPatch(c, &stateFor(c).patch, p.Diff)
			})
		} else {
			list.Grow(1)
		}
	})
}

// commitsHeader is the branch line, the compare link and push, plus the note
// the last push left.
func commitsHeader(c *ui.Context, t theme.Tokens, git *protocol.Git, p CommitsProps) {
	branch := git.Branch
	if branch == "" {
		branch = git.Worktree
	}
	ui.Column(c).FillWidth().Children(func() {
		ui.Row(c).FillWidth().Padding(8, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, branch).Font("monospace").FontSize(10).TextColor(t.MutedFg).
				SingleLine().TruncateMiddle().Grow(1)
			ui.Text(c, "→").FontSize(10).TextColor(t.MutedFg)
			baseSelect(c, t, git, p)
			if git.CompareURL != "" {
				label := i18n.T("openPr")
				if git.Remote.Kind == "gitlab" {
					label = i18n.T("openMr")
				}
				ui.Button(c, label).OnClick(func() { c.OpenURL(git.CompareURL) })
			}
			iconButton(c, icons.Send, i18n.T("push"), t.MutedFg, func() {
				if p.OnPush != nil {
					p.OnPush(false)
				}
			})
		})
		if p.PushNote != "" {
			ui.Text(c, p.PushNote).FontSize(10).TextColor(t.MutedFg).
				SingleLine().TruncateMiddle().Padding(0, 10)
		}
	})
}

// commitRow is one commit: the open marker, the subject and the counts, and
// its short hash, date and file count below.
func commitRow(c *ui.Context, t theme.Tokens, commit protocol.Commit, p CommitsProps) {
	open := commit.Hash != "" && commit.Hash == p.Expanded
	row := ui.Column(c).FillWidth().Padding(4, 6).Gap(2).Radius(6).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row.Background(c.Theme().SurfaceHover)
	}
	row.Children(func() {
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			arrow := icons.ChevronRight
			if open {
				arrow = icons.ChevronDown
			}
			ui.Icon(c, arrow).Size(10, 10).TextColor(t.MutedFg)
			ui.Text(c, commit.Subject).FontSize(12).TextColor(t.Foreground).
				SingleLine().TruncateMiddle().Grow(1)
			changeStats(c, t, commit.Added, commit.Removed)
		})
		meta := []string{commit.Short}
		if date := commitWhen(commit.Date); date != "" {
			meta = append(meta, date)
		}
		if commit.Files > 0 {
			meta = append(meta, fmt.Sprintf("%d %s", commit.Files, i18n.T("files")))
		}
		ui.Text(c, strings.Join(meta, " · ")).Font("monospace").FontSize(10).
			TextColor(t.MutedFg).SingleLine().TruncateMiddle().PaddingX(16)
	})
	if row.RowClicked() {
		switch {
		case open && p.OnClose != nil:
			p.OnClose()
		case !open && p.OnOpen != nil:
			p.OnOpen(commit.Hash)
		}
	}
}

// commitWhen is how old a commit is, in the shortest form that still reads;
// a date this client cannot parse shows as the day git sent. The unit
// suffixes are git abbreviations with no i18n entry of their own.
func commitWhen(date string) string {
	if when, err := time.Parse(time.RFC3339, date); err == nil {
		age := time.Since(when)
		switch {
		case age < time.Minute:
			return "now"
		case age < time.Hour:
			return fmt.Sprintf("%dm", int(age.Minutes()))
		case age < 24*time.Hour:
			return fmt.Sprintf("%dh", int(age.Hours()))
		case age < 30*24*time.Hour:
			return fmt.Sprintf("%dd", int(age.Hours()/24))
		case age < 365*24*time.Hour:
			return fmt.Sprintf("%dmo", int(age.Hours()/(24*30)))
		default:
			return fmt.Sprintf("%dy", int(age.Hours()/(24*365)))
		}
	}
	if len(date) >= 10 {
		return date[:10]
	}
	return date
}

// baseSelect chooses the branch the chat is compared against: branched from any
// branch, so the base is a choice (Commits.tsx:71).
func baseSelect(c *ui.Context, t theme.Tokens, git *protocol.Git, p CommitsProps) {
	names := make([]string, 0, len(git.Branches))
	for _, branch := range git.Branches {
		names = append(names, branch.Name)
	}
	options := sortedBranches(names, git.Base)
	value := git.Base
	ui.Select(c, &value, options).Width(120).OnChange(func() {
		if p.OnSetBase != nil && value != git.Base {
			p.OnSetBase(value)
		}
	})
}

// sortedBranches puts the current base first, then the rest in their order.
func sortedBranches(branches []string, base string) []string {
	out := make([]string, 0, len(branches)+1)
	if base != "" {
		out = append(out, base)
	}
	for _, branch := range branches {
		if branch != base {
			out = append(out, branch)
		}
	}
	return out
}
