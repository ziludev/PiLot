package app

import "github.com/egoist/mygo/ui"

// quickKeys are the chats behind ⌘1 … ⌘9: the first nine the rail shows, in
// the rail's own order, so the number a chat has on screen is the number that
// switches to it.
var quickKeys = []ui.Key{
	ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5,
	ui.Key6, ui.Key7, ui.Key8, ui.Key9,
}

// chatForQuickKey is the chat a number stands for, or 0 when the rail has
// fewer chats than that.
func (a *App) chatForQuickKey(index int) int {
	rows := a.railRows()
	seen := 0
	for _, row := range rows {
		if row.kind != railChatRow {
			continue
		}
		if seen == index {
			return row.surface.ConversationID
		}
		seen++
	}
	return 0
}
