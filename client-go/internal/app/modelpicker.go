package app

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// modelKey is the identity the favourites and recents lists store, the same
// `provider/id` shape ModelPicker.tsx keeps in localStorage.
func modelKey(ref protocol.ModelRef) string {
	return ref.Provider + "/" + ref.ModelID
}

// parseModelKey splits a stored key back into its two halves.
func parseModelKey(key string) (provider, modelID string, ok bool) {
	provider, modelID, ok = strings.Cut(key, "/")
	return provider, modelID, ok && provider != "" && modelID != ""
}

// modelPicker anchors the command palette under the composer's model label:
// search, favourites, recents, provider groups and the thinking level.
// modelKey takes the keys the palette owns before the input acts on them, so
// Enter picks instead of closing and the arrows walk the list while typing
// still filters.
func (a *App) modelKey(c *ui.Context, k ui.TextKey) bool {
	switch k.Key {
	case ui.KeyDown:
		a.walkModels(1)
		return true
	case ui.KeyUp:
		a.walkModels(-1)
		return true
	case ui.KeyEnter:
		a.pickModel()
		return true
	}
	return false
}

// walkModels moves the highlight by step, wrapping at both ends, the way the
// arrow keys behave in ModelPicker.tsx's palette.
func (a *App) walkModels(step int) {
	models := a.visibleModels()
	if len(models) == 0 {
		return
	}
	a.apply(func() {
		a.modelCursor = (a.modelCursor + step + len(models)) % len(models)
		a.invalidate()
	})
}

// pickModel takes the highlighted row, if any.
func (a *App) pickModel() {
	models := a.visibleModels()
	if a.modelCursor < 0 || a.modelCursor >= len(models) {
		return
	}
	picked := models[a.modelCursor]
	a.chooseModel(protocol.ModelRef{Provider: picked.Provider, ModelID: picked.ID})
}

// modelPicker is the button in the composer that opens the model palette. The
// list itself is drawn inline above the composer rather than as a popover: a
// popover takes Enter for its own dismiss, which is the key the palette needs
// for picking a row.
func (a *App) modelPicker(c *ui.Context) {
	t := theme.Current(c)
	ui.Button(c, a.modelLabel()).FontSize(10).Padding(3, 8).TextColor(t.MutedFg).
		OnClick(func() {
			a.apply(func() {
				a.modelOpen = !a.modelOpen
				if a.modelOpen {
					a.modelQuery = ""
					a.modelCursor = 0
				}
				a.invalidate()
			})
		})
}

// modelPalette is the command palette of ModelPicker.tsx: a search field, the
// models ordered favourites-first, and the thinking levels of the chosen one.
func (a *App) modelPalette(c *ui.Context) {
	t := theme.Current(c)
	ui.Column(c).FillWidth().Padding(10).Gap(6).Background(t.Popover).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Children(func() {
		ui.TextInput(c, &a.modelQuery).FillWidth().FontSize(11).
			Placeholder(i18n.T("searchModel")).Label(i18n.T("searchModel")).
			OnTextKey(func(k ui.TextKey) bool { return a.modelKey(c, k) }).
			OnSubmit(func() { a.pickModel() })
		ui.Scroll(c).Height(200).Children(func() {
			ui.Column(c).FillWidth().Gap(2).Children(func() {
				models := a.visibleModels()
				for i, entry := range models {
					a.modelRowAt(c, t, entry, i == a.modelCursor)
				}
			})
		})
		a.thinkingRow(c, t)
	})
}

// visibleModels is the list the picker draws: favourites first, then recents,
// then the rest by provider, all narrowed by the query.
func (a *App) visibleModels() []protocol.ModelEntry {
	query := strings.ToLower(strings.TrimSpace(a.modelQuery))
	rank := func(entry protocol.ModelEntry) int {
		key := modelKey(protocol.ModelRef{Provider: entry.Provider, ModelID: entry.ID})
		for _, fav := range a.modelsFav {
			if fav == key {
				return 0
			}
		}
		for index, recent := range a.modelsRecent {
			if recent == key {
				return 1 + index
			}
		}
		return 1000
	}
	var matched []protocol.ModelEntry
	for _, entry := range a.models {
		if query != "" {
			haystack := strings.ToLower(entry.Name + " " + entry.Provider + " " + entry.ID)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		matched = append(matched, entry)
	}
	sort.SliceStable(matched, func(i, j int) bool {
		ri, rj := rank(matched[i]), rank(matched[j])
		if ri != rj {
			return ri < rj
		}
		return matched[i].Name < matched[j].Name
	})
	return matched
}

// modelRow is one entry: initial, name, provider, price, context and star.
func (a *App) modelRow(c *ui.Context, t theme.Tokens, entry protocol.ModelEntry) {
	a.modelRowAt(c, t, entry, false)
}

// modelRowAt draws a row and marks it when the arrow keys stand on it.
func (a *App) modelRowAt(c *ui.Context, t theme.Tokens, entry protocol.ModelEntry, cursor bool) {
	ref := protocol.ModelRef{Provider: entry.Provider, ModelID: entry.ID}
	active := a.model != nil && *a.model == ref
	starred := false
	for _, fav := range a.modelsFav {
		if fav == modelKey(ref) {
			starred = true
			break
		}
	}
	initial := "?"
	if entry.Name != "" {
		initial = strings.ToUpper(string([]rune(entry.Name)[0]))
	}
	row := ui.Row(c).Fill().Gap(6).Padding(5, 6).Radius(6)
	if cursor {
		row = row.Background(t.Accent)
	}
	row.Children(func() {
		ui.Column(c).Width(20).Height(20).Radius(4).Background(t.Muted).Padding(4, 0).Children(func() {
			ui.Text(c, initial).FontSize(10).TextColor(t.MutedFg)
		})
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Row(c).Gap(4).Children(func() {
				ui.Text(c, entry.Name).FontSize(12).SingleLine()
				if active {
					ui.Icon(c, icons.Check).Size(10, 10).TextColor(t.Primary)
				}
			})
			ui.Text(c, modelPrice(entry, i18n.Current())+" · "+modelContext(entry.ContextWindow)).
				FontSize(10).TextColor(t.MutedFg).SingleLine()
		})
		ui.Icon(c, icons.Star).Size(11, 11).
			TextColor(map[bool]ui.Color{true: t.Warning, false: t.Border}[starred]).
			OnClick(func() {
				a.apply(func() { a.toggleFavorite(ref) })
			})
	}).OnClick(func() {
		a.chooseModel(ref)
	})
}

// toggleFavorite stars or unstars a model and keeps the order stable.
func (a *App) toggleFavorite(ref protocol.ModelRef) {
	key := modelKey(ref)
	for index, fav := range a.modelsFav {
		if fav == key {
			a.modelsFav = append(a.modelsFav[:index], a.modelsFav[index+1:]...)
			a.savePrefsLater()
			a.invalidate()
			return
		}
	}
	a.modelsFav = append(a.modelsFav, key)
	a.savePrefsLater()
	a.invalidate()
}

// chooseModel asks the bridge to switch this conversation's model and records
// it as recent.
func (a *App) chooseModel(ref protocol.ModelRef) {
	current := a.currentValue()
	a.apply(func() {
		a.model = &ref
		a.modelOpen = false
		a.rememberRecent(modelKey(ref))
		a.invalidate()
	})
	if current == 0 {
		return
	}
	choice := api.ModelChoice{Provider: ref.Provider, ModelID: ref.ModelID, ThinkingLevel: a.thinkingLevel()}
	a.goLoad(func() error {
		if err := a.api.SetModel(context.Background(), current, choice); err != nil {
			return err
		}
		a.apply(func() { a.model = &ref })
		return nil
	})
}

// rememberRecent moves a model to the front of the recents list, capped so the
// list stays a shortcut.
func (a *App) rememberRecent(key string) {
	kept := make([]string, 0, len(a.modelsRecent)+1)
	kept = append(kept, key)
	for _, recent := range a.modelsRecent {
		if recent != key && len(kept) < 6 {
			kept = append(kept, recent)
		}
	}
	a.modelsRecent = kept
	a.savePrefsLater()
}

// thinkingLevel is the level the conversation runs at; the backend's own
// default applies when none is chosen.
func (a *App) thinkingLevel() string {
	if a.thinking != "" {
		return a.thinking
	}
	return "high"
}

// thinkingRow is the level picker under the list: as many steps as the active
// model offers.
func (a *App) thinkingRow(c *ui.Context, t theme.Tokens) {
	levels := a.modelLevels()
	if len(levels) == 0 {
		return
	}
	ui.Divider(c).Background(t.Border)
	ui.Row(c).Gap(4).Children(func() {
		ui.Text(c, i18n.T("thinking")).FontSize(10).TextColor(t.MutedFg)
		for _, level := range levels {
			active := level == a.thinkingLevel()
			ui.Button(c, levelLabel(level)).FontSize(10).Padding(2, 7).
				Background(map[bool]ui.Color{true: t.Primary, false: t.Muted}[active]).
				TextColor(map[bool]ui.Color{true: t.PrimaryFg, false: t.MutedFg}[active]).
				OnClick(func() { a.chooseThinking(level) })
		}
	})
}

// modelLevels is what the active model offers, falling back to the usual
// ladder when the bridge did not list any.
func (a *App) modelLevels() []string {
	if a.model != nil {
		for _, entry := range a.models {
			if entry.Provider == a.model.Provider && entry.ID == a.model.ModelID {
				if len(entry.ThinkingLevels) > 0 {
					return entry.ThinkingLevels
				}
				return nil
			}
		}
	}
	return nil
}

// chooseThinking switches the level and tells the bridge.
func (a *App) chooseThinking(level string) {
	a.apply(func() {
		a.thinking = level
		a.invalidate()
	})
	ref := a.model
	if ref == nil {
		return
	}
	current := a.currentValue()
	if current == 0 {
		return
	}
	a.goLoad(func() error {
		return a.api.SetModel(context.Background(), current, api.ModelChoice{
			Provider: ref.Provider, ModelID: ref.ModelID, ThinkingLevel: level,
		})
	})
}

// levelNames is ModelPicker.tsx's LEVEL_LABEL: the thinking ladder is a
// provider constant, not a translatable message, so it lives here.
var levelNames = map[i18n.Lang]map[string]string{
	i18n.EN: {"off": "off", "minimal": "min", "low": "low", "medium": "mid", "high": "high", "xhigh": "xhigh", "max": "max"},
	i18n.ZH: {"off": "关", "minimal": "极低", "low": "低", "medium": "中", "high": "高", "xhigh": "很高", "max": "最高"},
}

// levelLabel is the short word for a thinking level.
func levelLabel(level string) string {
	if name, ok := levelNames[i18n.Current()][level]; ok {
		return name
	}
	return level
}

// modelPrice renders the per-million cost, "free" when both halves are zero.
func modelPrice(entry protocol.ModelEntry, lang i18n.Lang) string {
	if entry.Cost.Input == 0 && entry.Cost.Output == 0 {
		return map[i18n.Lang]string{i18n.ZH: "免费", i18n.EN: "free"}[lang]
	}
	prefix := "per M"
	if lang == i18n.ZH {
		prefix = "每百万"
	}
	return prefix + " $" + trimFloat(entry.Cost.Input) + " / $" + trimFloat(entry.Cost.Output)
}

// modelContext is the window as ModelPicker.tsx prints it: M for millions, k
// for thousands.
func modelContext(window int) string {
	switch {
	case window >= 1_000_000:
		return itoa(window/1_048_576) + "M"
	case window >= 1_000:
		return itoa(window/1_000) + "k"
	}
	return itoa(window)
}

// trimFloat prints a cost without trailing zeros.
func trimFloat(value float64) string {
	text := strings.TrimRight(strings.TrimRight(strconv.FormatFloat(value, 'f', 2, 64), "0"), ".")
	if text == "" {
		return "0"
	}
	return text
}
