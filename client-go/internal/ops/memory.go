package ops

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// MemoryProps is the memory tab: the three note scopes, the curation query and
// the suggestions it proposes.
type MemoryProps struct {
	Memory      protocol.Memory
	Query       string
	Curating    bool
	Suggestions []protocol.MemorySuggestion
	// Selected marks the suggestions the reader unchecked, as the app keeps
	// them: an index with a true entry is left out and an absent one is
	// checked, which is how the client reads `selected[index] !== false`.
	Selected map[int]bool
	OnQuery  func(string)
	OnCurate func()
	OnToggle func(index int)
	OnApply  func(indices []int)
	OnDrop   func(index int)
}

// memoryFolds records which scopes the reader collapsed. Props carry no view
// state and mygo rebuilds every node each frame, so the fold lives beside them.
var memoryFolds [3]bool

// MemoryPanel draws the memory tab: the foldable note scopes above, the query
// and the curation result below.
func MemoryPanel(c *ui.Context, p MemoryProps) {
	t := theme.Current(c)
	needle := strings.ToLower(strings.TrimSpace(p.Query))
	sections := [3]struct {
		title string
		notes []protocol.Note
	}{
		{i18n.T("globals"), p.Memory.Global},
		{i18n.T("work"), p.Memory.Work},
		{i18n.T("chat"), p.Memory.Chat},
	}
	shown := 0
	filtered := [3][]protocol.Note{}
	for i, s := range sections {
		filtered[i] = filterNotes(s.notes, needle)
		shown += len(filtered[i])
	}

	ui.Column(c).FillWidth().Grow(1).Children(func() {
		heading(c, t, icons.Brain, i18n.T("memory"), "", nil)
		ui.Scroll(c).FillWidth().Grow(1).Padding(0, 12).Children(func() {
			ui.Column(c).FillWidth().Gap(12).PaddingY(4).Children(func() {
				if shown == 0 {
					if needle != "" {
						empty(c, t, i18n.T("noHits"))
					} else {
						empty(c, t, i18n.T("noMemory"), i18n.T("noMemoryHint"))
					}
					return
				}
				for i, s := range sections {
					memorySection(c, t, i, s.title, filtered[i])
				}
			})
		})
		ui.Column(c).FillWidth().Padding(8, 12).Gap(6).Children(func() {
			memoryQuery(c, t, p)
			if len(p.Suggestions) > 0 {
				memorySuggestions(c, t, p)
			}
		})
	})
}

// memorySection is one scope: its fold header, then its notes while it is open.
func memorySection(c *ui.Context, t theme.Tokens, index int, title string, notes []protocol.Note) {
	open := !memoryFolds[index]
	ui.Column(c).FillWidth().Gap(4).Children(func() {
		head := ui.Row(c).FillWidth().Padding(2, 6).Gap(4).Radius(5).
			AlignItems(ui.Center).Cursor(ui.CursorPointer).Label(title)
		if head.Hovered() {
			head.Background(t.Accent)
		}
		head.OnClick(func() { memoryFolds[index] = !memoryFolds[index] })
		head.Children(func() {
			chevron := icons.ChevronRight
			if open {
				chevron = icons.ChevronDown
			}
			ui.Icon(c, chevron).Size(10, 10).TextColor(t.MutedFg)
			caption(c, t, title+" · "+strconv.Itoa(len(notes)))
		})
		if !open {
			return
		}
		ui.Column(c).FillWidth().Gap(3).Children(func() {
			for _, note := range notes {
				memoryNote(c, t, note)
			}
		})
	})
}

// memoryNote is one line of the memory: the age in a fixed gutter, the note
// beside it. Boxed rows made fifteen notes look like fifteen cards, and the eye
// had to cross a border to read the next one; a gutter reads as a log instead.
func memoryNote(c *ui.Context, t theme.Tokens, note protocol.Note) {
	ui.Row(c).FillWidth().Gap(8).Padding(2, 0).AlignItems(ui.Start).Children(func() {
		if note.At != "" {
			ui.Text(c, noteClock(note.At)).FontSize(9).TextColor(t.MutedFg).
				MaxLines(1).Width(noteGutter)
		}
		ui.Text(c, clipNote(note.Text)).FontSize(11).TextColor(t.Foreground).Wrap().Grow(1)
	})
}

// noteGutter is wide enough for "11-14 09:12" at the note's age size.
const noteGutter = 64

// memoryQuery is the search field and the Curate trigger, pinned under the list
// because curation is what the reader comes back to after reading.
func memoryQuery(c *ui.Context, t theme.Tokens, p MemoryProps) {
	ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
		value := p.Query
		input := ui.TextInput(c, &value).Height(26).FontSize(11).Placeholder(i18n.T("searchMemory"))
		input.Grow(1)
		if p.OnQuery != nil {
			input.OnChange(func() { p.OnQuery(value) })
		}
		label := i18n.T("curate")
		if p.Curating {
			label = i18n.T("curating")
		}
		button(c, t, icons.Wand, label, false, p.Curating, p.OnCurate)
	})
}

// memorySuggestions lists what Curate proposed: one row per fact, with the
// check box that picks it, the scope it would land in and the reason it was
// kept. Apply submits the rows still checked.
func memorySuggestions(c *ui.Context, t theme.Tokens, p MemoryProps) {
	ui.Column(c).FillWidth().Gap(6).Padding(8).Radius(theme.Radius).
		Background(t.Background).Border(1, t.Primary).Children(func() {
		ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Wand).Size(10, 10).TextColor(t.MutedFg)
			caption(c, t, i18n.T("suggestions")+" · "+strconv.Itoa(len(p.Suggestions)))
		})
		ui.Scroll(c).FillWidth().MaxHeight(112).Children(func() {
			ui.Column(c).FillWidth().Gap(4).Children(func() {
				for i, s := range p.Suggestions {
					memorySuggestion(c, t, i, s, suggestionChecked(p, i), p.OnToggle, p.OnDrop)
				}
			})
		})
		picked := pickedSuggestions(p)
		var apply func()
		if len(picked) > 0 {
			apply = func() {
				if p.OnApply != nil {
					p.OnApply(picked)
				}
			}
		}
		button(c, t, icons.Check, i18n.T("apply")+" "+strconv.Itoa(len(picked)), true, len(picked) == 0, apply)
	})
}

// suggestionChecked reports whether row index is checked. Selected keeps the
// rows the reader unchecked, so only a true entry leaves one out.
func suggestionChecked(p MemoryProps, index int) bool {
	return !p.Selected[index]
}

// pickedSuggestions lists the checked rows in the order they arrived; Apply
// submits these indexes only, and an empty list disables it.
func pickedSuggestions(p MemoryProps) []int {
	picked := make([]int, 0, len(p.Suggestions))
	for i := range p.Suggestions {
		if suggestionChecked(p, i) {
			picked = append(picked, i)
		}
	}
	return picked
}

// memorySuggestion is one proposed fact: its check box, the fact and where it
// would land, and the glyph that drops it from the list.
func memorySuggestion(c *ui.Context, t theme.Tokens, index int, s protocol.MemorySuggestion, checked bool, toggle, drop func(int)) {
	ui.Row(c).FillWidth().Gap(6).Padding(3, 2).AlignItems(ui.Start).Children(func() {
		box := checked
		checkbox := ui.Checkbox(c, &box, "").Label(s.Text)
		if toggle != nil {
			checkbox.OnChange(func() { toggle(index) })
		}
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Text(c, s.Text).FontSize(11).TextColor(t.Foreground).Wrap()
			ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
				if s.Scope != "" {
					badge(c, t, s.Scope, t.Accent, t.AccentFg)
				}
				if s.Reason != "" {
					ui.Text(c, s.Reason).FontSize(10).TextColor(t.MutedFg).Wrap().Grow(1)
				}
			})
		})
		glyph(c, t, icons.X, dropLabel, func() {
			if drop != nil {
				drop(index)
			}
		})
	})
}

// filterNotes keeps the notes whose text contains needle, which the caller has
// already lowercased.
func filterNotes(notes []protocol.Note, needle string) []protocol.Note {
	if needle == "" {
		return notes
	}
	kept := make([]protocol.Note, 0, len(notes))
	for _, note := range notes {
		if strings.Contains(strings.ToLower(note.Text), needle) {
			kept = append(kept, note)
		}
	}
	return kept
}

// clipNote keeps one long note from burying the rest of its scope; the client
// slices at 220 characters too.
func clipNote(text string) string {
	const limit = 220
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
