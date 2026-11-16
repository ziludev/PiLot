package app

import (
	"strings"

	"pitta/internal/bridge"
	"pitta/internal/i18n"
)

// palette is the Cmd+K list's second half: the commands, which Raycast puts in
// the same field as the results. Typing ">" switches the list from chats to
// commands; anything else searches chats, so the common case costs no prefix.
const palettePrefix = ">"

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
		{Name: "Find in this chat", Run: func() { a.apply(func() { a.findOpen = true }) }},
		{Name: "Toggle changes panel", Run: func() { a.toggleSide() }},
		{Name: "Show changes", Run: func() { a.openSideTab(SideChanges) }},
		{Name: "Show memory", Run: func() { a.openSideTab(SideMemory) }},
		{Name: "Show access", Run: func() { a.openSideTab(SideAccess) }},
		{Name: "Show usage", Run: func() { a.openSideTab(SideUsage) }},
		{Name: "Export chat as Markdown", Run: func() { a.exportMarkdown() }},
		{Name: "Pin or unpin this chat", Run: func() { a.togglePinSelected() }},
		{Name: "Archive this chat", Run: func() { a.archiveSelected() }},
		{Name: "Switch theme", Run: func() { a.toggleTheme() }},
		{Name: "Switch language", Run: func() { a.toggleLang() }},
		{Name: "Open in a new window", Run: func() { a.openInNewWindow(a.currentValue()) }},
		{Name: "Settings", Run: func() { a.apply(func() { a.settingsOpen = true }) }},
		{Name: "Open logs", Run: func() { bridge.OpenLogs() }},
		{Name: "Quit", Run: func() { a.askQuit() }},
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
