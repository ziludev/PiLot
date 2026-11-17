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
	"pitta/internal/motion"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// modelKey is the identity the favourites and recents lists store, the same
// `provider/id` shape the earlier client's view keeps in localStorage.
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
// arrow keys behave in the earlier client's view's palette.
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
	// the earlier client's view's trigger: an M badge, the model, its thinking level and
	// a chevron, all 11 px and quiet until the pointer arrives.
	trigger := ui.ButtonBase(c).Gap(6).Padding(2, 4).Radius(6).Cursor(ui.CursorPointer)
	trigger.Label(i18n.T("model"))
	trigger.Tooltip(i18n.T("model"))
	if trigger.Hovered() {
		trigger.Background(t.Accent)
	}
	trigger.Children(func() {
		ui.Row(c).Size(14, 14).Radius(3).Background(t.Muted).AlignItems(ui.Center).
			Justify(ui.Center).Children(func() {
			ui.Text(c, "M").FontSize(9).TextColor(t.MutedFg)
		})
		ui.Text(c, a.modelLabel()).FontSize(11).TextColor(t.MutedFg).MaxLines(1)
		a.levelBars(c, t, a.modelLevels(), a.thinkingLevel())
		ui.Text(c, levelLabel(a.thinkingLevel())).FontSize(11).TextColor(t.MutedFg)
		ui.Icon(c, icons.KeyRound).Size(12, 12).TextColor(t.MutedFg)
		ui.Icon(c, icons.ChevronDown).Size(12, 12).TextColor(t.MutedFg)
	})
	trigger.OnClick(func() {
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

// modelPalette is the command palette of the earlier client's view: a search field, the
// models ordered favourites-first, and the thinking levels of the chosen one.
func (a *App) modelPalette(c *ui.Context) {
	t := theme.Current(c)
	// The palette comes up from the composer over 280 ms, as a popover would
	// over its anchor; the rows inside a List cannot carry transitions.
	ui.Column(c).FillWidth().Padding(10).Gap(6).Background(t.Popover).
		BorderWidth(1, 0, 0, 0).BorderColor(t.Border).Key("model-palette").
		Transition(motion.PopoverEnter()).Children(func() {
		ui.TextInput(c, &a.modelQuery).FillWidth().FontSize(11).
			Placeholder(i18n.T("searchModel")).Label(i18n.T("searchModel")).
			OnTextKey(func(k ui.TextKey) bool { return a.modelKey(c, k) }).
			OnSubmit(func() { a.pickModel() })
		sections := a.modelSections()
		rows := paletteRows(sections)
		ui.Column(c).FillWidth().Height(paletteHeight(len(rows))).Children(func() {
			ui.List(c, &a.modelList, len(rows), func(i int) {
				row := rows[i]
				if !row.isEntry {
					a.modelSectionHeader(c, t, row.section)
					return
				}
				a.modelRowAt(c, t, row.entry, row.model == a.modelCursor)
			}).Fill()
		})
		a.thinkingRow(c, t)
		a.modelFooter(c, t)
	})
}

// visibleModels is the list the picker draws, flattened for the arrow keys.
func (a *App) visibleModels() []protocol.ModelEntry {
	var all []protocol.ModelEntry
	for _, section := range a.modelSections() {
		if section.Collapsed {
			continue
		}
		all = append(all, section.Entries...)
	}
	return all
}

// paletteHeight keeps the list as tall as its rows need, up to a cap, instead
// of leaving a window-sized hole under two providers.
func paletteHeight(rows int) float32 {
	height := float32(rows) * 26
	if height > 240 {
		return 240
	}
	if height < 60 {
		return 60
	}
	return height
}

// modelSection is one heading of the palette: the recents at the top or a
// provider's group, as the earlier client's view groups them.
type modelSection struct {
	Title     string
	Provider  string
	Entries   []protocol.ModelEntry
	Collapsed bool
}

// paletteRow is one line of the palette: a heading or a model, so ui.List can
// draw both with stable keys.
type paletteRow struct {
	section *modelSection
	entry   protocol.ModelEntry
	isEntry bool
	model   int
}

// paletteRows flattens the sections, numbering the models so the arrow keys and
// the highlight agree.
func paletteRows(sections []modelSection) []paletteRow {
	rows := make([]paletteRow, 0, 16)
	index := 0
	for i := range sections {
		section := &sections[i]
		rows = append(rows, paletteRow{section: section})
		if section.Collapsed {
			continue
		}
		for _, entry := range section.Entries {
			rows = append(rows, paletteRow{entry: entry, isEntry: true, model: index})
			index++
		}
	}
	return rows
}

// modelSections is the palette in drawing order: the recents first while no
// query narrows it, then one group per provider with the chat's own on top.
func (a *App) modelSections() []modelSection {
	matched := a.matchedModels()
	byKey := make(map[string]protocol.ModelEntry, len(a.models))
	for _, entry := range a.models {
		byKey[modelKey(protocol.ModelRef{Provider: entry.Provider, ModelID: entry.ID})] = entry
	}
	var sections []modelSection
	if strings.TrimSpace(a.modelQuery) == "" && len(a.modelsRecent) > 0 {
		recents := make([]protocol.ModelEntry, 0, len(a.modelsRecent))
		for _, key := range a.modelsRecent {
			if entry, ok := byKey[key]; ok && containsModel(matched, entry) {
				recents = append(recents, entry)
			}
		}
		if len(recents) > 0 {
			sections = append(sections, modelSection{Title: i18n.T("recent"), Entries: recents})
		}
	}
	current := ""
	if a.model != nil {
		current = a.model.Provider
	}
	providers := make([]string, 0, 8)
	grouped := map[string][]protocol.ModelEntry{}
	for _, entry := range matched {
		if _, seen := grouped[entry.Provider]; !seen {
			providers = append(providers, entry.Provider)
		}
		grouped[entry.Provider] = append(grouped[entry.Provider], entry)
	}
	sort.SliceStable(providers, func(i, j int) bool {
		if providers[i] == current {
			return true
		}
		if providers[j] == current {
			return false
		}
		return providers[i] < providers[j]
	})
	for _, provider := range providers {
		entries := grouped[provider]
		// Favourites float to the top inside their group.
		sort.SliceStable(entries, func(i, j int) bool {
			return a.favoriteRank(entries[i]) < a.favoriteRank(entries[j])
		})
		// Only the chat's own provider opens by default; with no model yet every
		// group stays open, so the palette never opens empty. A group the reader
		// folded or unfolded keeps that choice.
		collapsed := current != "" && provider != current
		if folded, ok := a.modelCollapsed[provider]; ok {
			collapsed = folded
		}
		sections = append(sections, modelSection{
			Title: provider, Provider: provider, Entries: entries, Collapsed: collapsed,
		})
	}
	return sections
}

// modelSectionHeader draws one heading: its fold arrow, the provider's key, its
// name and how many models it holds.
func (a *App) modelSectionHeader(c *ui.Context, t theme.Tokens, section *modelSection) {
	header := ui.Row(c).FillWidth().Height(18).Gap(5).Padding(0, 4).AlignItems(ui.Center)
	header.Children(func() {
		if section.Provider == "" {
			ui.Text(c, strings.ToUpper(section.Title)).FontSize(9).TextColor(t.MutedFg).SingleLine()
			return
		}
		if section.Collapsed {
			ui.Icon(c, icons.ChevronRight).Size(11, 11).TextColor(t.MutedFg)
		} else {
			ui.Icon(c, icons.ChevronDown).Size(11, 11).TextColor(t.MutedFg)
		}
		ui.Icon(c, icons.KeyRound).Size(11, 11).TextColor(t.MutedFg)
		ui.Text(c, section.Title).FontSize(11).TextColor(t.Foreground).SingleLine()
		ui.Text(c, strconv.Itoa(len(section.Entries))).FontSize(9).TextColor(t.MutedFg).SingleLine().Width(20)
	})
	if section.Provider == "" {
		return
	}
	provider, folded := section.Provider, section.Collapsed
	header.OnClick(func() {
		a.apply(func() {
			if a.modelCollapsed == nil {
				a.modelCollapsed = map[string]bool{}
			}
			a.modelCollapsed[provider] = !folded
			a.invalidate()
		})
	})
}

// modelFooter is the palette's bottom bar: the effort in use, then the two
// actions the earlier client's view offers, setting the default and refreshing the list.
func (a *App) modelFooter(c *ui.Context, t theme.Tokens) {
	ui.Divider(c).Background(t.Border)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(c, i18n.T("effort")+" · "+a.modelLabel()).FontSize(10).TextColor(t.MutedFg).SingleLine()
		ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, icons.Sparkles).Size(10, 10).TextColor(t.Warning)
			ui.Text(c, levelLabel(a.thinkingLevel())).FontSize(10).TextColor(t.Warning).SingleLine()
		})
	})
	ui.Row(c).FillWidth().Gap(6).Children(func() {
		ui.Button(c, i18n.T("setDefault")).FontSize(10).Padding(3, 8).
			OnClick(func() { a.setDefaultModel() })
		ui.Button(c, "").FontSize(10).Padding(3, 7).Label(i18n.T("refresh")).Children(func() {
			ui.Icon(c, icons.RefreshCw).Size(10, 10).TextColor(t.MutedFg)
		}).OnClick(func() {
			a.goLoad(func() error {
				if err := a.loadModels(context.Background()); err != nil {
					return err
				}
				a.apply(func() { a.notice = i18n.T("refreshed") })
				return nil
			})
		})
	})
}

// setDefaultModel makes the current choice the default for new chats.
func (a *App) setDefaultModel() {
	if a.model == nil {
		return
	}
	choice := api.ModelChoice{
		Provider: a.model.Provider, ModelID: a.model.ModelID, ThinkingLevel: a.thinkingLevel(),
	}
	a.goLoad(func() error {
		if err := a.api.SetDefaultModel(context.Background(), choice); err != nil {
			return err
		}
		a.apply(func() { a.notice = i18n.T("defaultSet") })
		return nil
	})
}

// matchedModels is every model the query lets through, by name.
func (a *App) matchedModels() []protocol.ModelEntry {
	query := strings.ToLower(strings.TrimSpace(a.modelQuery))
	matched := make([]protocol.ModelEntry, 0, len(a.models))
	for _, entry := range a.models {
		if query != "" {
			haystack := strings.ToLower(entry.Name + " " + entry.Provider + " " + entry.ID)
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		matched = append(matched, entry)
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Name < matched[j].Name })
	return matched
}

// containsModel reports whether the slice holds this provider and id.
func containsModel(entries []protocol.ModelEntry, want protocol.ModelEntry) bool {
	for _, entry := range entries {
		if entry.Provider == want.Provider && entry.ID == want.ID {
			return true
		}
	}
	return false
}

// favoriteRank orders a group's rows: favourites first, then by name.
func (a *App) favoriteRank(entry protocol.ModelEntry) int {
	key := modelKey(protocol.ModelRef{Provider: entry.Provider, ModelID: entry.ID})
	for _, fav := range a.modelsFav {
		if fav == key {
			return 0
		}
	}
	return 1
}

// levelBars is the four-step meter the earlier client's view shows beside a model: the
// bars up to the current level light up.
func (a *App) levelBars(c *ui.Context, t theme.Tokens, levels []string, value string) {
	index := 0
	for i, level := range levels {
		if level == value {
			index = i
			break
		}
	}
	ui.Row(c).Gap(2).Height(10).AlignItems(ui.End).Children(func() {
		for bar := 0; bar < 4; bar++ {
			height := float32(4 + bar*2)
			filled := index >= bar && index > 0
			color := t.MutedFg.Alpha(0.4)
			if filled {
				color = ui.RGB(52, 211, 153)
			}
			ui.Row(c).Width(2).Height(height).Radius(1).Background(color)
		}
	})
}

// modelInitial is the lettered plate in front of a model's name. A Column
// left-aligns and bottom-aligns by default, which put the letter in the corner
// of the plate instead of the middle of it.
func modelInitial(c *ui.Context, t theme.Tokens, initial string) ui.Element {
	return ui.Column(c).Size(20, 20).Radius(4).Background(t.Muted).
		AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		ui.Text(c, initial).FontSize(10).TextColor(t.MutedFg)
	})
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
		// The initial sits in the middle of its plate: a Column left-aligns and
		// bottom-aligns by default, which put the letter in the corner.
		modelInitial(c, t, initial)
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

// levelNames is the earlier client's view's LEVEL_LABEL: the thinking ladder is a
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

// modelContext is the window as the earlier client's view prints it: M for millions, k
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
