package app

// 扩展: what this bridge has installed, and which of it this chat gets.
//
// An extension is code the model can call, so the list is not a settings page
// of preferences — each row is a switch that changes the tools the next turn
// sees. The bridge refuses to touch the few that everything else stands on, and
// says so; the window repeats its answer rather than guessing at one.

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
	"pilot/internal/icons"
	"pilot/internal/protocol"
	"pilot/internal/theme"
)

// extPanel is the list and the one write in flight. conv is the chat the rows
// were read for: a switch flips for a chat, so an answer for another chat is
// stale and dropped.
type extPanel struct {
	items    []protocol.Extension
	conv     int
	seq      int
	busy     bool
	failed   string
	toggling string
	list     ui.ListState
}

func (a *App) extensionsLabel() string {
	return i18n.T("extensions")
}

// loadExtensions reads them. Nothing polls: the set changes when the bridge is
// restarted or the list is asked again.
func (a *App) loadExtensions() {
	conv := a.active
	if conv == 0 {
		a.ext = extPanel{}
		a.invalidate()
		return
	}
	seq := a.ext.seq + 1
	a.ext.seq = seq
	a.ext.conv = conv
	a.ext.busy = true
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		rows, err := a.api.Extensions(ctx, conv)
		a.post(event{kind: evExtensions, seq: seq, convID: conv, extensions: rows, err: err})
	}()
}

func (a *App) applyExtensions(seq, conv int, rows []protocol.Extension, err error) {
	if seq != a.ext.seq || conv != a.ext.conv {
		return
	}
	a.ext.busy = false
	if err != nil {
		a.ext.failed = err.Error()
		a.invalidate()
		return
	}
	a.ext.failed = ""
	a.ext.items = rows
	a.invalidate()
}

// toggleExtension flips one for the open chat. The switch says what it did as
// soon as the bridge answers; a refusal keeps the row as it was.
func (a *App) toggleExtension(row protocol.Extension) {
	if a.ext.conv == 0 || a.ext.toggling != "" || row.Fixed != "" {
		return
	}
	conv := a.ext.conv
	a.ext.toggling = row.Name
	a.invalidate()
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		rows, err := a.api.SetExtension(ctx, conv, row.Name, !row.Enabled)
		a.post(event{kind: evExtToggle, convID: conv, text: row.Name, extensions: rows, err: err})
	}()
}

func (a *App) applyExtToggle(conv int, name string, rows []protocol.Extension, err error) {
	if conv != a.ext.conv {
		return
	}
	a.ext.toggling = ""
	if err != nil {
		a.ext.failed = err.Error()
		a.invalidate()
		return
	}
	a.ext.failed = ""
	if len(rows) > 0 {
		a.ext.items = rows
	}
	a.invalidate()
}

// extLine is one row of the list: either a group's heading or an extension.
type extLine struct {
	group string
	row   protocol.Extension
}

// extLines sorts the list into the two groups that matter: what a page may turn
// off, and what it may not. Installed is the bridge's order, so the split is
// stable and the count on each heading is the truth about that group.
func extLines(rows []protocol.Extension) []extLine {
	var optional, system []protocol.Extension
	for _, row := range rows {
		if row.Fixed != "" {
			system = append(system, row)
			continue
		}
		optional = append(optional, row)
	}
	lines := make([]extLine, 0, len(rows)+2)
	add := func(label string, group []protocol.Extension) {
		if len(group) == 0 {
			return
		}
		lines = append(lines, extLine{group: label})
		for _, row := range group {
			lines = append(lines, extLine{row: row})
		}
	}
	add(i18n.T("extGroupOptional"), optional)
	add(i18n.T("extGroupSystem"), system)
	return lines
}

func (a *App) extensionsBody(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Grow(1).Padding(0, 8, 10, 8).Gap(6).Children(func() {
		lines := extLines(a.ext.items)
		on := 0
		for _, row := range a.ext.items {
			if row.Enabled {
				on++
			}
		}
		ui.Row(c).Height(22).Padding(0, 4).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, itoa(len(a.ext.items))).FontSize(theme.TextMicro).FontWeight(600).
				TextColor(t.Ink3)
			ui.Text(c, i18n.T("extEnabledCount")).FontSize(theme.TextMicro).TextColor(t.Ink3)
			ui.Spacer(c)
			// on/total: the number that matters is how much of it this chat gets.
			ui.Text(c, itoa(on)+"/"+itoa(len(a.ext.items))).FontSize(theme.TextMicro).
				TextColor(t.Ink3)
			reload := ui.ButtonBase(c).Size(20, 20).Radius(theme.RadiusChip).Justify(ui.Center).
				AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("refresh"))
			reload.Children(func() { ui.Icon(c, icons.RefreshCw).Size(11, 11).TextColor(t.Ink3) })
			reload.OnClick(func() { a.loadExtensions() })
		})
		switch {
		case a.ext.failed != "":
			ui.Text(c, a.ext.failed).FontSize(theme.TextMicro).TextColor(t.Red).
				Padding(2, 4).MaxLines(3)
			return
		case a.ext.conv == 0:
			ui.Text(c, i18n.T("extNoChat")).FontSize(theme.TextMicro).TextColor(t.Ink3).
				Padding(2, 4).MaxLines(3)
			return
		case a.ext.busy && len(a.ext.items) == 0:
			ui.Text(c, i18n.T("loading")).FontSize(theme.TextMicro).TextColor(t.Ink3).Padding(2, 4)
			return
		case len(a.ext.items) == 0:
			ui.Text(c, i18n.T("extNone")).FontSize(theme.TextMicro).TextColor(t.Ink3).
				Padding(2, 4).MaxLines(3)
			return
		}
		ui.List(c, &a.ext.list, len(lines), func(index int) {
			line := lines[index]
			if line.group != "" {
				a.extensionGroup(c, t, line.group)
				return
			}
			a.extensionRow(c, t, line.row)
		}).Grow(1).Width(a.railW - 16)
	})
}

// extensionGroup is a heading over its rows: the same shape the rail's own
// sections have, so the list reads as two lists rather than one long one.
func (a *App) extensionGroup(c *ui.Context, t theme.Tokens, label string) {
	count := 0
	for _, row := range a.ext.items {
		if (label == i18n.T("extGroupSystem")) == (row.Fixed != "") {
			count++
		}
	}
	ui.Row(c).FillWidth().Height(28).Padding(4, 6, 2, 6).Gap(6).AlignItems(ui.Center).
		Children(func() {
		ui.Text(c, label).FontSize(theme.TextMicro).FontWeight(600).TextColor(t.Ink3)
		ui.Text(c, itoa(count)).FontSize(theme.TextMicro).TextColor(t.Ink3).Opacity(0.8)
	})
}

// extensionRow is one extension: its name, what it brings, and the switch.
func (a *App) extensionRow(c *ui.Context, t theme.Tokens, row protocol.Extension) {
	line := ui.Row(c).FillWidth().Height(40).Padding(4, 6).Gap(8).AlignItems(ui.Center).
		Radius(theme.RadiusControl)
	if line.Hovered() && row.Fixed == "" {
		line = line.Background(t.Hover)
	}
	line.Children(func() {
		nameColor := t.Ink
		if !row.Enabled {
			nameColor = t.Ink3
		}
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, row.Name).FontSize(theme.TextRow).FontWeight(500).
				TextColor(nameColor).MaxLines(1)
			detail := itoa(len(row.Tools)) + i18n.T("extTools")
			if len(row.Tools) == 0 {
				detail = i18n.T("extNoTools")
			}
			ui.Text(c, detail).FontSize(theme.TextMicro).TextColor(t.Ink3).MaxLines(1)
		})
		if row.Fixed != "" {
			return
		}
		a.extensionSwitch(c, t, row.Enabled, a.ext.toggling == row.Name,
			func() { a.toggleExtension(row) })
	})
}

// extensionSwitch is the one control on the row: a track with a knob, dark when
// on. A switch mid-flight says so by dimming rather than by moving early.
func (a *App) extensionSwitch(c *ui.Context, t theme.Tokens, on, busy bool, onClick func()) {
	track := ui.ButtonBase(c).Width(30).Height(18).Radius(9).Padding(2).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("extToggleTip"))
	justify := ui.Start
	if on {
		track = track.Background(t.Brand)
		justify = ui.End
	} else {
		track = track.Background(t.LineStrong)
	}
	if busy {
		track = track.Opacity(0.5)
	}
	track.Justify(justify).Children(func() {
		ui.Box(c).Size(14, 14).Radius(7).Background(t.Surface)
	})
	if !busy {
		track.OnClick(onClick)
	}
}
