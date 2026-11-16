package side

import (
	"fmt"
	"strconv"

	"github.com/egoist/mygo/ui"

	"pitta/internal/diff"
	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// iconButton is a small square button, as the header rows use: the icon only,
// with the label as its tooltip and accessible name.
func iconButton(c *ui.Context, icon *ui.SVG, label string, tint ui.Color, onClick func()) {
	if onClick == nil {
		return
	}
	button := ui.ButtonBase(c).Size(22, 22).Radius(4).Cursor(ui.CursorPointer)
	button.Label(label)
	button.Tooltip(label)
	if button.Hovered() {
		button.Background(c.Theme().SurfaceHover)
	}
	button.Children(func() {
		ui.Icon(c, icon).Size(12, 12).TextColor(tint)
	})
	button.OnClick(onClick)
}

// statusLetter is the one-letter git badge for a file status.
func statusLetter(status string) string {
	switch status {
	case "modified":
		return "M"
	case "added":
		return "A"
	case "deleted":
		return "D"
	case "renamed":
		return "R"
	case "conflicted":
		return "C"
	case "untracked":
		return "U"
	}
	if status == "" {
		return "?"
	}
	return status[:1]
}

// statusLabel is the localized word for a file status; a status this client
// does not know shows as it came from git.
func statusLabel(status string) string {
	switch status {
	case "modified", "added", "deleted", "renamed", "conflicted", "untracked", "clean":
		return i18n.T(status)
	}
	if status == "" {
		return "?"
	}
	return status
}

// statusTint colors the badge so a mix of changes reads at a glance.
func statusTint(c *ui.Context, t theme.Tokens, status string) ui.Color {
	colors := diffColors(c)
	switch status {
	case "added", "untracked":
		return colors.AddFg
	case "deleted":
		return colors.DelFg
	case "renamed":
		return t.Primary
	case "conflicted":
		return t.Destructive
	case "modified":
		return t.Warning
	}
	return t.MutedFg
}

func diffColors(c *ui.Context) diff.Colors {
	if c.Theme().Dark {
		return diff.Dark()
	}
	return diff.Light()
}

// renderPatch draws a unified patch with the diff package's cards.
func renderPatch(c *ui.Context, view *diff.View, patch string) {
	files := diff.Parse(patch)
	if len(files) == 0 {
		ui.Text(c, patch).Font("monospace").FontSize(10).TextColor(theme.Current(c).MutedFg)
		return
	}
	view.Render(c, files, diff.Options{Colors: diffColors(c)})
}

// changeStats draws the +N −N column, each count in its own color.
func changeStats(c *ui.Context, t theme.Tokens, added, removed int) {
	colors := diffColors(c)
	ui.Row(c).Gap(4).Children(func() {
		if added > 0 {
			ui.Text(c, "+"+strconv.Itoa(added)).FontSize(10).TextColor(colors.AddFg)
		}
		if removed > 0 {
			ui.Text(c, "−"+strconv.Itoa(removed)).FontSize(10).TextColor(colors.DelFg)
		}
	})
}

// sizeLabel is a file's size as the viewer's header shows it.
func sizeLabel(size int) string {
	return fmt.Sprintf("%.1f KB", float64(size)/1024)
}
