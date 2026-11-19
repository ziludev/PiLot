package app

// Finding words inside the open chat: Cmd-F opens a bar over the transcript,
// counts the rows whose words contain the query, and steps between them. The
// unit of a hit is a row, not an occurrence — the transcript is markdown, and a
// substring highlighter would have to reach inside styled runs; the bar says
// which of how many, and the matching rows get a strip so the eye knows what
// the count is about.

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/theme"
)

type findState struct {
	open    bool
	query   string
	current int

	// matches are row indices in the open chat that hold the query.
	matches []int
	// key is the chat and query the matches were counted for: a new key resets
	// the walk to the first hit, an old one keeps the current step even as rows
	// stream in behind.
	key string
	// shown is the row that was last scrolled to, so a still frame does not
	// scroll again over the reader's own scrolling.
	shown string
	// focus asks for the keyboard on the next frame, once: the bar is opened by
	// a menu item, and typing is what the reader wants next.
	focus bool
}

// ToggleFind is the menu's Cmd-F: the first press opens the bar, the second
// closes it and gives the transcript its follow-the-end behaviour back.
func (a *App) ToggleFind() {
	if a.active == 0 {
		return
	}
	a.find.open = !a.find.open
	if a.find.open {
		// Re-open where the last search left off, but always scroll: the reader
		// may have scrolled away in between.
		a.find.key = ""
		a.find.current = 0
		a.find.focus = true
	} else {
		a.findClear()
	}
	a.invalidate()
}

func (a *App) findClear() {
	a.find.matches, a.find.current, a.find.key, a.find.shown = nil, 0, "", ""
}

// findBrowsing is true while a query is holding the transcript at a hit instead
// of the newest turn.
func (a *App) findBrowsing() bool {
	return a.find.open && strings.TrimSpace(a.find.query) != ""
}

// StepFind walks the hits: the bar's arrows and the two menu items both land
// here. Wrapping at the ends is what every find bar does, so a reader stepping
// past the last hit arrives at the first.
func (a *App) StepFind(delta int) {
	if !a.find.open || len(a.find.matches) == 0 {
		return
	}
	next := (a.find.current + delta) % len(a.find.matches)
	if next < 0 {
		next += len(a.find.matches)
	}
	a.find.current = next
	a.invalidate()
}

// syncFind recounts the hits for the frame. It runs before the bar draws, not
// after the transcript does: the bar shows the count, and a count one frame
// behind the query reads as "nothing found" right when the reader typed it.
func (a *App) syncFind() {
	// A closed bar is not worth reading a thread for.
	if !a.find.open || a.active == 0 {
		a.findClear()
		return
	}
	a.syncFindRows(a.threadRows(a.active))
}

// syncFindRows recounts the hits for the frame's rows and scrolls to the current
// one when the walk moved. It runs every frame: a streaming turn appends rows,
// and a stale count is a lie about how many are left.
func (a *App) syncFindRows(rows []threadRow) {
	if !a.find.open {
		a.findClear()
		return
	}
	query := strings.ToLower(strings.TrimSpace(a.find.query))
	if query == "" {
		a.findClear()
		return
	}
	key := strconv.Itoa(a.active) + "|" + query
	if key != a.find.key {
		a.find.key, a.find.current, a.find.shown = key, 0, ""
	}
	a.find.matches = a.find.matches[:0]
	for index := range rows {
		if strings.Contains(rowWords(rows[index]), query) {
			a.find.matches = append(a.find.matches, index)
		}
	}
	if len(a.find.matches) == 0 {
		return
	}
	if a.find.current >= len(a.find.matches) {
		a.find.current = 0
	}
	row := a.find.matches[a.find.current]
	// The row index plus the query says where the walk stands: scroll once per
	// step, not once per frame.
	shown := strconv.Itoa(row) + "|" + key
	if shown != a.find.shown {
		a.find.shown = shown
		a.vm.threadList.ScrollTo(row, ui.Center)
	}
}

// rowWords is what a row is searched by: its own text, and the words of the
// steps folded behind it, because a hit inside a tool call is still a hit.
func rowWords(row threadRow) string {
	if len(row.steps) == 0 {
		return strings.ToLower(row.text)
	}
	var words strings.Builder
	words.WriteString(strings.ToLower(row.text))
	for _, step := range row.steps {
		words.WriteString("\n")
		words.WriteString(strings.ToLower(step.label + "\n" + step.body + "\n" + step.out))
	}
	return words.String()
}

// findMark is how a row shows up while a search is on: 1 for the hit the walk
// is standing on, 0 for the others, -1 for a row the query does not touch.
func (a *App) findMark(row int) int {
	if !a.findBrowsing() {
		return -1
	}
	for _, hit := range a.find.matches {
		if hit == row {
			if hit == a.find.matches[a.find.current] {
				return 1
			}
			return 0
		}
	}
	return -1
}

// findBar is the bar itself, drawn under the header so it outlives the
// transcript's loading and error states. It is hand-drawn: mygo's FindBar says
// "1 of 3" in the window's language, which this app sets from its own prefs,
// and a bar that reads English in a Chinese window is worse than four small
// buttons.
func (a *App) findBar(c *ui.Context, t theme.Tokens) {
	if a.active == 0 || !a.find.open {
		return
	}
	ui.Column(c).FillWidth().Children(func() {
		ui.Row(c).FillWidth().Height(40).Padding(0, gutter).Gap(8).
			AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Search).Size(12, 12).TextColor(t.MutedFg)
			field := ui.TextInputBase(c, &a.find.query).FontSize(12).Height(26).Grow(1).
				Padding(0, 8).Radius(6).Background(t.SidebarAccent).
				Placeholder(i18n.T("findPlaceholder"))
			if a.find.focus {
				a.find.focus = false
				field.Focus()
			}
			ui.Text(c, a.findCount()).FontSize(10.5).TextColor(t.MutedFg).MaxLines(1)
			a.findStepButton(c, t, "↑", i18n.T("findPrev"), -1)
			a.findStepButton(c, t, "↓", i18n.T("findNext"), 1)
			a.findStepButton(c, t, "×", i18n.T("cancel"), 0)
		})
		ui.Box(c).FillWidth().Height(1).Background(t.SidebarBorder)
	})
}

// findCount is the walk's position, or the truth when there is nothing to walk.
func (a *App) findCount() string {
	if strings.TrimSpace(a.find.query) == "" {
		return ""
	}
	if len(a.find.matches) == 0 {
		return i18n.T("findNone")
	}
	return i18n.TF("findCount", map[string]any{"n": a.find.current + 1, "total": len(a.find.matches)})
}

// findStepButton is one of the bar's three squares: up, down, and out.
func (a *App) findStepButton(c *ui.Context, t theme.Tokens, glyph, tip string, delta int) {
	btn := ui.ButtonBase(c).Size(22, 22).Radius(5).Justify(ui.Center).AlignItems(ui.Center).
		Cursor(ui.CursorPointer).Tooltip(tip)
	btn.Children(func() {
		ui.Text(c, glyph).FontSize(12).TextColor(t.MutedFg)
	})
	btn.OnClick(func() {
		if delta == 0 {
			a.find.open = false
			a.findClear()
		} else {
			a.StepFind(delta)
		}
		a.invalidate()
	})
}
