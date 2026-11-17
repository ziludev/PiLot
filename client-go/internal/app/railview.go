package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// surfaceKind is the transport a chat came in on. The bridge leaves Kind out of
// surfaces it does not know, so the id prefix decides, as the rail's
// kindOf does.
func surfaceKind(s protocol.Surface) string {
	switch {
	case s.Kind != "":
		return s.Kind
	case strings.HasPrefix(s.SurfaceID, "oc_"):
		return "feishu"
	case strings.HasPrefix(s.SurfaceID, "client:"):
		return "client"
	}
	return "other"
}

// surfaceLabel names a surface the bridge has no title for: a Feishu chat is
// the head of its id, after the "oc_" that marks one.
func surfaceLabel(surfaceID string) string {
	if !strings.HasPrefix(surfaceID, "oc_") {
		return surfaceID
	}
	head := strings.TrimPrefix(surfaceID, "oc_")
	if len(head) > 6 {
		head = head[:6]
	}
	return i18n.T("feishu") + " " + head
}

// surfaceTitle is the chat's name: the one its owner gave it, else the name the
// client gave the chats it opened, else the surface it talks on.
func surfaceTitle(s protocol.Surface) string {
	if s.Title != "" {
		return s.Title
	}
	if surfaceKind(s) == "client" {
		return i18n.T("newChat") + " #" + itoa(s.ConversationID)
	}
	if label := surfaceLabel(s.SurfaceID); label != "" {
		return label
	}
	return "#" + itoa(s.ConversationID)
}

// surfaceMeta is the row's second line: the chat's number and what it cost.
func surfaceMeta(s protocol.Surface) string {
	return "#" + itoa(s.ConversationID) + " · " + railCost(s.Cost)
}

// railCost is the row's spend: cents under a dollar, tenths above it, as the
// rail prints it.
func railCost(cost float64) string {
	if cost < 1 {
		return "$" + money(cost)
	}
	return fmt.Sprintf("$%.1f", cost)
}

// railFilters are the views of the list: by where a chat came from, then by
// what it is doing. prefs.SurfaceFilter keeps one of these values.
var railFilters = []string{"all", "client", "feishu", "running", "needs", "unread"}

// railFilterKeys name each view in the dictionary, in the same order as
// railFilters, so a label cannot end up over another view's count.
var railFilterKeys = []string{"allChats", "clientChats", "feishuChats", "running", "needsYou", "unread"}

// railFilterIndex is where the saved filter sits in railFilters. An unknown
// value reads as "all", so a hand-edited prefs file cannot empty the rail.
func railFilterIndex(value string) int {
	for i, filter := range railFilters {
		if filter == value {
			return i
		}
	}
	return 0
}

// railProjectKey is the project a chat belongs to. The bridge leaves a chat's
// project out when it belongs to the builtin workspace, which it names
// "workspace" everywhere else.
func railProjectKey(projectID string) string {
	if projectID == "" {
		return "workspace"
	}
	return projectID
}

// railProjectName is the project's name, or its id when the bridge no longer
// knows the project.
func (a *App) railProjectName(projectID string) string {
	for _, project := range a.projects {
		if project.ID == projectID {
			return project.Name
		}
	}
	return projectID
}

type railRowKind uint8

const (
	railChatRow railRowKind = iota
	railOtherProjects
)

// railRow is one line of the list: a chat, or the header that folds the chats
// of the projects this rail is not scoped to away.
type railRow struct {
	kind    railRowKind
	surface protocol.Surface
	topic   bool   // a fork, drawn under the chat it forked from
	other   bool   // outside the project the rail is scoped to
	project string // that project's name, on rows that are outside it
	count   int    // chats the folded header stands for
}

// railRows is the rail's list: the chats of the project it is scoped to, then
// the header folding the other projects' chats away with them under it.
func (a *App) railRows() []railRow {
	scoped := make([]protocol.Surface, 0, len(a.surfaces))
	others := make([]protocol.Surface, 0, len(a.surfaces))
	for _, s := range a.sortedSurfaces() {
		if !a.railShown(s, railFilterIndex(a.prefs.SurfaceFilter)) {
			continue
		}
		if railProjectKey(s.ProjectID) == railProjectKey(a.project) {
			scoped = append(scoped, s)
			continue
		}
		others = append(others, s)
	}
	rows := a.railChatRows(scoped, false)
	if len(others) == 0 {
		return rows
	}
	rows = append(rows, railRow{kind: railOtherProjects, count: len(others)})
	if a.othersOpen {
		rows = append(rows, a.railChatRows(others, true)...)
	}
	return rows
}

// railFilterCounted reports whether a view's label carries its count: the total
// and the three state views, whose numbers move as turns run, rather than the
// two kind views, which are fixed slices of the total.
func railFilterCounted(filter string) bool {
	switch filter {
	case "client", "feishu":
		return false
	}
	return true
}

// railShown reports whether the list's filter shows the chat: the kind views by
// the surface the chat came from, the status views by the work it still has.
func (a *App) railShown(s protocol.Surface, filter int) bool {
	if filter <= 0 || filter >= len(railFilters) {
		return true
	}
	switch railFilters[filter] {
	case "running":
		return a.isRunning(s.ConversationID)
	case "needs":
		return a.chatNeedsYou(s.ConversationID)
	case "unread":
		return a.isUnread(s.ConversationID)
	}
	return surfaceKind(s) == railFilters[filter]
}

// railFilterCounts is how many chats each view holds, which its label says. It
// counts through the predicate that filters, so a label cannot promise a view
// the list will not draw.
func (a *App) railFilterCounts() []int {
	counts := make([]int, len(railFilters))
	for _, s := range a.sortedSurfaces() {
		for i := range railFilters {
			if a.railShown(s, i) {
				counts[i]++
			}
		}
	}
	return counts
}

// railChatRows lists chats depth-first: a fork follows the chat it forked from,
// as the rail nests it, so a busy chat cannot push the others off the
// rail. A fork whose parent is not listed stays folded away with it.
func (a *App) railChatRows(list []protocol.Surface, other bool) []railRow {
	listed := make(map[int]bool, len(list))
	for _, s := range list {
		listed[s.ConversationID] = true
	}
	children := make(map[int][]railRow, len(list))
	rows := make([]railRow, 0, len(list))
	for _, s := range list {
		row := railRow{kind: railChatRow, surface: s, other: other}
		if other {
			row.project = a.railProjectName(s.ProjectID)
		}
		if s.ThreadParent != 0 && listed[s.ThreadParent] {
			row.topic = true
			children[s.ThreadParent] = append(children[s.ThreadParent], row)
			continue
		}
		rows = append(rows, row)
	}
	nested := make([]railRow, 0, len(rows))
	for _, row := range rows {
		nested = append(nested, row)
		nested = append(nested, children[row.surface.ConversationID]...)
	}
	return nested
}

// railRowMeta is a row's second line: the id and the spend, and the project on
// rows drawn under the folded header.
func railRowMeta(row railRow) string {
	if row.project == "" {
		return surfaceMeta(row.surface)
	}
	return surfaceMeta(row.surface) + " · " + row.project
}

// railTaskLabel names the task kinds the owner starts. A kind without an entry
// shows its own id, which is still a better name than nothing.
func railTaskLabel(kind string) string {
	zh := i18n.Current() == i18n.ZH
	switch kind {
	case "app.feature":
		if zh {
			return "需求流水线"
		}
		return "Feature pipeline"
	case "app.reminder":
		if zh {
			return "定时提醒"
		}
		return "Reminder"
	}
	return kind
}

// railTaskState words the task's state, as the rail's STATE_LABELS does.
func railTaskState(state string) string {
	zh := i18n.Current() == i18n.ZH
	labels := map[string][2]string{
		"pending":    {"queued", "排队中"},
		"running":    {"running", "运行中"},
		"waiting":    {"waiting", "等待中"},
		"sleeping":   {"sleeping", "休眠"},
		"completing": {"finishing", "收尾"},
	}
	label, ok := labels[state]
	if !ok {
		return state
	}
	if zh {
		return label[1]
	}
	return label[0]
}

// railTaskTitle is the task's name, or the name of its kind when the owner gave
// it none.
func railTaskTitle(task protocol.Task) string {
	if task.Title != "" {
		return task.Title
	}
	return railTaskLabel(task.Kind)
}

// railTaskColor is the task's dot: the accent while it works, red when it needs
// the owner, grey while it waits or sleeps.
func railTaskColor(t theme.Tokens, state string) ui.Color {
	switch state {
	case "running", "completing":
		return t.Primary
	case "blocked", "faulted":
		return t.Destructive
	}
	return t.MutedFg
}

// railLiveTasks are the tasks the owner started. The bridge's own tasks, the
// sampler among them, are not work anyone asked for and would crowd the rail.
func railLiveTasks(tasks []protocol.Task) []protocol.Task {
	live := make([]protocol.Task, 0, len(tasks))
	for _, task := range tasks {
		if !strings.HasPrefix(task.Kind, "app.") || task.Kind == "app.usage-sampler" {
			continue
		}
		live = append(live, task)
	}
	return live
}

// railMark is the one slot a row keeps for what its chat is doing, since a row
// is a single line: what the reader must act on, what is being worked on, then
// what arrived unseen.
type railMark uint8

const (
	railNoMark railMark = iota
	railNeedsYouMark
	railRunningMark
	railUnreadMark
	railSleepingMark
)

// railMarkFor is the mark the chat's row shows. A task that waits for its owner
// outranks one that runs, which outranks an answer nobody has read. A task asleep
// until a time of its own comes last: it is the quietest thing a row can say, and
// the three above it all ask for a look sooner.
func (a *App) railMarkFor(conversationID int, selected bool) railMark {
	switch {
	case a.chatNeedsYou(conversationID):
		return railNeedsYouMark
	case a.isRunning(conversationID):
		return railRunningMark
	case !selected && a.isUnread(conversationID):
		return railUnreadMark
	case a.chatSleeping(conversationID):
		return railSleepingMark
	}
	return railNoMark
}

// drawRailMark draws that mark: a red dot for a task that waits, a ring for work in
// flight (its outline keeps it apart from the unread dot's fill), and a dot for
// an answer that arrived while nobody was looking.
func drawRailMark(c *ui.Context, t theme.Tokens, mark railMark, rise func(ui.Element) ui.Element) {
	switch mark {
	case railNeedsYouMark:
		rise(ui.Box(c).Width(6).Height(6).Radius(3).AlignSelf(ui.Center).Background(t.Destructive))
	case railRunningMark:
		rise(ui.Box(c).Width(8).Height(8).Radius(4).AlignSelf(ui.Center).
			BorderWidth(1.5).BorderColor(t.Primary))
	case railUnreadMark:
		rise(ui.Box(c).Width(5).Height(5).Radius(3).AlignSelf(ui.Center).Background(t.Primary))
	case railSleepingMark:
		// A clock rather than a dot: the chat is not doing anything now, and it
		// will, which a coloured dot cannot say.
		rise(ui.Icon(c, icons.Clock).Size(11, 11).TextColor(t.MutedFg).AlignSelf(ui.Center))
	}
}
