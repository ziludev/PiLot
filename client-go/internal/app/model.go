package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// The model picker. A chat keeps its own model, so this is a property of the
// chat being read, not a global setting — hence the chip in the composer rather
// than a row in the rail.
//
// The panel is inline, above the composer, and not a popover: mygo's popover
// treats Enter as its own close key, and Enter is how you choose here. The
// filter takes the keyboard while it is open, so typing narrows the list and
// never reaches the message.

// modelPick is the panel's state between frames.
type modelPick struct {
	open   bool
	focus  bool
	filter string
	seen   string
	sel    int
	// catalog is the whole list from the bridge, fetched when the panel first
	// opens: 756 rows are not worth a round trip before anyone asks.
	catalog []protocol.ModelEntry
	fetched bool
	failed  bool
	// list is the panel's scroll: the catalog is 756 rows across a dozen
	// providers, and a window of eight with no wheel is a worse answer than
	// the list itself.
	list ui.ListState
}

// modelPanelHeight is how tall the list is: about seven rows, then the wheel.
const modelPanelHeight = 244

// modelRowSpan is roughly how much room a row takes, which is what the panel's
// height is made of when the list is shorter than the panel.
const modelRowSpan = 26

// modelRecents is how many models the panel remembers having been chosen from.
const modelRecents = 5

// modelMatches is what the panel lists: the catalog narrowed by the filter. With
// nothing typed it is ordered by what the person has actually used — the chat's
// model, the models chosen before, the default's provider — because the
// catalog's own order starts at whichever aggregator sorts first.
func (a *App) modelMatches() []protocol.ModelEntry {
	filter := strings.ToLower(strings.TrimSpace(a.pick.filter))
	if filter == "" {
		return a.modelOrdered()
	}
	var out []protocol.ModelEntry
	for _, entry := range a.pick.catalog {
		hay := strings.ToLower(entry.Name + " " + entry.ID + " " + entry.Provider + " " + entry.ProviderName)
		if strings.Contains(hay, filter) {
			out = append(out, entry)
		}
	}
	return out
}

// modelOrdered is the panel with nothing typed: current, recent, the default
// provider, then the rest of the catalog as the bridge lists it.
func (a *App) modelOrdered() []protocol.ModelEntry {
	seen := make(map[string]bool, len(a.pick.catalog))
	out := make([]protocol.ModelEntry, 0, len(a.pick.catalog))
	add := func(entry protocol.ModelEntry) {
		key := modelKey(entry)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, entry)
	}
	if entry := a.modelEntry(a.model); entry != nil {
		add(*entry)
	}
	for _, key := range a.prefs.Recent {
		if entry := a.modelEntryByKey(key); entry != nil {
			add(*entry)
		}
	}
	provider := ""
	if a.defaultModel != nil {
		provider = a.defaultModel.Provider
	}
	if provider == "" && a.model != nil {
		provider = a.model.Provider
	}
	for _, entry := range a.pick.catalog {
		if entry.Provider == provider {
			add(entry)
		}
	}
	for _, entry := range a.pick.catalog {
		add(entry)
	}
	return out
}

// modelKey names a model the way prefs remembers it: provider and id.
func modelKey(entry protocol.ModelEntry) string {
	return entry.Provider + "/" + entry.ID
}

// modelEntryByKey is the catalog's row for a remembered key.
func (a *App) modelEntryByKey(key string) *protocol.ModelEntry {
	provider, id, ok := strings.Cut(key, "/")
	if !ok {
		return nil
	}
	return a.modelEntry(&protocol.ModelRef{Provider: provider, ModelID: id})
}

// rememberModel puts a chosen model at the head of the panel's recent list.
func (a *App) rememberModel(entry protocol.ModelEntry) {
	key := modelKey(entry)
	recent := []string{key}
	for _, k := range a.prefs.Recent {
		if k != key && len(recent) < modelRecents {
			recent = append(recent, k)
		}
	}
	a.prefs.Recent = recent
	savePrefs(a.prefs)
}

// modelEntry is the catalog's row for a reference, nil when it is not there.
func (a *App) modelEntry(ref *protocol.ModelRef) *protocol.ModelEntry {
	if ref == nil {
		return nil
	}
	for i, entry := range a.pick.catalog {
		if entry.Provider == ref.Provider && entry.ID == ref.ModelID {
			return &a.pick.catalog[i]
		}
	}
	return nil
}

// modelName is what the chip says: the catalog's name when it has one, the id's
// last segment when the catalog has not caught up.
func (a *App) modelName() string {
	ref := a.model
	if ref == nil {
		ref = a.defaultModel
	}
	if entry := a.modelEntry(ref); entry != nil && entry.Name != "" {
		return entry.Name
	}
	if ref == nil {
		return i18n.T("model")
	}
	if i := strings.LastIndex(ref.ModelID, "/"); i >= 0 {
		return ref.ModelID[i+1:]
	}
	return ref.ModelID
}

// openModelPicker shows the panel and asks for the catalog the first time.
func (a *App) openModelPicker() {
	if a.pick.open {
		a.pick.open = false
		return
	}
	a.pick.open = true
	a.pick.focus = true
	a.pick.filter = ""
	a.pick.seen = ""
	a.pick.sel = 0
	if a.pick.fetched {
		return
	}
	a.pick.fetched = true
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		list, err := a.api.Models(ctx)
		a.post(event{kind: evModels, models: list, err: err})
	}()
}

// chooseModel points the chat at a model and closes the panel. The level the
// chat already had is kept only when the new model takes that level.
func (a *App) chooseModel(entry protocol.ModelEntry) {
	level := ""
	for _, l := range entry.ThinkingLevels {
		if l == a.thinking {
			level = l
		}
	}
	a.rememberModel(entry)
	a.model = &protocol.ModelRef{Provider: entry.Provider, ModelID: entry.ID}
	a.thinking = level
	a.pick.open = false
	a.pick.filter = ""
	a.setModel(entry.Provider, entry.ID, level)
}

// setLevel changes the thinking level of the chat's current model.
func (a *App) setLevel(level string) {
	if a.model == nil {
		return
	}
	if level == a.thinking {
		return
	}
	a.thinking = level
	a.setModel(a.model.Provider, a.model.ModelID, level)
}

// setModel writes the choice. What is on screen moves first; a bridge that
// refuses gets the chat reloaded under the panel, so the next frame is true
// again rather than politely wrong.
func (a *App) setModel(provider, id, level string) {
	conv := a.active
	ref := protocol.ModelRef{Provider: provider, ModelID: id}
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		err := a.api.SetModel(ctx, conv, ref, level)
		a.post(event{kind: evModelSet, convID: conv, err: err})
	}()
}

// movePick walks the list, wrapping at both ends so holding down does not park
// on the edge.
func (a *App) movePick(delta int) {
	n := len(a.modelMatches())
	if n == 0 {
		return
	}
	a.pick.sel = (a.pick.sel + delta + n) % n
	a.pick.list.ScrollIntoView(a.pick.sel)
}

// choosePick takes the highlighted row; Enter and a click both land here.
func (a *App) choosePick() {
	matches := a.modelMatches()
	if len(matches) == 0 {
		return
	}
	if a.pick.sel >= len(matches) {
		a.pick.sel = len(matches) - 1
	}
	a.chooseModel(matches[a.pick.sel])
}

// modelChip is the line in the composer that says what will answer, and the
// only way into the panel.
func (a *App) modelChip(c *ui.Context, t theme.Tokens) {
	label := a.modelName()
	if a.thinking != "" {
		label += " · " + a.thinking
	}
	button := ui.ButtonBase(c).Height(24).Radius(6).Padding(0, 7).Gap(4).
		AlignItems(ui.Center).Cursor(ui.CursorPointer).Tooltip(i18n.T("model"))
	if a.pick.open {
		button = button.Background(t.Muted)
	}
	button.Children(func() {
		ui.Text(c, clip(label, 26)).FontSize(11).TextColor(t.MutedFg).MaxLines(1)
	})
	button.OnClick(a.openModelPicker)
}

// modelPanel is the list above the composer.
func (a *App) modelPanel(c *ui.Context, t theme.Tokens) {
	// A new filter starts at the top of its own results.
	if a.pick.seen != a.pick.filter {
		a.pick.seen = a.pick.filter
		a.pick.sel = 0
		a.pick.list.ScrollTo(0, ui.Start)
	}
	ui.Column(c).Width(380).Padding(8, 8, 8, 8).Radius(10).Gap(4).
		Border(1, t.Border).Background(t.Card).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			input := ui.TextInputBase(c, &a.pick.filter).FillWidth().Height(28).
				FontSize(11.5).Placeholder(i18n.T("modelSearch"))
			if a.pick.focus {
				input.AutoFocus()
				a.pick.focus = false
			}
			input.OnTextKey(a.modelKey)
		})
		switch {
		case a.pick.failed:
			ui.Text(c, i18n.T("modelFailed")).FontSize(11).TextColor(t.Destructive).Padding(4, 0)
			return
		case !a.pick.fetched || len(a.pick.catalog) == 0:
			ui.Text(c, i18n.T("loading")).FontSize(11).TextColor(t.MutedFg).Padding(4, 0)
			return
		}
		matches := a.modelMatches()
		if len(matches) == 0 {
			ui.Text(c, i18n.T("noResults")).FontSize(11).TextColor(t.MutedFg).Padding(4, 0)
			return
		}
		if a.pick.sel >= len(matches) {
			a.pick.sel = len(matches) - 1
		}
		// As tall as its rows, up to where the wheel takes over: a panel for
		// four models should not be a 244pt hole under them.
		height := float32(modelPanelHeight)
		if want := float32(len(matches) * modelRowSpan); want < height {
			height = want
		}
		ui.List(c, &a.pick.list, len(matches), func(row int) {
			a.modelRow(c, t, matches[row], row == a.pick.sel)
		}).Height(height)
		a.levelChips(c, t)
	})
}

// modelRow is one candidate: what it is called, where it comes from, and what
// it costs, since the last two are what decide between two similar names.
func (a *App) modelRow(c *ui.Context, t theme.Tokens, entry protocol.ModelEntry, selected bool) {
	row := ui.ButtonBase(c).FillWidth().Padding(5, 6).Radius(6).Gap(8).
		AlignItems(ui.Center).Cursor(ui.CursorPointer)
	if selected {
		row = row.Background(t.SidebarAccent)
	}
	row.Children(func() {
		ui.Text(c, clip(entry.Name, 34)).FontSize(11.5).FontWeight(500).MaxLines(1)
		ui.Text(c, modelMeta(entry)).Grow(1).FontSize(10).TextColor(t.MutedFg).
			MaxLines(1).TextAlign(ui.End)
	})
	chosen := entry
	row.OnClick(func() { a.chooseModel(chosen) })
}

// modelMeta is the right-hand column: provider, context, price per million.
func modelMeta(entry protocol.ModelEntry) string {
	provider := entry.ProviderName
	if provider == "" {
		provider = entry.Provider
	}
	meta := provider
	if entry.ContextWindow > 0 {
		meta += fmt.Sprintf(" · %dk", entry.ContextWindow/1000)
	}
	if entry.Cost.Input > 0 || entry.Cost.Output > 0 {
		meta += fmt.Sprintf(" · $%g/$%g", entry.Cost.Input, entry.Cost.Output)
	}
	return meta
}

// levelChips is the thinking level of the chat's model. Only models that take a
// level show it, and only when there is a choice to make.
func (a *App) levelChips(c *ui.Context, t theme.Tokens) {
	entry := a.modelEntry(a.model)
	if entry == nil || len(entry.ThinkingLevels) < 2 {
		return
	}
	ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Padding(4, 2).Children(func() {
		ui.Text(c, i18n.T("thinking")).FontSize(10).TextColor(t.MutedFg).Padding(0, 2)
		for _, level := range entry.ThinkingLevels {
			chip := ui.ButtonBase(c).Height(20).Radius(5).Padding(0, 6).
				Justify(ui.Center).AlignItems(ui.Center).Cursor(ui.CursorPointer).
				Background(t.Muted)
			fg, weight := t.MutedFg, 400
			if level == a.thinking {
				chip = chip.Background(t.SidebarAccent)
				fg, weight = t.Foreground, 500
			}
			chip.Children(func() {
				ui.Text(c, level).FontSize(10).FontWeight(weight).TextColor(fg).MaxLines(1)
			})
			chosen := level
			chip.OnClick(func() { a.setLevel(chosen) })
		}
	})
}

// modelKey is the panel's keyboard: Enter takes the highlighted row, Esc closes,
// and the arrows walk the list. Everything else goes to the filter.
func (a *App) modelKey(k ui.TextKey) bool {
	switch {
	case k.Key == ui.KeyEscape && k.Mods == 0:
		a.pick.open = false
		return true
	case k.Key == ui.KeyEnter && k.Mods == 0:
		a.choosePick()
		return true
	case k.Key == ui.KeyUp && k.Mods == 0:
		a.movePick(-1)
		return true
	case k.Key == ui.KeyDown && k.Mods == 0:
		a.movePick(1)
		return true
	}
	return false
}

// applyModels keeps the catalog. A failed fetch leaves the panel saying so,
// since an empty list and an unreachable bridge look the same otherwise.
func (a *App) applyModels(list *protocol.Models, err error) {
	if err != nil || list == nil {
		a.pick.failed = true
		a.pick.fetched = false
		return
	}
	a.pick.failed = false
	a.pick.catalog = list.Models
	a.defaultModel = list.Default
}

// applyModelSet is the write's answer. On failure the chat is reloaded so the
// chip goes back to the model the bridge actually has.
func (a *App) applyModelSet(convID int, err error) {
	if err == nil {
		return
	}
	a.post(event{kind: evNotice, notice: i18n.T("modelFailed")})
	if convID == a.active {
		a.loadThread(convID)
	}
}
