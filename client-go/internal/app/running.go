package app

// TrayState is what the menu bar shows: the chats the agent is working in and
// the calls waiting for a decision. A window that is behind another app still
// needs to say both.
type TrayState struct {
	Running   []RunningChat
	Approvals []TrayApproval
}

// RunningChat is one chat the agent is working in.
type RunningChat struct {
	ID    int
	Title string
	State string
}

// TrayApproval is one pending call, named the way the approval bar names it.
type TrayApproval struct {
	CallID         string
	ConversationID int
	Tool           string
	Reason         string
}

// trayHook republishes the state to whoever owns the menu bar.
var trayHook func(TrayState)

// SetTrayHook lets main.go put the running chats and pending approvals in the
// tray's menu.
func SetTrayHook(fn func(TrayState)) { trayHook = fn }

// The state words are the scheduler's own, from its inspect() projection
// (dist/harness/scheduler.js): running, completing, waiting, pending, blocked
// and sleeping. A terminal task leaves the list instead of lingering as "done".

// taskInFlight is a task the model works on now, or one that runs as soon as its
// own children finish. Waiting counts: a chat whose generation waits on a tool is
// still working, and hiding it is what made the rail look idle mid-turn.
func taskInFlight(state string) bool {
	switch state {
	case "running", "completing", "waiting", "pending":
		return true
	}
	return false
}

// taskNeedsYou is a task the scheduler could not place, which only its owner can
// move: a blocked fit, a failed migration, an approval the turn stops on.
func taskNeedsYou(state string) bool {
	switch state {
	case "blocked", "faulted":
		return true
	}
	return false
}

// taskSleeping is a task that will run at a time of its own: a reminder, a digest,
// a scheduled follow-up. Nothing is being worked on, and nothing is waiting for
// the reader, so it is neither in flight nor unfinished.
func taskSleeping(state string) bool {
	return state == "sleeping"
}

// taskUnfinished is a task that has not finished: the model is working on it,
// or it stopped for its owner. Either way the chat is not a finished one.
func taskUnfinished(state string) bool {
	return taskInFlight(state) || taskNeedsYou(state)
}

// isRunning reports whether the chat has work that has not finished, which the
// rail draws as a mark on its row and lists under its running view. A task that
// waits for an approval is still in progress from the reader's side, so it
// belongs there rather than among the finished chats.
func (a *App) isRunning(conversationID int) bool {
	for _, task := range a.tasks {
		if task.ConversationID != conversationID {
			continue
		}
		if taskUnfinished(task.State) {
			return true
		}
	}
	return false
}

// chatSleeping reports whether the chat has work parked until a time of its own.
func (a *App) chatSleeping(conversationID int) bool {
	for _, task := range a.tasks {
		if task.ConversationID != conversationID {
			continue
		}
		if taskSleeping(task.State) {
			return true
		}
	}
	return false
}

// chatNeedsYou reports whether one of the chat's tasks waits for its owner.
func (a *App) chatNeedsYou(conversationID int) bool {
	for _, task := range a.tasks {
		if task.ConversationID == conversationID && taskNeedsYou(task.State) {
			return true
		}
	}
	return false
}

// runningChats is the busy half of the task list, one entry per chat, titled
// the way the rail titles them.
func (a *App) runningChats() []RunningChat {
	var out []RunningChat
	seen := map[int]bool{}
	for _, task := range a.tasks {
		if task.ConversationID == 0 || seen[task.ConversationID] {
			continue
		}
		if !taskInFlight(task.State) {
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

// trayApprovals names each pending call with its surface's chat, so the menu's
// row can take the reader to the bar that decides it.
func (a *App) trayApprovals() []TrayApproval {
	if len(a.approvals) == 0 {
		return nil
	}
	out := make([]TrayApproval, 0, len(a.approvals))
	for _, approval := range a.approvals {
		out = append(out, TrayApproval{
			CallID:         approval.CallID,
			ConversationID: a.conversationOfSurface(approval.SurfaceID),
			Tool:           approval.Tool,
			Reason:         approval.Reason,
		})
	}
	// The server's order is kept: it is the order the approval bar shows.
	return out
}

// conversationOfSurface maps a surface id back to the chat that owns it, which
// is what selecting it needs.
func (a *App) conversationOfSurface(surfaceID string) int {
	for _, s := range a.surfaces {
		if s.SurfaceID == surfaceID {
			return s.ConversationID
		}
	}
	return 0
}

// publishTray hands the current state to the menu bar.
func (a *App) publishTray() {
	if trayHook == nil || a.secondary {
		return
	}
	state := TrayState{Running: a.runningChats(), Approvals: a.trayApprovals()}
	go trayHook(state)
}

// MenuOpenChat brings a chat to the front, which is what the tray's items do.
func (a *App) MenuOpenChat(conversationID int) {
	a.apply(func() { a.selectConversation(conversationID) })
}
