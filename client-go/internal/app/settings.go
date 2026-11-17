package app

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// Words for the settings panel. Constants, not i18n keys: the dictionary has
// to stay identical to internal/i18n/origin/i18n.ts.
const (
	settingsTitle    = "Settings"
	settingsTheme    = "Appearance"
	settingsWindow   = "Window"
	settingsOpacity  = "Transparency"
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
				// ButtonBase draws no text: the label is what assistive
				// technology reads, and the children are what the eye sees.
				button := ui.ButtonBase(c).Padding(4, 10).Radius(6).Label(choice.label)
				ink := t.MutedFg
				if active {
					button.Background(t.Primary)
					ink = t.PrimaryFg
				} else {
					button.Background(t.Muted)
				}
				button.Children(func() {
					ui.Text(c, choice.label).FontSize(11).TextColor(ink)
				})
				button.OnClick(func() {
					a.apply(func() {
						a.prefs.Theme = choice.value
						a.savePrefsLater()
						a.invalidate()
					})
				})
			}
		})
		ui.Text(c, settingsWindow).FontSize(10).TextColor(t.MutedFg)
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, settingsOpacity).FontSize(11).TextColor(t.Foreground)
			// The slider writes into prefs through setOpacity, which also
			// repaints every open window, so the change shows while dragging.
			opacity := float64(a.prefs.opacity())
			ui.Slider(c, &opacity, float64(minOpacity), float64(maxOpacity)).Grow(1).
				OnChange(func() {
					a.apply(func() {
						a.setOpacity(float32(opacity))
						a.savePrefsLater()
						a.invalidate()
					})
				})
			ui.Text(c, itoa(int(opacity*100))+"%").FontSize(10).Width(34).
				TextColor(t.MutedFg).MaxLines(1)
		})
		// The agent's own controls: they act on the daemon, not on a chat, so
		// they belong here rather than under the pointer in the rail's footer.
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			for _, action := range []struct{ key, control string }{
				{"restart", "restart"},
				{"backup", "backup"},
			} {
				action := action
				ui.Button(c, i18n.T(action.key)).FontSize(11).OnClick(func() {
					go a.control(action.control)
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
