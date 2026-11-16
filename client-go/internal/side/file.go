package side

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// FileProps is the read-only viewer of one workspace file.
type FileProps struct {
	Path      string
	File      *protocol.FilePayload
	Err       string
	OnClose   func()
	OnComment func(line string) // stage a review note on a line
	// OnPreview opens the file in the system's preview, when the platform has
	// one; nil hides the button.
	OnPreview func()
}

// FileView draws one numbered line per row and scrolls sideways on long
// lines, as the earlier client FileViewer scrolls its pre block.
func FileView(c *ui.Context, p FileProps) {
	t := theme.Current(c)
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		fileHeader(c, t, p)
		switch {
		case p.Err != "":
			ui.Row(c).FillWidth().Padding(14, 12).Gap(8).Children(func() {
				ui.Icon(c, icons.FileIcon).Size(14, 14).TextColor(t.Destructive)
				ui.Text(c, p.Err).FontSize(11).TextColor(t.MutedFg).Wrap().Grow(1)
			})
		case p.File == nil:
			ui.Text(c, "…").FontSize(11).TextColor(t.MutedFg).Padding(12, 12)
		default:
			fileBody(c, t, p)
		}
	})
}

// fileHeader is the path, the size and the close button above the lines.
func fileHeader(c *ui.Context, t theme.Tokens, p FileProps) {
	ui.Row(c).FillWidth().Padding(8, 10).Gap(8).AlignItems(ui.Center).
		BorderWidth(0, 0, 1, 0).BorderColor(t.Border).Children(func() {
		ui.Text(c, p.Path).Font("monospace").FontSize(11).TextColor(t.Foreground).
			SingleLine().TruncateMiddle().Grow(1)
		if p.File != nil {
			// No i18n key names the unit of a file's line count; the client
			// shows English there too.
			meta := fmt.Sprintf("%d lines · %s", p.File.Lines, sizeLabel(p.File.Size))
			if p.File.Truncated {
				meta += " · " + i18n.T("truncated")
			}
			ui.Text(c, meta).FontSize(10).TextColor(t.MutedFg)
		}
		if p.OnPreview != nil {
			iconButton(c, icons.ExternalLink, i18n.T("preview"), t.MutedFg, p.OnPreview)
		}
		iconButton(c, icons.X, i18n.T("back"), t.MutedFg, p.OnClose)
	})
}

// fileBody is the virtual list of lines: gutter numbers stay at the left and
// the line, which can be wider than the panel, scrolls sideways.
func fileBody(c *ui.Context, t theme.Tokens, p FileProps) {
	lines := strings.Split(p.File.Content, "\n")
	st := stateFor(c)
	// Coloring the file is what an editor does with it; a type the highlighter
	// does not know comes back nil and the lines stay plain.
	spans := codeSpans(c, &st.code, p.Path, p.File.Content, lines)
	ui.List(c, &st.file, len(lines), func(i int) {
		line := lines[i]
		shown := line
		if shown == "" {
			shown = " "
		}
		row := ui.Row(c).Height(17).PaddingX(8).Gap(10).Cursor(ui.CursorPointer)
		if row.Hovered() {
			row.Background(c.Theme().SurfaceHover)
		}
		row.Children(func() {
			ui.Text(c, strconv.Itoa(i+1)).Font("monospace").FontSize(10).
				TextColor(t.MutedFg).Width(44).TextAlign(ui.End)
			line := shown
			if spans != nil && i < len(spans) && len(spans[i]) > 0 {
				ui.RichText(c, spans[i]...).Font("monospace").FontSize(11).NoWrap()
				return
			}
			ui.Text(c, line).Font("monospace").FontSize(11).TextColor(t.Foreground).NoWrap()
		})
		if row.RowClicked() && p.OnComment != nil {
			p.OnComment(line)
		}
	}).Grow(1).FillWidth().RowWidth(lineWidth(c, lines))
}

// lineWidth is the room the widest line needs, plus the gutter, so that short
// files do not scroll sideways and long ones do.
func lineWidth(c *ui.Context, lines []string) float32 {
	longest := ""
	for _, line := range lines {
		if len(line) > len(longest) {
			longest = line
		}
	}
	width, _ := c.MeasureText(0, ui.Span{Text: longest, Font: "monospace", Size: 11})
	return width + 70
}
