package app

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/theme"
)

// errorPanel surfaces the failures that only ever reached the log file: a
// button in the corner while anything is wrong, and the list behind it. It
// draws nothing when the app is clean.
func (a *App) errorPanel(c *ui.Context, t theme.Tokens) {
	if len(a.errs) == 0 {
		return
	}
	ui.Column(c).FillWidth().Padding(0, 8).Children(func() {
		ui.Row(c).AlignSelf(ui.End).Children(func() {
			pill := ui.Row(c).Gap(5).Padding(2, 8).Radius(10).Material(pillGlass()).
				OnClick(func() {
					a.apply(func() { a.errsOpen = !a.errsOpen; a.invalidate() })
				})
			pill.Children(func() {
				ui.Icon(c, icons.AlertTriangle).Size(11, 11).TextColor(t.Destructive)
				ui.Text(c, itoa(len(a.errs))+" "+i18n.T("errors")).FontSize(10).TextColor(t.Destructive)
			})
		})
	})
	open := a.errsOpen
	ui.Modal(c, &open, func() {
		ui.Text(c, i18n.T("errors")).FontSize(13).FontWeight(600).TextColor(t.Foreground)
		ui.Text(c, i18n.T("errorsHint")).FontSize(10).TextColor(t.MutedFg)
		ui.Scroll(c).FillWidth().Height(260).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for _, failure := range a.errs {
					ui.Row(c).FillWidth().Gap(6).Padding(4, 6).Radius(6).Background(t.Muted).Children(func() {
						ui.Text(c, failure.At.Format("15:04:05")).FontSize(9).TextColor(t.MutedFg)
						ui.Text(c, failure.Message).FontSize(10).TextColor(t.Destructive).Grow(1)
					})
				}
			})
		})
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			ui.Row(c).Grow(1)
			ui.Button(c, i18n.T("clear")).FontSize(11).OnClick(func() {
				a.apply(func() { a.errs = nil; a.errsOpen = false; a.invalidate() })
			})
			ui.Button(c, i18n.T("cancel")).FontSize(11).OnClick(func() {
				a.apply(func() { a.errsOpen = false; a.invalidate() })
			})
		})
	})
	if a.errsOpen != open {
		a.apply(func() { a.errsOpen = open; a.invalidate() })
	}
}
