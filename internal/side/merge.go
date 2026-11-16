package side

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// MergeButton draws the merge action and the outcome of the last merge. A
// result is only shown once the server answered; a nil result is the idle
// button.
func MergeButton(c *ui.Context, result *protocol.MergeResult, onMerge func()) {
	t := theme.Current(c)
	button := func() {
		ui.Button(c, i18n.T("merge")).OnClick(func() {
			if onMerge != nil {
				onMerge()
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
