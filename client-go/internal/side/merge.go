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

// MergeButton opens the merge dialog and shows the outcome of the last merge
// under it. A result is only shown once the server answered; a nil result is
// the idle button.
func MergeButton(c *ui.Context, result *protocol.MergeResult, onOpen func()) {
	t := theme.Current(c)
	button := func() {
		ui.Button(c, i18n.T("merge")).OnClick(func() {
			if onOpen != nil {
				onOpen()
			}
		})
	}
	if result == nil {
		button()
		return
	}
	ui.Column(c).Gap(2).Children(func() {
		button()
		text, tint := mergeOutcome(t, result)
		if text != "" {
			ui.Text(c, text).FontSize(10).TextColor(tint).Wrap().MaxWidth(260)
		}
	})
}

// MergeProps is what the merge dialog needs: the branches it joins, whether the
// checkout still has uncommitted work, and the outcome of the last attempt.
type MergeProps struct {
	Open, Busy, Dirty bool
	Branch, Base      string
	Result            *protocol.MergeResult
	OnCancel          func()
	OnConfirm         func()
	OnAskAgent        func()
}

// MergeDialog is MergeButton's confirmation, MergeButton.tsx:57-120: it asks
// before touching the worktree, refuses while there is uncommitted work, and
// when the merge conflicts it offers to hand the files to the agent instead of
// leaving the user with a sentence they cannot act on.
func MergeDialog(c *ui.Context, p MergeProps) {
	t := theme.Current(c)
	ui.Modal(c, &p.Open, func() {
		ui.Column(c).Width(400).Padding(14, 16).Gap(10).Radius(12).
			Background(t.Popover).Border(1, t.Border).Children(func() {
			ui.Text(c, i18n.T("merge")).FontSize(13).FontWeight(600).TextColor(t.PopoverFg)
			hint := p.Branch + " → " + p.Base
			ui.Text(c, hint).FontSize(11).TextColor(t.MutedFg).MaxLines(1)

			conflicts := conflictsOf(p.Result)
			switch {
			case len(conflicts) > 0:
				ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, icons.AlertTriangle).Size(12, 12).TextColor(t.Destructive)
					ui.Text(c, i18n.T("conflictIn")).FontSize(11).TextColor(t.Destructive)
				})
				for _, file := range conflicts {
					ui.Text(c, file).FontSize(10).Font("monospace").
						TextColor(t.MutedFg).MaxLines(1)
				}
			case p.Dirty:
				ui.Text(c, i18n.T("commitFirst")).FontSize(11).TextColor(t.Warning).Wrap()
			default:
				ui.Text(c, i18n.T("mergeHint")).FontSize(11).TextColor(t.MutedFg).Wrap()
			}

			ui.Row(c).FillWidth().Gap(6).Justify(ui.End).Children(func() {
				ui.Button(c, i18n.T("cancel")).OnClick(func() {
					if p.OnCancel != nil {
						p.OnCancel()
					}
				})
				if len(conflicts) > 0 {
					ui.PrimaryButton(c, i18n.T("askAgent")).OnClick(func() {
						if p.OnAskAgent != nil {
							p.OnAskAgent()
						}
					})
					return
				}
				button := ui.PrimaryButton(c, i18n.T("merge"))
				if p.Dirty || p.Busy {
					return
				}
				button.OnClick(func() {
					if p.OnConfirm != nil {
						p.OnConfirm()
					}
				})
			})
		})
	})
}

// conflictsOf names the files a conflicting merge stopped on.
func conflictsOf(result *protocol.MergeResult) []string {
	if result == nil || result.Status != "conflict" {
		return nil
	}
	return result.Conflicts
}

// mergeOutcome words a merge result as the side panel does: what happened, or
// what stands in the way.
func mergeOutcome(t theme.Tokens, result *protocol.MergeResult) (string, ui.Color) {
	switch result.Status {
	case "merged":
		// i18n has no key for the past tense; the confirmation stays English.
		text := "Merged"
		if result.Branch != "" {
			text = result.Branch
		}
		if result.Base != "" {
			text += " → " + result.Base
		}
		return fmt.Sprintf("%s · %d", text, result.Commits), t.MutedFg
	case "up-to-date":
		return i18n.T("nothingToMerge"), t.MutedFg
	case "dirty":
		return i18n.T("commitFirst"), t.Warning
	case "main-dirty":
		text := i18n.T("mainDirty")
		if len(result.Files) > 0 {
			files := result.Files
			if len(files) > 3 {
				files = append(files[:3:3], "…")
			}
			text += ": " + strings.Join(files, ", ")
		}
		return text, t.Warning
	case "conflict":
		text := i18n.T("conflictIn")
		if len(result.Conflicts) > 0 {
			text += ": " + strings.Join(result.Conflicts, ", ")
		}
		return text + " · " + i18n.T("askAgent"), t.Destructive
	}
	if result.Message != "" {
		return result.Message, t.MutedFg
	}
	return result.Status, t.MutedFg
}
