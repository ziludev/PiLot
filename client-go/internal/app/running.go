package app

// RunningChat is one chat the agent is working in, as the tray lists it: the
// window is a poor place to look while it is behind another app.
type RunningChat struct {
	ID    int
	Title string
	State string
}

// runningHook republishes the running chats to whoever owns the menu bar.
var runningHook func([]RunningChat)

// SetRunningHook lets main.go put the running chats in the tray's menu.
func SetRunningHook(fn func([]RunningChat)) { runningHook = fn }

// runningChats is the busy half of the task list, one entry per chat, titled
// the way the rail titles them.
func (a *App) runningChats() []RunningChat {
	var out []RunningChat
	seen := map[int]bool{}
	for _, task := range a.tasks {
		if task.ConversationID == 0 || seen[task.ConversationID] {
			continue
		}
		if task.State != "running" && task.State != "queued" && task.State != "pending" {
			continue
		}
		seen[task.ConversationID] = true
		title := task.Title
		if title == "" {
			title = a.surfaceTitleByID(task.ConversationID)
		}
		out = append(out, RunningChat{ID: task.ConversationID, Title: title, State: task.State})
	}
	return out
}

// publishRunning hands the current set to the tray.
func (a *App) publishRunning() {
	if runningHook == nil || a.secondary {
		return
	}
	running := a.runningChats()
	go runningHook(running)
}

// MenuOpenChat brings a chat to the front, which is what the tray's items do.
func (a *App) MenuOpenChat(conversationID int) {
	a.apply(func() { a.selectConversation(conversationID) })
}
