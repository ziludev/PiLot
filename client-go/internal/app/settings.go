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

func (a *App) settingsDialog(c *ui.Context, t theme.Tokens) {
	if !a.settings.open {
		return
	}
	ui.Modal(c, &a.settings.open, func() {
		ui.Column(c).Width(340).Padding(16).Gap(14).Radius(10).
			Background(t.Popover).Border(1, t.Border).Children(func() {
			ui.Text(c, i18n.T("settings")).FontSize(12.5).FontWeight(600).TextColor(t.Foreground)
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
		})
	})
}

// settingsChoice is one labelled set of peers: a quiet group with the chosen one
// raised, the same shape as the rail's 会话 | 任务 switch.
func (a *App) settingsChoice(c *ui.Context, t theme.Tokens, label string, options []string,
	current func() int, pick func(int)) {
	ui.Column(c).FillWidth().Gap(6).Children(func() {
		ui.Text(c, label).FontSize(10.5).TextColor(t.MutedFg).MaxLines(1)
		ui.Row(c).FillWidth().Height(28).Radius(7).Padding(2).Gap(2).
			Background(t.SidebarAccent).Children(func() {
			on := current()
			for index, text := range options {
				at, atText := index, text
				active := at == on
				btn := ui.ButtonBase(c).Grow(1).Height(24).Radius(5).
					Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer)
				if active {
					btn = btn.Background(t.Background)
				}
				btn.Children(func() {
					color, weight := t.MutedFg, 500
					if active {
						color, weight = t.Foreground, 600
					}
					ui.Text(c, atText).FontSize(11).FontWeight(weight).TextColor(color).MaxLines(1)
				})
				btn.OnClick(func() { pick(at) })
			}
		})
	})
}

// settingsButton is the rail footer's way in: one icon where the two switches
// used to be.
func (a *App) settingsButton(c *ui.Context, t theme.Tokens) {
	btn := ui.ButtonBase(c).Size(22, 22).Radius(5).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(i18n.T("settings"))
	btn.Children(func() {
		ui.Icon(c, icons.Sliders).Size(12, 12).TextColor(t.MutedFg)
	})
	btn.OnClick(func() {
		a.settings.open = true
		a.invalidate()
	})
}
