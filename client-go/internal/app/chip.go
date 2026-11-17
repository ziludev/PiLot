package app

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/theme"
)

// chip is one choice laid flat, as the settings panel's theme and language
// buttons and the rail's filter are: a short label, filled when it is the choice
// in force and washed on hover otherwise.
//
// ButtonBase draws no text of its own — its label is what assistive technology
// reads — so the visible text is a child, and the two say the same thing.
func chip(c *ui.Context, t theme.Tokens, label string, active bool, onClick func()) ui.Element {
	button := ui.ButtonBase(c).Height(21).PaddingX(5).Radius(5).
		AlignItems(ui.Center).Cursor(ui.CursorPointer)
	button.Label(label)
	ink := t.MutedFg
	switch {
	case active:
		button.Background(t.Primary)
		ink = t.PrimaryFg
	case button.Hovered():
		button.Background(t.Accent)
	}
	button.Children(func() {
		ui.Text(c, label).FontSize(10.5).TextColor(ink).MaxLines(1)
	})
	button.OnClick(onClick)
	return button
}
