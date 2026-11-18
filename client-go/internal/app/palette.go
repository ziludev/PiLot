package app

import (
	"strings"

	"pitta/internal/i18n"
)

// palette is the Cmd+K list's second half: the commands, which Raycast puts in
// the same field as the results. Typing ">" switches the list from chats to
// commands; anything else searches chats, so the common case costs no prefix.
const palettePrefix = ">"

// The lines the dictionary has no key for. They read as commands rather than
// UI labels, so a constant with its reason is what the i18n rule allows; the
// ones that do have a key (newChat, changes, memory, access, usage, pinned,
// archive, theme, language) use it, so a Chinese window stays Chinese.
const (
	paletteFind      = "Find in this chat…"
	paletteToggle    = "Toggle the side panel"
	paletteExport    = "Export this chat as Markdown"
	paletteSettings  = "Settings…"
	paletteLogs      = "Open the agent's logs"
	paletteQuit      = "Quit PiLot"
	paletteNewWindow = "Open in a new window"
	paletteHint      = "Run a command…  (⌫ to search chats)"
)

// paletteAction is one command: what it reads as, and what it does.
type paletteAction struct {
	Name string
	Run  func()
}

// paletteActions is what the palette offers. The order is by how often the
// action is worth reaching for: everyday view switches first, quitting last.
func (a *App) paletteActions() []paletteAction {
	return []paletteAction{
		{Name: i18n.T("newChat"), Run: func() { a.newConversation() }},
		{Name: paletteFind, Run: func() { a.apply(func() { a.findOpen = true }) }},
		{Name: paletteToggle, Run: func() { a.toggleSide() }},
		{Name: i18n.T("changes"), Run: func() { a.openSideTab(SideChanges) }},
		{Name: i18n.T("memory"), Run: func() { a.openSideTab(SideMemory) }},
		{Name: i18n.T("access"), Run: func() { a.openSideTab(SideAccess) }},
		{Name: i18n.T("usage"), Run: func() { a.openSideTab(SideUsage) }},
		{Name: paletteExport, Run: func() { a.exportMarkdown() }},
		{Name: i18n.T("pinned"), Run: func() { a.togglePinSelected() }},
		{Name: i18n.T("archive"), Run: func() { a.archiveSelected() }},
		{Name: i18n.T("theme"), Run: func() { a.toggleTheme() }},
		{Name: i18n.T("language"), Run: func() { a.toggleLang() }},
		{Name: paletteNewWindow, Run: func() { a.openInNewWindow(a.currentValue()) }},
		{Name: paletteSettings, Run: func() { a.apply(func() { a.settingsOpen = true }) }},
		{Name: paletteLogs, Run: func() { a.apply(func() { a.toggleLogs(); a.invalidate() }) }},
		{Name: paletteQuit, Run: func() { a.askQuit() }},
	}
}

// paletteMode reports whether the query asks for commands.
func paletteMode(query string) bool {
	return strings.HasPrefix(strings.TrimSpace(query), palettePrefix)
}

// paletteMatches narrows the commands by what follows the prefix.
func (a *App) paletteMatches(query string) []paletteAction {
	needle := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), palettePrefix)))
	all := a.paletteActions()
	if needle == "" {
		return all
	}
	matched := make([]paletteAction, 0, len(all))
	for _, action := range all {
		if strings.Contains(strings.ToLower(action.Name), needle) {
			matched = append(matched, action)
		}
	}
	return matched
}
