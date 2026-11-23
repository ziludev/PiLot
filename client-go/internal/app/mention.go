package app

// Naming a file while typing.
//
// The palette finds a file and puts its path in the composer, which means
// leaving the sentence to open a dialog. A path is usually part of the
// sentence — "the bug in prefs.go" — so typing @ offers the same list in
// place: the words carry on, and the chosen path lands where the @ was.

import (
	"sort"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/icons"
	"pitta/internal/theme"
)

// mentionRows is how many candidates the popover shows before the list stops
// being a suggestion and becomes a scroll.
const mentionRows = 6

type mentionHit struct {
	// label is the line as drawn: the chat's title or the file's path.
	label string
	// insert is what replaces the @query. A path keeps its @, so the agent
	// reads it as a reference rather than as part of a sentence.
	insert string
	// dir is the file's directory, drawn faint after the name.
	dir string
	// chat marks a conversation rather than a file.
	chat bool
}

type mentionState struct {
	open  bool
	query string
	index int
	hits  []mentionHit
}

// syncMentions reads the composer for an unfinished @word and rebuilds the
// candidate list under it. It runs on every frame: the text area owns the
// text, so a frame is the only place its changes are visible.
func (a *App) syncMentions() {
	// A mention is the @ that starts the last word, if any: once a space
	// follows it the sentence has moved on.
	draft := a.draft
	at := strings.LastIndexByte(draft, '@')
	if at < 0 || strings.ContainsAny(draft[at:], " \n\t") {
		a.mention.open = false
		return
	}
	query := draft[at+1:]
	if len(query) > 64 {
		a.mention.open = false
		return
	}
	if a.repoFiles.all == nil && !a.repoFiles.busy {
		a.loadRepoFiles()
	}
	hits := a.mentionCandidates(query)
	a.mention.query = query
	if len(hits) == 0 {
		a.mention.open = false
		a.mention.hits = nil
		return
	}
	a.mention.open = true
	a.mention.hits = hits
	if a.mention.index >= len(hits) {
		a.mention.index = 0
	}
}

// mentionCandidates is the files and chats whose name contains the query, the
// ones it starts with first: "prefs" should find prefs.go before
// checkprefs_test.go, and both before a chat that mentions them.
func (a *App) mentionCandidates(query string) []mentionHit {
	q := strings.ToLower(strings.TrimSpace(query))
	var prefix, contains []mentionHit
	for _, path := range a.repoFiles.all {
		name := strings.ToLower(path)
		if q != "" && !strings.Contains(name, q) {
			continue
		}
		hit := mentionHit{label: baseName(path), insert: "@" + path, dir: dirOf(path)}
		if q != "" && strings.HasPrefix(strings.ToLower(baseName(path)), q) {
			prefix = append(prefix, hit)
			continue
		}
		contains = append(contains, hit)
		if len(prefix)+len(contains) >= mentionRows*4 {
			break
		}
	}
	for _, surface := range a.surfaceSnapshot() {
		title := strings.TrimSpace(surface.Title)
		if title == "" || (q != "" && !strings.Contains(strings.ToLower(title), q)) {
			continue
		}
		hit := mentionHit{label: title, insert: "@" + title, chat: true}
		if q != "" && strings.HasPrefix(strings.ToLower(title), q) {
			prefix = append(prefix, hit)
		} else {
			contains = append(contains, hit)
		}
	}
	// A path is what an instruction usually names, so files lead their group;
	// a chat is the rarer reference and waits behind them.
	sort.SliceStable(prefix, func(i, j int) bool {
		if prefix[i].chat != prefix[j].chat {
			return !prefix[i].chat
		}
		return prefix[i].label < prefix[j].label
	})
	out := append(prefix, contains...)
	if len(out) > mentionRows {
		out = out[:mentionRows]
	}
	return out
}

// mentionKey takes the keys the popover owns while it is open: the arrows walk
// it, Enter takes the row, Escape puts the list away and leaves the text. It
// reports whether the key was consumed.
func (a *App) mentionKey(k ui.TextKey) bool {
	if !a.mention.open {
		return false
	}
	switch k.Key {
	case ui.KeyUp:
		a.mention.index--
		if a.mention.index < 0 {
			a.mention.index = len(a.mention.hits) - 1
		}
		a.invalidate()
		return true
	case ui.KeyDown:
		a.mention.index = (a.mention.index + 1) % len(a.mention.hits)
		a.invalidate()
		return true
	case ui.KeyEnter:
		if k.Mods&ui.Shift != 0 {
			return false // a newline is still a newline
		}
		a.pickMention(a.mention.index)
		return true
	case ui.KeyEscape:
		a.mention.open = false
		a.invalidate()
		return true
	}
	return false
}

// pickMention puts the chosen row where the @word was and hands the composer
// back: the caret belongs after what was just named.
func (a *App) pickMention(index int) {
	if index < 0 || index >= len(a.mention.hits) {
		return
	}
	at := strings.LastIndexByte(a.draft, '@')
	if at < 0 {
		a.mention.open = false
		return
	}
	hit := a.mention.hits[index]
	a.draft = a.draft[:at] + hit.insert + " "
	a.mention.open = false
	a.mention.hits = nil
	a.mention.index = 0
	a.prefs.setDraft(a.active, a.draft)
	a.focusComposer = true
	a.invalidate()
}

// mentionList draws the candidates just above the input, inside the composer's
// own card so it reads as part of writing rather than as a modal over it.
func (a *App) mentionList(c *ui.Context, t theme.Tokens) {
	if !a.mention.open || len(a.mention.hits) == 0 {
		return
	}
	ui.Column(c).FillWidth().Radius(8).Border(1, t.Border).Background(t.Background).
		Padding(3, 4).Gap(1).Children(func() {
		for i, hit := range a.mention.hits {
			hot := i == a.mention.index
			row := ui.ButtonBase(c).FillWidth().Height(24).Radius(5).Padding(0, 6).
				Justify(ui.Start).AlignItems(ui.Center).Gap(6).Cursor(ui.CursorPointer)
			if hot {
				row = row.Background(t.Selection)
			}
			row.Children(func() {
				if hit.chat {
					ui.Icon(c, icons.MessagePlus).Size(11, 11).TextColor(t.MutedFg)
				} else {
					ui.Icon(c, icons.Code).Size(11, 11).TextColor(t.MutedFg)
				}
				ui.Text(c, hit.label).FontSize(11.5).TextColor(t.Foreground).MaxLines(1)
				if hit.dir != "" {
					ui.Text(c, hit.dir).FontSize(10.5).TextColor(t.MutedFg).MaxLines(1).Grow(1)
				}
			})
			row.OnClick(func() { a.pickMention(i) })
		}
	})
}

// baseName is the last segment of a path, which is what the popover shows: the
// directory is beside it in a lighter tone.
func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// dirOf is the directory part, trailing slash dropped.
func dirOf(path string) string {
	if i := strings.LastIndexByte(path, '/'); i > 0 {
		return path[:i]
	}
	return ""
}
