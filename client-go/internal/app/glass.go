package app

import (
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"pitta/internal/motion"
	"pitta/internal/theme"
)

// panelGlass is the material of the layers that float over the client: the same
// Liquid Glass the earlier client draws with `.glass` (backdrop blur 22, saturate 180), which
// mygo paints with its glass plugin — frosting what is under the panel, bending
// it near the rim and lighting the hairline, on the GPU or the CPU.
func panelGlass() ui.Material { return glass.Glass{} }

// pillGlass is the clearer glass of the small pills that sit close to content,
// the error count and the jump-to-latest bar: at 0.6 the backdrop reads
// through, as the earlier client's 95%-opaque `bg-background/95` does, and the text keeps
// its contrast against the pane rather than the page.
func pillGlass() ui.Material { return glass.Glass{Clarity: 0.6} }

// toasts draws the client's toasts on glass, as sonner's sit on `.glass` in
// the earlier client: one line over whatever is under it, instead of the theme's opaque
// inverse pill mygo draws when the view builds no viewport.
func (a *App) toasts(c *ui.Context) {
	t := theme.Current(c)
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Bottom(24).AlignItems(ui.Center).Gap(8)
		for _, toast := range toasts {
			parts := ui.ToastBase(c, toast)
			// ToastBase gives the pill a key of its own (ts.key), so the
			// transition below has an identity to play against and the exit
			// ghost matches the node it replaced. Adding a Key here would
			// replace that key and break both.
			pill := parts.Root.Row().AlignItems(ui.Center).Gap(8).Padding(10, 14).
				Radius(12).MaxWidth(420).Material(panelGlass()).
				Transition(motion.ToastEnter())
			// ToastBase draws a status box, not its title: the theme's own
			// toasts fill the pill, so this one has to as well.
			pill.Children(func() {
				ui.Text(c, toast.Title).FontSize(13).TextColor(t.Foreground).MaxLines(2)
			})
		}
	})
}
