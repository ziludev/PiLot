package app

import (
	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
	"pilot/internal/theme"
)

// The comparison base: what a chat's work is measured against. It decides which
// files count as changed, which commits count as unmerged, and where 合并 lands.
//
// It is shown, not offered. Every repository this window writes to merges back
// into the branch the chat was cut from, so a picker would have no use beyond
// making the drawer describe the branch wrongly: a base set to something the
// worktree was not cut from turns every commit into an unmerged one and every
// file into a change. A readout next to the verbs keeps a person honest about
// what the numbers mean without inviting that mistake. Moving a base is a git
// question, answered in git.

// baseChip is the footer's first line: what the numbers below are measured from.
func (a *App) baseChip(c *ui.Context, t theme.Tokens) {
	ui.Row(c).Height(20).Padding(0, 2).Gap(4).AlignItems(ui.Center).
		Tooltip(i18n.T("baseTooltip")).Children(func() {
		ui.Text(c, i18n.TF("baseChip", map[string]any{"branch": a.changes.base})).
			FontSize(theme.TextMicro).TextColor(t.MutedFg).MaxLines(1)
	})
}
