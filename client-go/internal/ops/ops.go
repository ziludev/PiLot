// Package ops draws the four read-only panels the React client's side panel and
// task dialog show: memory, access, the usage calendar and one task's detail.
//
// The panels own no data. Every value arrives in Props and every action leaves
// through a callback, so the app decides what a click means and the panels stay
// free of the bridge.
package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/theme"
)

// The dictionary carries the React client's copy, and these labels have no key
// there because the React panels hardcode them inline: the English words stand
// in for both languages.
const (
	levelLabelAll    = "All"
	levelLabelOwner  = "Owner"
	levelLabelGroup  = "Group"
	levelLabelClosed = "Closed"

	dropLabel     = "Drop"
	closeLabel    = "Close"
	cancelLabel   = "Canceling…"
	tokensLabel   = "Tokens"
	peakLabel     = "Peak"
	membersLabel  = "members"
	envLockedNote = "locked in .env: this chat cannot be opened here"
	untitledTask  = "(no description)"
	readingLabel  = "Reading…"
)

// card is the panels' surface: shadcn's rounded card at the client's radius.
func card(c *ui.Context, t theme.Tokens) ui.Element {
	return ui.Column(c).FillWidth().Padding(10).Gap(6).Radius(theme.Radius).
		Background(t.Card).Border(1, t.Border)
}

// heading is a panel's title row: an icon, a name, a muted hint and the panel's
// own action at the right, which fn draws.
func heading(c *ui.Context, t theme.Tokens, icon *ui.SVG, title, hint string, action func()) {
	ui.Row(c).FillWidth().Padding(8, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, icon).Size(12, 12).TextColor(t.MutedFg)
		ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(t.Foreground).MaxLines(1)
		switch {
		case hint != "":
			ui.Text(c, hint).FontSize(10).TextColor(t.MutedFg).MaxLines(1).Grow(1)
		case action != nil:
			ui.Spacer(c)
		}
		if action != nil {
			action()
		}
	})
}

// button is a shadcn pill; filled for the panel's one main action, outlined for
// the rest. Its label is also its accessible name, so the tester can click it.
func button(c *ui.Context, t theme.Tokens, icon *ui.SVG, label string, filled, disabled bool, onClick func()) ui.Element {
	b := ui.ButtonBase(c).Height(24).PaddingX(8).Gap(4).Radius(6).
		Cursor(ui.CursorPointer).Label(label).Disabled(disabled)
	fg := t.Foreground
	if filled {
		b.Background(t.Primary)
		fg = t.PrimaryFg
	} else {
		b.Background(t.Card).Border(1, t.Border)
	}
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(10, 10).TextColor(fg)
		}
		ui.Text(c, label).FontSize(10).TextColor(fg).MaxLines(1)
	})
	if onClick != nil {
		b.OnClick(onClick)
	}
	return b
}

// glyph is a borderless icon button. name is both its tooltip and its
// accessible name, which is what the tester clicks.
func glyph(c *ui.Context, t theme.Tokens, icon *ui.SVG, name string, onClick func()) {
	b := ui.ButtonBase(c).Size(22, 22).Radius(5).Cursor(ui.CursorPointer).Label(name)
	b.Tooltip(name)
	if b.Hovered() {
		b.Background(t.Accent)
	}
	b.Children(func() { ui.Icon(c, icon).Size(11, 11).TextColor(t.MutedFg) })
	if onClick != nil {
		b.OnClick(onClick)
	}
}

// badge is a short chip in one of the palette's colors.
func badge(c *ui.Context, t theme.Tokens, text string, bg, fg ui.Color) {
	ui.Row(c).Shrink(0).Padding(2, 6).Radius(4).Background(bg).
		Border(1, t.Border).AlignItems(ui.Center).Children(func() {
		ui.Text(c, text).FontSize(9).FontWeight(600).TextColor(fg).MaxLines(1)
	})
}

// caption is a section's muted name, in the small caps the client uses.
func caption(c *ui.Context, t theme.Tokens, s string) {
	ui.Text(c, strings.ToUpper(s)).FontSize(9).FontWeight(600).LetterSpacing(0.4).
		TextColor(t.MutedFg).MaxLines(1)
}

// empty says why a list is blank, centered in the space it was given.
func empty(c *ui.Context, t theme.Tokens, lines ...string) {
	ui.Column(c).FillWidth().Padding(16, 8).Gap(3).AlignItems(ui.Center).Children(func() {
		for i, line := range lines {
			size := float32(11)
			if i > 0 {
				size = 10
			}
			ui.Text(c, line).FontSize(size).TextColor(t.MutedFg).Wrap().TextAlign(ui.Center)
		}
	})
}

// money is a spend with cents, as the client prints totals.
func money(cost float64) string {
	return fmt.Sprintf("$%.2f", cost)
}

// moneyFine is a single day's spend; a day can cost less than a cent.
func moneyFine(cost float64) string {
	return fmt.Sprintf("$%.4f", cost)
}

// tokenCount is a token total in the k/M shorthand the calendar uses.
func tokenCount(tokens int) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.2fM", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}

// clock is a timestamp in the client's short form: the day, then the minute.
func clock(at int64) string {
	return time.UnixMilli(at).Format("01-02 15:04")
}

// noteClock shortens a note's ISO timestamp to the same day and minute form.
func noteClock(at string) string {
	if len(at) >= 16 {
		return at[5:10] + " " + at[11:16]
	}
	return at
}

// tail is the end of a chat id: enough to tell two chats apart where the full
// id would push everything else off the row.
func tail(id string) string {
	if len(id) <= 8 {
		return id
	}
	return "…" + id[len(id)-8:]
}
