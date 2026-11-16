package app

// stepRail moves the selection to the next or previous chat in the rail and
// opens it, the way Slack and Linear switch with the keyboard. It walks the
// list the rail draws, so the order matches what the eye sees.
//
// The keys are ⌥↑/⌥↓ rather than Linear's bare j/k: a bare letter cannot be a
// window shortcut here without eating every j typed into the composer.
func (a *App) stepRail(delta int) {
	a.apply(func() {
		// The chat rows the rail draws, in drawing order, skipping the
		// "other projects" header the walk must not land on.
		chats := make([]railRow, 0)
		for _, row := range a.railRows() {
			if row.kind == railChatRow {
				chats = append(chats, row)
			}
		}
		if len(chats) == 0 {
			return
		}
		index := -1
		for i, row := range chats {
			if row.surface.ConversationID == a.current {
				index = i
				break
			}
		}
		next := index + delta
		if index < 0 {
			next = 0
		}
		if next < 0 {
			next = 0
		}
		if next >= len(chats) {
			next = len(chats) - 1
		}
		target := chats[next].surface.ConversationID
		a.railCursor = target
		if target != a.current {
			a.selectConversation(target)
		}
		a.invalidate()
	})
}

// togglePinSelected pins or unpins the chat the rail is on.
func (a *App) togglePinSelected() {
	a.apply(func() { a.togglePin(a.current) })
}

// archiveSelected archives the open chat, the keyboard twin of the row menu.
func (a *App) archiveSelected() {
	a.apply(func() {
		a.menu.id = a.current
		a.archiveChat(true)
	})
}
