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
}

// modelRows is how many models the panel shows at once.
const modelRows = 8

// modelMatches is what the panel lists: the catalog narrowed by the filter,
// with the chat's own model first when nothing is typed.
func (a *App) modelMatches() []protocol.ModelEntry {
	filter := strings.ToLower(strings.TrimSpace(a.pick.filter))
	var out []protocol.ModelEntry
	for _, entry := range a.pick.catalog {
		if filter == "" {
			out = append(out, entry)
			continue
		}
		hay := strings.ToLower(entry.Name + " " + entry.ID + " " + entry.Provider + " " + entry.ProviderName)
		if strings.Contains(hay, filter) {
			out = append(out, entry)
		}
	}
	if filter == "" && a.model != nil {
		for i, entry := range out {
			if entry.Provider == a.model.Provider && entry.ID == a.model.ModelID {
				copy(out[1:i+1], out[:i])
				out[0] = entry
				break
			}
		}
	}
	return out
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
		start := 0
		if a.pick.sel >= modelRows {
			start = a.pick.sel - modelRows + 1
		}
		end := start + modelRows
		if end > len(matches) {
			end = len(matches)
		}
		for i := start; i < end; i++ {
			a.modelRow(c, t, matches[i], i == a.pick.sel)
		}
		if len(matches) > end-start {
			hint := i18n.TF("modelMore", map[string]any{"n": len(matches) - (end - start)})
			ui.Text(c, hint).FontSize(10).TextColor(t.MutedFg).Padding(2, 6)
		}
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
