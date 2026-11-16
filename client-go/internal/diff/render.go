package diff

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

const (
	fontSize   float32 = 11
	lineHeight float32 = 18
	radius     float32 = 6
	padX       float32 = 8
)

// Colors are the palette Render draws with. The caller picks a set to match
// the window; this package never looks at the theme.
type Colors struct {
	Text     ui.Color
	Number   ui.Color
	AddFg    ui.Color
	AddBg    ui.Color
	DelFg    ui.Color
	DelBg    ui.Color
	HunkFg   ui.Color
	HunkBg   ui.Color
	MetaFg   ui.Color
	HeaderFg ui.Color
	Border   ui.Color
}

// Light is the palette for a light window.
func Light() Colors {
	return Colors{
		Text:     ui.Hex("#3f3f46"),
		Number:   ui.Hex("#a1a1aa"),
		AddFg:    ui.Hex("#047857"),
		AddBg:    ui.RGBA(16, 185, 129, 0.12),
		DelFg:    ui.Hex("#be123c"),
		DelBg:    ui.RGBA(244, 63, 94, 0.10),
		HunkFg:   ui.Hex("#71717a"),
		HunkBg:   ui.Hex("#f4f4f5"),
		MetaFg:   ui.Hex("#a1a1aa"),
		HeaderFg: ui.Hex("#18181b"),
		Border:   ui.Hex("#e4e4e7"),
	}
}

// Dark is the palette for a dark window.
func Dark() Colors {
	return Colors{
		Text:     ui.Hex("#d4d4d8"),
		Number:   ui.Hex("#71717a"),
		AddFg:    ui.Hex("#6ee7b7"),
		AddBg:    ui.RGBA(16, 185, 129, 0.16),
		DelFg:    ui.Hex("#fda4af"),
		DelBg:    ui.RGBA(244, 63, 94, 0.14),
		HunkFg:   ui.Hex("#a1a1aa"),
		HunkBg:   ui.Hex("#27272a"),
		MetaFg:   ui.Hex("#71717a"),
		HeaderFg: ui.Hex("#f4f4f5"),
		Border:   ui.RGBA(255, 255, 255, 0.10),
	}
}

// Options are how Render draws.
type Options struct {
	Colors Colors
}

// A View draws diffs and remembers which files the reader folded, which no
// File can hold: the same patch can appear twice with different folds. The
// zero View works; keep one per panel or transcript.
type View struct {
	folded map[fileKey]bool
}

type fileKey struct {
	index int
	path  string
}

// Render draws the files as a stack of cards, each with a header that folds
// its lines, a hunk header on gray, added and removed lines on green and red,
// and both line numbers on the left. Lines wider than the card scroll
// sideways within the card.
func (v *View) Render(c *ui.Context, files []File, o Options) {
	if v.folded == nil {
		v.folded = map[fileKey]bool{}
	}
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		for i, f := range files {
			v.file(c, fileKey{index: i, path: f.Path}, f, o)
		}
	})
}

func (v *View) file(c *ui.Context, key fileKey, f File, o Options) {
	rows := fileRows(f)
	if f.Path == "" {
		// A fragment of a patch: nothing to fold, no card to draw around it.
		v.body(c, rows, o)
		return
	}
	ui.Column(c).FillWidth().Clip().Border(1, o.Colors.Border).Radius(radius).Children(func() {
		v.header(c, key, f, o)
		if !v.folded[key] {
			v.body(c, rows, o)
		}
	})
}

func (v *View) header(c *ui.Context, key fileKey, f File, o Options) {
	folded := v.folded[key]
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(6).Padding(5, padX).
		OnClick(func() { v.folded[key] = !folded }).
		Children(func() {
			arrow := "▾"
			if folded {
				arrow = "▸"
			}
			ui.Text(c, arrow).FontSize(9).TextColor(o.Colors.Number)
			ui.Text(c, f.Path).Font("monospace").FontSize(fontSize).NoWrap().SingleLine().
				TruncateMiddle().Grow(1).TextColor(o.Colors.HeaderFg)
			if f.Added > 0 {
				ui.Text(c, "+"+strconv.Itoa(f.Added)).FontSize(10).TextColor(o.Colors.AddFg)
			}
			if f.Removed > 0 {
				ui.Text(c, "-"+strconv.Itoa(f.Removed)).FontSize(10).TextColor(o.Colors.DelFg)
			}
		})
}

func (v *View) body(c *ui.Context, rows []row, o Options) {
	if len(rows) == 0 {
		return
	}
	charW := charWidth(c)
	gutter := gutterWidth(rows, charW)
	cells := 0
	for _, r := range rows {
		if n := textCells(r.text); n > cells {
			cells = n
		}
	}
	width := float32(cells)*charW + 2*gutter + 2*padX

	// A List stretches its rows to its own width, which is the wider of the
	// panel and RowWidth. That keeps a row's background across the whole panel
	// when the patch is narrow, and across the scrolled area when it is wide.
	// The fixed height means the wheel never stops at the diff, and the rows
	// stay virtualized however long the file is.
	ui.List(c, nil, len(rows), func(i int) {
		drawRow(c, rows[i], o, gutter)
	}).FillWidth().RowWidth(width).Height(float32(len(rows))*lineHeight).
		ScrollbarAxes(true, false)
}

func drawRow(c *ui.Context, r row, o Options, gutter float32) {
	// A context line shows the card through: only its text is colored.
	var bg ui.Color
	fg := o.Colors.Text
	switch r.kind {
	case Add:
		bg, fg = o.Colors.AddBg, o.Colors.AddFg
	case Del:
		bg, fg = o.Colors.DelBg, o.Colors.DelFg
	case kindHunk:
		bg, fg = o.Colors.HunkBg, o.Colors.HunkFg
	case Meta:
		fg = o.Colors.MetaFg
	}
	e := ui.Row(c).FillWidth().Height(lineHeight).AlignItems(ui.Center).Padding(0, padX)
	if bg.A != 0 {
		e.Background(bg)
	}
	e.Children(func() {
		ui.Text(c, number(r.oldNo)).Width(gutter).TextAlign(ui.End).NoWrap().
			FontSize(10).TextColor(o.Colors.Number)
		ui.Text(c, number(r.newNo)).Width(gutter).TextAlign(ui.End).NoWrap().
			FontSize(10).TextColor(o.Colors.Number)
		ui.Text(c, r.text).Font("monospace").FontSize(fontSize).NoWrap().TextColor(fg)
	})
}

// row is one line as drawn: a hunk header and a file's meta lines are rows
// too, which Parse keeps out of Lines.
type row struct {
	kind         Kind
	text         string
	oldNo, newNo int
}

func fileRows(f File) []row {
	rows := make([]row, 0, len(f.Meta)+len(f.Hunks)*4)
	for _, m := range f.Meta {
		rows = append(rows, row{kind: Meta, text: m})
	}
	for _, h := range f.Hunks {
		if h.Header != "" {
			rows = append(rows, row{kind: kindHunk, text: h.Header})
		}
		for _, l := range h.Lines {
			rows = append(rows, row{kind: l.Kind, text: l.Text, oldNo: l.OldNo, newNo: l.NewNo})
		}
	}
	return rows
}

func number(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// charWidth measures one monospace cell, which is all the width estimate
// needs: shaping every row of a long file each frame is too costly.
func charWidth(c *ui.Context) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: "0000000000", Font: "monospace", Size: fontSize})
	if w <= 0 {
		return 6.6 // A cell of the Menlo or SF Mono fallback at 11.
	}
	return w / 10
}

func gutterWidth(rows []row, charW float32) float32 {
	digits := 1
	for _, r := range rows {
		if n := max(r.oldNo, r.newNo); n > 0 {
			if d := len(strconv.Itoa(n)); d > digits {
				digits = d
			}
		}
	}
	return float32(digits)*charW + padX + 4
}

// textCells counts the cells a monospace font draws the text in: a tab moves
// to the next stop, an East Asian or emoji rune takes two. Guessing high only
// scrolls further than needed; guessing low would clip the end of a line.
func textCells(s string) int {
	cells := 0
	for _, r := range s {
		switch {
		case r == '\t':
			cells += 8
		case isWide(r):
			cells += 2
		default:
			cells++
		}
	}
	return cells
}

func isWide(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f ||
		r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1f64f) ||
		(r >= 0x1f900 && r <= 0x1f9ff) ||
		(r >= 0x20000 && r <= 0x3fffd))
}
