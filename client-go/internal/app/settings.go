package app

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// Words for the settings panel. Constants, not i18n keys: the dictionary has
// to stay identical to client/src/i18n.ts.
const (
	settingsTitle    = "Settings"
	settingsTheme    = "Appearance"
	settingsKeys     = "Keyboard"
	themeSystemLabel = "Match the system"
	themeLightLabel  = "Light"
	themeDarkLabel   = "Dark"
)

// settingsDialog is the ⌘, panel: the two things that were only reachable from
// the rail's header buttons, plus the key list.
func (a *App) settingsDialog(c *ui.Context, t theme.Tokens) {
	if !a.settingsOpen {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Text(c, settingsTitle).FontSize(13).FontWeight(600).TextColor(t.Foreground)
		ui.Text(c, settingsTheme).FontSize(10).TextColor(t.MutedFg)
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			for _, choice := range []struct{ value, label string }{
				{"system", themeSystemLabel},
				{"light", themeLightLabel},
				{"dark", themeDarkLabel},
			} {
				choice := choice
				active := a.prefs.Theme == choice.value || (choice.value == "system" && a.prefs.Theme == "")
				button := ui.ButtonBase(c).Padding(4, 10).Radius(6).Label(choice.label).FontSize(11)
				if active {
					button.Background(t.Primary).TextColor(t.PrimaryFg)
				} else {
					button.Background(t.Muted).TextColor(t.MutedFg)
				}
				button.OnClick(func() {
					a.apply(func() {
						a.prefs.Theme = choice.value
						a.savePrefsLater()
						a.invalidate()
					})
				})
			}
		})
		ui.Text(c, settingsKeys).FontSize(10).TextColor(t.MutedFg)
		ui.Scroll(c).Height(220).FillWidth().Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				for _, row := range shortcuts() {
					ui.Row(c).FillWidth().Gap(10).Children(func() {
						ui.Text(c, row.Keys).FontSize(10).Width(110).SingleLine().TextColor(t.Foreground)
						ui.Text(c, row.Action).FontSize(10).Grow(1).TextColor(t.MutedFg)
					})
				}
			})
		})
		ui.Row(c).FillWidth().Children(func() {
			ui.Row(c).Grow(1)
			ui.Button(c, i18n.T("cancel")).FontSize(11).OnClick(func() {
				a.apply(func() { a.settingsOpen = false; a.invalidate() })
			})
		})
	})
	if !open && a.settingsOpen {
		a.apply(func() { a.settingsOpen = false; a.invalidate() })
	}
}
