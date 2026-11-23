package app

// The settings panel: the switches the rail's footer used to carry, behind one
// gear. There were only two, but the right panel is about to add more (which
// side widens, whether the diff folds), and a footer of unrelated icons is the
// wrong place to grow those.

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/theme"
)

// settingsPanel is only a latch: every switch it shows reads and writes prefs
// directly, so the panel never holds a copy that could drift.
type settingsPanel struct {
	open bool
}

// toggleSettings is the settings sheet, from the rail button or from ⌘K.
func (a *App) toggleSettings() {
	a.settings.open = !a.settings.open
	a.invalidate()
}

func (a *App) settingsDialog(c *ui.Context, t theme.Tokens) {
	if !a.settings.open {
		return
	}
	ui.Modal(c, &a.settings.open, func() {
		ui.Column(c).Width(340).Padding(14).Gap(12).Radius(theme.RadiusWindow).
			Background(t.Surface).Border(1, t.Line).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.22)).Children(func() {
			ui.Text(c, i18n.T("settings")).FontSize(theme.TextTitle).FontWeight(600).TextColor(t.Ink)
			a.settingsChoice(c, t, i18n.T("language"),
				[]string{"中文", "English"},
				func() int {
					if i18n.Current() == i18n.EN {
						return 1
					}
					return 0
				},
				func(picked int) {
					if picked == 1 {
						a.setLang(i18n.EN)
					} else {
						a.setLang(i18n.ZH)
					}
				})
			a.settingsChoice(c, t, i18n.T("theme"),
				[]string{i18n.T("themeSystem"), i18n.T("themeLightName"), i18n.T("themeDarkName")},
				func() int {
					switch a.prefs.Theme {
					case "light":
						return 1
					case "dark":
						return 2
					}
					return 0
				},
				func(picked int) {
					a.setTheme([3]string{"", "light", "dark"}[picked])
				})
			a.settingsChoice(c, t, i18n.T("remind"),
				[]string{i18n.T("remindOn"), i18n.T("remindOff")},
				func() int {
					if a.prefs.Silent {
						return 1
					}
					return 0
				},
				func(picked int) { a.setSilent(picked == 1) })
			a.settingsChoice(c, t, i18n.T("quietHours"),
				[]string{i18n.T("quietOn"), i18n.T("quietOff")},
				func() int {
					if a.prefs.Quiet == "off" {
						return 1
					}
					return 0
				},
				func(picked int) { a.setQuiet(picked == 0) })
			a.settingsChoice(c, t, i18n.T("dailyCap"),
				[]string{i18n.T("capFive"), i18n.T("capOff")},
				func() int {
					if a.prefs.DailyCap == "off" {
						return 1
					}
					return 0
				},
				func(picked int) { a.setCap(picked == 0) })
			a.settingsChoice(c, t, i18n.T("railGroup"),
				[]string{i18n.T("railGroupKind"), i18n.T("railGroupProject")},
				func() int {
					if a.prefs.groupByProject() {
						return 1
					}
					return 0
				},
				func(picked int) { a.setRailGroup([2]string{"", groupProject}[picked]) })
		})
	})
}

// settingsChoice is one labelled set of peers: a quiet group with the chosen one
// raised, the same shape as the rail's 会话 | 任务 switch.
func (a *App) settingsChoice(c *ui.Context, t theme.Tokens, label string, options []string,
	current func() int, pick func(int)) {
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, label).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
		ui.Row(c).FillWidth().Height(28).Radius(theme.RadiusControl).Padding(2).Gap(2).
			Background(t.Field).Children(func() {
			on := current()
			for index, text := range options {
				at, atText := index, text
				active := at == on
				btn := ui.ButtonBase(c).Grow(1).Height(24).Radius(theme.RadiusChip).
					Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer)
				if active {
					// Raised on the track, with a hairline: on a white panel the
					// plain page colour is the louder of the two.
					btn = btn.Background(t.Surface).Border(1, t.LineSoft)
				}
				btn.Children(func() {
					color, weight := t.Ink3, 500
					if active {
						color, weight = t.Ink, 600
					}
					ui.Text(c, atText).FontSize(theme.TextSmall).FontWeight(weight).TextColor(color).MaxLines(1)
				})
				btn.OnClick(func() { pick(at) })
			}
		})
	})
}

// settingsButton is the rail footer's way in: one icon where the two switches
// used to be.
func (a *App) settingsButton(c *ui.Context, t theme.Tokens) {
	btn := ui.ButtonBase(c).Size(24, 24).Radius(theme.RadiusControl).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(i18n.T("settings"))
	btn.Children(func() {
		ui.Icon(c, icons.Sliders).Size(12, 12).TextColor(t.Ink3)
	})
	btn.OnClick(func() {
		a.settings.open = true
		a.invalidate()
	})
}
