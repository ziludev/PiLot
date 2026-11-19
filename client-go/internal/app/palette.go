package app

import (
	"sort"
	"strings"

	"pitta/internal/i18n"
	"pitta/internal/theme"

	"github.com/egoist/mygo/ui"
)

// paletteMaxRows is how many answers ⌘K shows. Past this the list stops being
// something the eye scans and becomes something you scroll, which is the thing
// the palette exists to avoid. It has to stay at or above the verb count, or the
// last verb is unreachable with an empty query; the verbs rank first and the
// chats follow, so the field is what shortens the list.
const paletteMaxRows = 14

// paletteState is the ⌘K palette: one field that either opens a chat or runs a
// verb. It keeps the query and where the reader is standing, and nothing else —
// the rows are a function of the window's state and that query, so a chat that
// arrives while the palette is open is findable at once.
type paletteState struct {
	open bool
	// mode is which list is showing: the window's verbs and chats (⌘K), or the
	// checkout's files (⌘P). The two share the field, the keys and the ranker.
	mode  paletteMode
	query string
	// index is the row the reader is standing on. It is clamped on every draw,
	// because the list shrinks as the query narrows.
	index int
}

// paletteMode is which list the dialog is showing.
type paletteMode int

const (
	paletteCommands paletteMode = iota
	paletteFiles
)

// openPalette arms the palette with an empty query. Every opening starts fresh:
// a leftover query would hide the verb the reader came for.
func (a *App) openPalette() {
	a.palette.open = true
	a.palette.mode = paletteCommands
	a.palette.query = ""
	a.palette.index = 0
	a.invalidate()
}

func (a *App) closePalette() {
	if !a.palette.open {
		return
	}
	a.palette.open = false
	a.invalidate()
}

// paletteRow is one answer: a verb to run, or a chat to open.
type paletteRow struct {
	label  string
	detail string
	// hint is the key that does the same thing without the palette, for the
	// reader who would rather learn the key than keep the palette in mind.
	hint string
	// alias is the same label in the other language, so the palette can be
	// searched in either one.
	alias string
	// slug identifies the row across frames, so the highlight stays on the row
	// the reader chose instead of jumping back to the top on every keystroke.
	slug  string
	score int
	run   func()
}

// paletteRows is the whole palette: verbs first, then chats, ranked by how well
// they answer the query. With no query the verbs keep their order and the most
// recent chats follow, which is what an empty palette should be.
func (a *App) paletteRows() []paletteRow {
	if a.palette.mode == paletteFiles {
		return a.repoFileRows()
	}
	query := strings.TrimSpace(a.palette.query)
	rows := make([]paletteRow, 0, 24)
	add := func(row paletteRow) {
		score, ok := paletteMatch(query, strings.TrimSpace(row.label+" "+row.alias+" "+row.detail))
		if !ok {
			return
		}
		row.score = score
		rows = append(rows, row)
	}
	for _, verb := range a.paletteVerbs() {
		add(verb)
	}
	for _, chat := range a.paletteChats() {
		add(chat)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].score > rows[j].score })
	if len(rows) > paletteMaxRows {
		rows = rows[:paletteMaxRows]
	}
	return rows
}

// paletteVerbs is the list of things the window can be told to do from a
// keyboard. Each one is the same call the button makes, so the palette can never
// drift from the chrome.
func (a *App) paletteVerbs() []paletteRow {
	verbs := []paletteRow{
		{label: i18n.T("newChat"), alias: i18n.Alt("newChat"), hint: "⌘N", slug: "verb.new", run: a.newChat},
		{label: i18n.T("findMenu"), alias: i18n.Alt("findMenu"), hint: "⌘F", slug: "verb.find", run: a.ToggleFind},
		{label: i18n.T("exportMarkdown"), alias: i18n.Alt("exportMarkdown"), hint: "⇧⌘E", slug: "verb.export", run: a.exportMarkdown},
		{label: i18n.T("shortcuts"), alias: i18n.Alt("shortcuts"), hint: "⌘⇧/", slug: "verb.shortcuts", run: a.toggleCheatsheet},
		{label: i18n.T("exportHTML"), alias: i18n.Alt("exportHTML"), slug: "verb.html", run: a.exportHTML},
		{label: i18n.T("railGroupVerb"), alias: i18n.Alt("railGroupVerb"), slug: "verb.group", run: a.toggleRailGroup},
		{label: i18n.T("gotoFile"), alias: i18n.Alt("gotoFile"), hint: "⌘P", slug: "verb.file", run: a.openFiles},
		{label: i18n.T("jumpLatest"), alias: i18n.Alt("jumpLatest"), hint: "⌘J", slug: "verb.latest", run: a.scrollToLatest},
		{label: i18n.T("changesTab"), alias: i18n.Alt("changesTab"), slug: "verb.changes", run: a.toggleChanges},
		{label: i18n.T("memory"), alias: i18n.Alt("memory"), slug: "verb.memory", run: a.toggleMemory},
		{label: i18n.T("projects"), alias: i18n.Alt("projects"), slug: "verb.projects", run: a.toggleProjects},
		{label: i18n.T("settings"), alias: i18n.Alt("settings"), slug: "verb.settings", run: a.toggleSettings},
		{label: i18n.T("restartAgent"), alias: i18n.Alt("restartAgent"), slug: "verb.restart", run: a.restartAgent},
	}
	return verbs
}

// paletteChats is every chat the rail knows about, newest first, so typing a few
// letters of a title lands on it without first working out which shelf it is on.
func (a *App) paletteChats() []paletteRow {
	surfaces := a.sortedSurfaces()
	rows := make([]paletteRow, 0, len(surfaces))
	for _, s := range surfaces {
		convID := s.ConversationID
		title := a.chatTitle(convID)
		detail := s.ProjectID
		rows = append(rows, paletteRow{
			label:  title,
			detail: detail,
			slug:   "chat." + itoa(convID),
			run:    func() { a.selectConversation(convID) },
		})
	}
	return rows
}

// paletteMatch scores how well a typed query covers a row: a case-folded
// subsequence, where a letter that starts a word and a run of adjacent letters
// are worth more, and a shorter label wins ties against a long label that also
// matches.
func paletteMatch(query, text string) (int, bool) {
	if query == "" {
		return 0, true
	}
	q := []rune(strings.ToLower(query))
	t := []rune(strings.ToLower(text))
	score, at, run := 0, 0, 0
	for i := 0; i < len(t) && at < len(q); i++ {
		if t[i] != q[at] {
			run = 0
			continue
		}
		score += 10
		if i == 0 || strings.ContainsRune(" /-_.:(", t[i-1]) {
			score += 12
		}
		if run > 0 {
			score += 8 * run
		}
		run++
		at++
	}
	if at < len(q) {
		return 0, false
	}
	return score - len(t)/8, true
}

// paletteDialog draws the palette over the window. The rows are recomputed on
// each draw rather than stored, and the field keeps the focus while it is open.
func (a *App) paletteDialog(c *ui.Context, t theme.Tokens) {
	rows := a.paletteRows()
	if !a.palette.open {
		return
	}
	if a.palette.index >= len(rows) {
		a.palette.index = len(rows) - 1
	}
	if a.palette.index < 0 {
		a.palette.index = 0
	}

	// run closes the palette first: the verb may open another dialog, and the
	// two modals would fight over the keyboard.
	run := func(i int) {
		if i < 0 || i >= len(rows) {
			return
		}
		row := rows[i]
		a.closePalette()
		row.run()
	}
	step := func(delta int) {
		if len(rows) == 0 {
			return
		}
		a.palette.index = (a.palette.index + delta + len(rows)) % len(rows)
		a.invalidate()
	}

	ui.Modal(c, &a.palette.open, func() {
		ui.Column(c).Width(480).Gap(6).Padding(10).Radius(12).
			Background(t.Popover).Border(1, t.Border).
			Shadow(0, 12, 40, 0, ui.RGBA(0, 0, 0, 0.35)).
			Children(func() {
				placeholder := i18n.T("palettePlaceholder")
				if a.palette.mode == paletteFiles {
					placeholder = i18n.T("filesPlaceholder")
				}
				field := ui.TextInputBase(c, &a.palette.query).
					Height(34).Padding(0, 10).FontSize(13).Radius(8).
					Background(t.SidebarAccent).TextColor(t.Foreground).
					Placeholder(placeholder)
				field.AutoFocus()
				field.OnTextKey(func(k ui.TextKey) bool {
					switch k.Key {
					case ui.KeyDown:
						step(1)
						return true
					case ui.KeyUp:
						step(-1)
						return true
					case ui.KeyEnter:
						run(a.palette.index)
						return true
					case ui.KeyEscape:
						a.closePalette()
						return true
					}
					return false
				})

				if len(rows) == 0 {
					empty := i18n.T("paletteEmpty")
					if a.palette.mode == paletteFiles {
						empty = a.filesEmptyLabel()
					}
					ui.Text(c, empty).
						Padding(8, 10).FontSize(11.5).TextColor(t.MutedFg)
					return
				}
				for i, row := range rows {
					chosen := i == a.palette.index
					item := ui.ButtonBase(c).FillWidth().Height(32).Padding(0, 10).Gap(8).
						Radius(7).AlignItems(ui.Center).Cursor(ui.CursorPointer)
					if chosen {
						// A tinted row, not a slightly different white: the reader has
						// to see where ⏎ will land without hunting for it. The theme's
						// neutral accent is all but invisible on a popover, so the
						// selection tint carries it.
						item = item.Background(t.Selection.Alpha(0.45)).Border(1, t.Selection.Alpha(0.7))
					}
					item.Children(func() {
						ui.Text(c, row.label).FontSize(12).MaxLines(1).TextColor(t.Foreground)
						ui.Spacer(c)
						if row.detail != "" {
							ui.Text(c, row.detail).FontSize(10.5).MaxLines(1).TextColor(t.MutedFg)
						}
						if row.hint != "" {
							ui.Text(c, row.hint).FontSize(10).TextColor(t.MutedFg)
						}
					})
					clicked := i
					item.OnClick(func() { run(clicked) })
				}

				hint := i18n.T("paletteHint")
				if a.palette.mode == paletteFiles {
					hint = a.filesHintText()
				}
				ui.Text(c, hint).
					Padding(2, 10).FontSize(10).TextColor(t.MutedFg)
			})
	})
}
