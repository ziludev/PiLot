// Package app is the window: it owns the conversation state, talks to the
// bridge, and draws the shell (rail, transcript, composer, side panel).
//
// Every field below the layout block is mutated only on the UI thread, inside
// apply: network calls run in goroutines and hand their result back through
// apply, so the views read the state without locks.
package app

import (
	"context"
	"fmt"
	"log"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"pitta/internal/api"
	"pitta/internal/brand"
	"pitta/internal/diff"
	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/motion"
	"pitta/internal/protocol"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// Config is what the window needs to reach the agent.
type Config struct {
	BaseURL string
	Token   string
	Home    string
}

// SideTab is one panel of the right-hand column, as the old client's store's tab field names
// them.
type SideTab string

const (
	SideChanges SideTab = "changes"
	SideMemory  SideTab = "memory"
	SideAccess  SideTab = "access"
	SideUsage   SideTab = "usage"
)

// ViewKind is what the middle pane shows.
type ViewKind int

const (
	ViewTranscript ViewKind = iota
	ViewFile
)

// ReviewNote is one staged line comment the next message sends to the agent.
type ReviewNote struct {
	Path string `json:"path"`
	Line string `json:"line"`
	Note string `json:"note"`
}

// Conn is where the live socket stands.
type Conn int

const (
	ConnConnecting Conn = iota
	ConnOpen
	ConnDown
)

// AppError is one client-side failure, newest last: the bridge's log file is
// not visible in the window, so the window keeps its own.
type AppError struct {
	At      time.Time
	Message string
}

type App struct {
	cfg Config
	api *api.Client
	win *mygo.Window
	// secondary windows skip what only one window may own: the approval poll,
	// the Dock badge and the preference file.
	secondary bool
	// ctx ends the chat's watcher when the window closes, so a closed window
	// does not keep a websocket alive.
	ctx    context.Context
	cancel context.CancelFunc

	// currentID mirrors current for goroutines: with a window, currentChanges
	// only on the UI thread, so a background read of it would race.
	currentID atomic.Int64

	// Layout and widget state: UI thread only, never read from a goroutine.
	railWidth     float32
	railListState ui.ListState
	rows          []railRow
	prefs         prefs
	md            markdown.Options
	steer         bool
	// turnTotal is how many turns the chat holds: what a title or a summary
	// counts, not what a frame draws.
	turnTotal int
	// chatRows are the rows the last frame laid out, and chatBuilt the turns
	// they came from: a row points into them, so both live and die together.
	chatRows  []chatRow
	chatBuilt []turns.Turn
	// Grouped turns, kept until the entries or the waits change: grouping
	// parses every entry, and a frame that only scrolls parses nothing.
	turnCache []turns.Turn
	turnStale bool

	// The chat's history is longer than the first page: historyMore says the
	// bridge still holds entries older than the ones loaded, historyLoading
	// stops a second request while one is in flight.
	historyMore    bool
	historyLoading bool
	// jumpKey is what the find bar or a search hit last asked the list to land
	// on, so the jump happens once rather than every frame; jumpTarget is the
	// row it is at.
	jumpKey    string
	jumpTarget int

	// M2 layout: the side panel and the file view share the middle pane.
	sideOpen  bool
	sideTab   SideTab
	sideWidth float32
	view      ViewKind
	filePath  string
	file      *protocol.FilePayload
	fileErr   string

	// Which memory suggestions the reader unchecked before applying. The panel
	// treats an absent index as checked.
	memoryUnchecked map[int]bool

	// Cross-conversation search: the box in the rail keeps filtering titles as
	// the user types, and a submit asks the bridge to look inside transcripts.
	searchHits    []protocol.SearchHit
	searchPending bool
	searchSeq     int

	// In-chat search, review notes and the git/commit dialogs.
	filter           string
	focusEntry       int
	convQuery        string
	convMatches      []int
	matchIndex       int
	review           []ReviewNote
	reviewDraft      string
	reviewPick       *ReviewNote
	commitDiffs      map[string]string
	commitExpanded   string
	commitDialogOpen bool
	commitBusy       bool
	commitMsg        string
	gitBase          string
	mergeOpen        bool
	mainPane         float32
	// turnList is the chat's place among its rows: it keeps that place by the
	// row the reader is on, so turns loading above them move nothing.
	turnList ui.ListState
	// settingsScroll keeps the panel where the reader left it while a
	// preference is dragged.
	settingsScroll ui.ScrollState
	previewScale   float32
	// seenApprovals remembers which waits have already sounded the alert, and
	// waitingAlert remembers that one is still running.
	seenApprovals map[string]bool
	waitingAlert  bool
	// quitAsk holds the ⌘Q confirmation open while a turn is still running.
	quitAsk bool
	// othersOpen is the rail's "other projects" fold, restored from prefs.
	othersOpen bool
	// railCursor is the chat the rail's keyboard walk is on, which is the open
	// chat unless the walk moved without opening.
	railCursor int
	// history is what this window sent, for Up/Down recall; historyAt walks it
	// and historyDraft holds what was being typed before the walk started.
	history      []string
	historyAt    int
	historyDraft string
	// settingsOpen holds the ⌘, panel.
	settingsOpen bool
	// ownChat is the chat this window was opened for; 0 for the first window,
	// which loads whichever chat the reader picks.
	ownChat int
	// titleTick counts the title refreshes, which is what makes a long wait
	// blink.
	titleTick   int
	atBottom    bool
	newMessages int
	sideSynced  bool
	// sideRoom is the room the two panes shared when the split was last synced:
	// a different room means the window was resized, not the divider dragged.
	sideRoom       float32
	frameRecovered bool
	mergeBusy      bool
	merge          *protocol.MergeResult
	changesLoading bool
	pushNote       string

	menu          chatMenu
	findOpen      bool
	shortcutsOpen bool
	// logs is the agent's own log, drawn in the window rather than handed to
	// Finder: reading it here keeps the tail one keystroke away.
	logs logViewer

	// unread dots the chats whose answers arrived while the window was behind
	// another app, or while they were not the open one.
	unread map[int]bool

	// focused is false while the window is behind another one; nothing is
	// notified while the reader is looking at the answer.
	focused bool
	// notified is the answer that last reached the owner, per chat: coming back
	// to a chat must not announce the answer already read there.
	notified map[int]string
	// notifyAt is when each chat last buzzed. The quiet wait alone would still
	// let a turn that keeps pausing buzz at every pause.
	notifyAt map[int]time.Time
	// notifyTimer is the wait that ends in a notification: every frame pushes it
	// back, so only an answer that has fallen still is announced. notifyQuiet is
	// how long that is; a test shortens it.
	notifyTimer *time.Timer
	notifyQuiet time.Duration

	// errsOpen shows the panel behind the corner error badge.
	errsOpen bool

	// pendingImages are the pictures the next turn carries, and imagePath is
	// what the path dialog holds while it is open.
	pendingImages []imagePart
	imagePath     string
	imageOpen     bool
	previewImage  int
	previewSet    []imagePart

	// historyImages holds the pictures a stored entry carries, decoded once per
	// entry. View draws every frame; decoding a screenshot's PNG there costs
	// ~15ms a frame, which pins the main thread and freezes the window.
	imageCache map[int][]imagePart
	imageOrder []int

	// Project switcher state: the menu, the open dialog and the removal
	// confirmation.
	projectMenu     bool
	projectOpen     bool
	projectPath     string
	projectRemoving bool
	removeTarget    *protocol.Project

	// Memory curation and the model picker.
	memoryQuery  string
	suggestions  []protocol.MemorySuggestion
	curating     bool
	modelCursor  int
	modelsFav    []string
	modelsRecent []string
	modelQuery   string
	// modelCollapsed remembers which provider groups the palette folded.
	modelCollapsed map[string]bool
	// modelList draws the palette rows, which mix headings and models.
	modelList ui.ListState
	modelOpen bool
	stepOpen  map[int]bool
	// stepGroups is the runs of tool calls a reader opened; a run is folded until
	// it is in here, under the turn it belongs to.
	stepGroups map[string]bool
	// outputOpen is the tool answers a reader asked to see in full; every other
	// one is capped with the count of what is held back under it.
	outputOpen map[int]bool
	// filesOpen is the turns whose file list the reader opened. The summary line
	// says "2 files"; which two is the question that follows it.
	filesOpen    map[int]bool
	patchView    diff.View
	draft        string
	query        string
	notice       string
	showArchived bool

	// Data, written only inside apply.
	conn       Conn
	downReason string
	retry      int
	reachable  bool
	lastOpen   time.Time
	busy       bool

	surfaces []protocol.Surface
	current  int
	projects []protocol.Project
	project  string
	tasks    []protocol.Task

	entries   []protocol.Entry
	live      *protocol.LiveDoc
	mood      brand.Mood
	moodUntil time.Time
	wasLive   bool

	model    *protocol.ModelRef
	thinking string
	models   []protocol.ModelEntry

	approvals []protocol.PendingApproval
	waits     []protocol.ApprovalWait
	memory    protocol.Memory
	changes   *protocol.Changes
	git       *protocol.Git
	access    *protocol.AccessView
	audit     []protocol.AuditEvent
	usage     protocol.Usage
	usageDays []protocol.UsageDay
	feishu    *protocol.Feishu
	taskOpen  *protocol.TaskDetail

	errs []AppError

	// switcher is the Cmd+K jump list's own state.
	switcher switcher

	// badge counts what the Dock icon shows; main.go wires it to mygo.
	badge int
}

// New builds the app; the window is attached once mygo creates it.
func New(cfg Config) *App {
	p := loadPrefs()
	lang := i18n.EN
	if p.Lang == string(i18n.ZH) {
		lang = i18n.ZH
	}
	i18n.Set(lang)
	width := float32(260)
	if p.RailWidth > 120 && p.RailWidth < 600 {
		width = p.RailWidth
	}
	sideWidth := float32(300)
	if p.SideWidth > 220 && p.SideWidth < 700 {
		sideWidth = p.SideWidth
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Every window draws at the saved transparency, which the settings panel
	// then changes for all of them at once.
	windowOpacity = p.opacity()
	return &App{
		ctx:           ctx,
		cancel:        cancel,
		cfg:           cfg,
		api:           api.New(cfg.BaseURL, cfg.Token),
		railWidth:     width,
		sideWidth:     sideWidth,
		othersOpen:    !p.OthersCollapsed,
		sideTab:       SideChanges,
		prefs:         p,
		focused:       true,
		stepOpen:      map[int]bool{},
		stepGroups:    map[string]bool{},
		outputOpen:    map[int]bool{},
		filesOpen:     map[int]bool{},
		notified:      map[int]string{},
		notifyAt:      map[int]time.Time{},
		notifyQuiet:   notifySettle,
		previewImage:  -1,
		seenApprovals: map[string]bool{},
		previewScale:  1,
		commitDiffs:   map[string]string{},
		modelsFav:     p.Favorites,
		modelsRecent:  p.Recents,
		unread:        unreadFromPrefs(p),
		// A chat opens at its newest turn, as the earlier client's transcript does.
		atBottom: true,
		turnList: ui.ListState{FollowEnd: true},
	}
}

// NewSecondary builds a window that shares the first one's client: it keeps its
// own transcript and panels, while the approval poll, the Dock badge and the
// preference file stay with the first window, which owns them.
func NewSecondary(cfg Config, client *api.Client, conversationID int) *App {
	a := New(cfg)
	a.api = client
	a.secondary = true
	a.ownChat = conversationID
	a.current = conversationID
	a.currentID.Store(int64(conversationID))
	return a
}

// openInNewWindow asks main.go for a second window on this chat. The opener is
// nil in the headless tests, which is why the rail hides nothing when absent.
func (a *App) openInNewWindow(conversationID int) {
	if windowOpener == nil || a.secondary {
		return
	}
	windowOpener(a.cfg, a.api, conversationID)
}

// openChildChat opens a subagent's conversation beside the current one, and in
// place of it only when a second window is not available: the parent is where the
// reader goes back to, so it should not be the thing that gets replaced.
func (a *App) openChildChat(conversationID int) {
	if windowOpener != nil && !a.secondary {
		a.openInNewWindow(conversationID)
		return
	}
	a.selectConversation(conversationID)
}

// Attach takes the window and starts the first load.
func (a *App) Attach(win *mygo.Window) {
	a.win = win
	applyOpacity(win)
	// A finished answer should reach the owner when the window is behind
	// another one, as the earlier web client notifies only while hidden.
	a.focused = win.IsFocused()
	// the earlier client's rail panel defaults to 21% of the window and may be dragged
	// between 16% and 30%; without a stored width, start there.
	if a.prefs.RailWidth == 0 {
		if width, _ := win.ContentSize(); width > 0 {
			a.railWidth = railForWindow(float32(width))
		}
	}
	win.OnClosed(func() {
		// The window is gone: end the websocket instead of letting it reconnect
		// forever in the background, and stop hearing the other windows.
		// Where the window was is where it should come back: the last chat,
		// the divider widths, the filters.
		a.savePrefsNow()
		if a.ownChat != 0 {
			forgetOwnWindow(a.ownChat)
		}
		a.cancel()
		unsubscribeSurfaces(a)
	})
	subscribeSurfaces(a)
	// A window that shows one chat of its own says so, so the rail of the
	// first window can point at it instead of loading the chat again.
	if a.ownChat != 0 {
		chat := a.ownChat
		win := win
		noteOwnWindow(a, chat, func() {
			win.Show()
			win.Focus()
		})
	}
	a.restoreFrame()
	go a.tickTitle()
	win.OnFocus(func() {
		a.apply(func() {
			a.focused = true
			// Looking at the chat again clears its dot.
			a.clearUnread(a.currentValue())
			a.invalidate()
		})
	})
	win.OnBlur(func() { a.apply(func() { a.focused = false }) })
	go a.init()
}

// railForWindow is the rail's starting width: 21% of the window, clamped to the
// range a draggable divider allows.
func railForWindow(width float32) float32 {
	rail := width * 0.21
	if rail < 190 {
		return 190
	}
	if rail > width*0.30 {
		return width * 0.30
	}
	return rail
}

// apply runs fn on the UI thread. Without a window (tests) it runs inline.
func (a *App) apply(fn func()) {
	a.post(fn)
}

// post runs fn on the window's own thread, without waiting for it. Without a
// window (tests) it runs inline.
func (a *App) post(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	a.win.Update(fn)
}

// applyWait runs fn on the main thread and waits for it, running it directly
// when the caller is the main thread already. Goroutines use it when the next
// step depends on the state fn writes: win.Update only queues fn, so reading
// a.current right after apply can see the old value.
//
// It must not queue and then wait on a channel: click and shortcut handlers
// run on the main thread, and waiting there for the main loop itself deadlocks
// the window (macOS then says the application is not responding). RunOnMain
// runs fn directly when it is already the main thread, which is that case.
func (a *App) applyWait(fn func()) {
	if a.win == nil {
		fn()
		return
	}
	mygo.RunOnMain(fn)
}

// invalidate asks for one more frame.
func (a *App) invalidate() {
	if a.win == nil {
		return
	}
	a.win.Invalidate()
}

// fail records a client-side error, capped so a storm cannot grow unbounded.
func (a *App) fail(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("app: %s", msg)
	a.apply(func() {
		a.errs = append(a.errs, AppError{At: time.Now(), Message: msg})
		if len(a.errs) > 50 {
			a.errs = a.errs[len(a.errs)-50:]
		}
		a.invalidate()
	})
}

// init loads the shell and then subscribes to the open conversation. The app
// and the agent both start at login, so the first request may arrive before
// the bridge listens: retry for 30s before saying the bridge is down.
func (a *App) init() {
	ctx := context.Background()
	var err error
	for attempt := 1; attempt <= 30; attempt++ {
		err = a.loadState(ctx)
		if err == nil {
			break
		}
		a.apply(func() {
			a.retry = attempt
			a.reachable = false
			a.invalidate()
		})
		if attempt == 30 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		a.apply(func() {
			a.conn = ConnDown
			a.downReason = err.Error()
			a.invalidate()
		})
		return
	}
	a.apply(func() {
		a.retry = 0
		a.reachable = true
		a.lastOpen = time.Now()
		a.invalidate()
	})
	if a.secondary {
		a.reloadConversation(a.currentValue())
		return
	}
	a.openDefaultConversation()
	go a.pollApprovals()
	go a.pollSurfaces()
}

// surfacePoll is how often the rail re-reads the bridge. A chat started in
// Feishu or from the terminal is written by another process and nothing pushes
// it here, so without a poll the rail is a snapshot of the moment the app
// started.
const surfacePoll = 8 * time.Second

// pollSurfaces keeps the rail honest while other surfaces write conversations.
func (a *App) pollSurfaces() {
	ctx := context.Background()
	for {
		time.Sleep(surfacePoll)
		// A bridge that is down says so on the socket; a failed poll is not news.
		_ = a.loadState(ctx)
	}
}

// pollApprovals keeps the pending list fresh: a tool call can wait for the
// user while the socket is busy streaming something else.
func (a *App) pollApprovals() {
	ctx := context.Background()
	for {
		time.Sleep(5 * time.Second)
		if err := a.loadApprovals(ctx); err != nil {
			log.Printf("app: %v", err)
		}
	}
}

// openDefaultConversation selects the conversation the rail sorts first, as
// the earlier web client does on boot.
func (a *App) openDefaultConversation() {
	// The chat that was open last time comes back, as a desktop app's window
	// does; a chat that is gone falls through to the newest one.
	if last := a.prefs.LastChat; last != 0 && a.surfaceExists(last) {
		a.selectConversation(last)
		return
	}
	picked := 0
	a.applyWait(func() {
		if id := defaultConversationID(a.surfaces); id != 0 {
			a.setCurrent(id)
			picked = id
		}
	})
	a.reloadConversation(picked)
	a.savePrefsLater()
}

// defaultConversationID is the newest conversation that is not archived, which
// is the chat the earlier web client reopens.
func defaultConversationID(surfaces []protocol.Surface) int {
	newest := 0
	for _, surface := range surfaces {
		if surface.Archived || surface.ConversationID == 0 {
			continue
		}
		if surface.ConversationID > newest {
			newest = surface.ConversationID
		}
	}
	return newest
}

func (a *App) loadState(ctx context.Context) error {
	st, err := a.api.State(ctx)
	if err != nil {
		return err
	}
	a.apply(func() {
		a.noteTaskFinish(a.tasks, st.Tasks)
		// A poll that brings the same list costs no frame and no tray rebuild;
		// apply runs on the window's thread asynchronously, so the comparison
		// and everything that follows it live in this one closure.
		if a.sameState(st) {
			return
		}
		a.surfaces = st.Surfaces
		a.tasks = st.Tasks
		a.projects = st.Projects
		a.feishu = st.Feishu
		// The menu bar shows what is running, so the trays do not go stale.
		a.publishTray()
		// The other windows share the client, so they share this list too.
		broadcastSurfaces(a, st.Surfaces)
		a.invalidate()
	})
	return nil
}

// sameState says the poll brought nothing the rail draws differently.
func (a *App) sameState(st *protocol.State) bool {
	return reflect.DeepEqual(a.surfaces, st.Surfaces) &&
		reflect.DeepEqual(a.tasks, st.Tasks) &&
		reflect.DeepEqual(a.projects, st.Projects) &&
		reflect.DeepEqual(a.feishu, st.Feishu)
}

// reloadConversation loads the open conversation and (re)subscribes its socket.
// hasNewAnswer reports whether a frame brought an answer the reader had not
// seen, which is what a dot stands for.
func hasNewAnswer(before, after []protocol.Entry) bool {
	if len(after) <= len(before) {
		return false
	}
	for _, entry := range after[len(before):] {
		if entry.Kind == "pi.assistant" {
			return true
		}
	}
	return false
}

// growEntries takes the entries a frame carries and counts what arrived while
// the reader is scrolled away from the newest turn.
func (a *App) growEntries(entries []protocol.Entry) {
	merged := mergeEntries(a.entries, entries)
	if !a.atBottom && len(merged) > len(a.entries) {
		a.newMessages += len(merged) - len(a.entries)
	}
	a.entries = merged
	a.turnStale = true
}

// mergeEntries keeps the entries already held that fall before the frame's
// window. The bridge's watch window holds the newest 50 entries and drops its
// oldest when it would hold more than 100, so assigning the window would shrink
// a chat back to its tail on the first frame after loading it.
func mergeEntries(held, window []protocol.Entry) []protocol.Entry {
	if len(window) == 0 {
		return held
	}
	// A window reaching back as far as what is held is a full reload: it wins.
	if len(held) == 0 || window[0].ID <= held[0].ID {
		return window
	}
	keep := 0
	for keep < len(held) && held[keep].ID < window[0].ID {
		keep++
	}
	if keep == 0 {
		return window
	}
	merged := make([]protocol.Entry, 0, keep+len(window))
	merged = append(merged, held[:keep]...)
	return append(merged, window...)
}

func (a *App) reloadConversation(current int) {
	if current == 0 {
		return
	}
	ctx := context.Background()
	conv, err := a.api.Conversation(ctx, current)
	if err != nil {
		a.fail("load conversation %d: %v", current, err)
		return
	}
	a.apply(func() {
		a.entries = conv.Entries
		a.historyMore = conv.HasMore
		a.historyLoading = false
		a.turnStale = true
		a.model = conv.Model
		a.thinking = conv.ThinkingLevel
		if conv.Usage != nil {
			a.usage = *conv.Usage
		}
		a.waits = conv.ApprovalWaits
		a.turnStale = true
		a.live = nil
		// A newly opened chat lands on its newest turn, not its oldest.
		// Another chat is another list: its keys, its place and its scroll are
		// not this one's, so the list starts afresh and at its end.
		a.turnList = ui.ListState{FollowEnd: true}
		a.atBottom, a.newMessages = true, 0
		a.invalidate()
	})
	go a.watch(current)
	a.goLoad(func() error { return a.loadApprovals(ctx) })
	a.goLoad(func() error { return a.loadChanges(ctx) })
	a.goLoad(func() error { return a.loadGit(ctx) })
	a.goLoad(func() error { return a.loadModels(ctx) })
}

// currentValue reads the open conversation id on the UI thread.
func (a *App) currentValue() int {
	if a.win == nil {
		return a.current
	}
	return int(a.currentID.Load())
}

// setCurrent records the open conversation and keeps the goroutine-visible
// mirror in step; call it only inside an apply closure.
func (a *App) setCurrent(conversationID int) {
	a.current = conversationID
	a.currentID.Store(int64(conversationID))
}

// goLoad runs one loader in a goroutine and reports its failure in the panel.
func (a *App) goLoad(fn func() error) {
	go func() {
		if err := fn(); err != nil {
			a.fail("%v", err)
		}
	}()
}

// watch keeps the conversation's socket open; api.Watch reconnects on its own
// and closes the channel when ctx is cancelled.
func (a *App) watch(conversationID int) {
	ctx := a.ctx
	a.apply(func() {
		a.conn = ConnConnecting
		a.invalidate()
	})
	for frame := range a.api.Watch(ctx, conversationID) {
		if frame.Value == nil {
			continue
		}
		value := frame.Value
		a.apply(func() {
			if a.current != conversationID {
				return
			}
			a.conn = ConnOpen
			a.reachable = true
			a.lastOpen = time.Now()
			if !a.focused && hasNewAnswer(a.entries, value.Entries) {
				a.markUnread(conversationID)
			}
			a.growEntries(value.Entries)
			if doc, ok := value.Docs["pi.live"]; ok {
				a.live = &doc
			} else {
				a.live = nil
			}
			a.invalidate()
		})
		a.maybeNotify()
	}
	a.apply(func() {
		if a.current == conversationID {
			a.conn = ConnDown
			a.invalidate()
		}
	})
}

// selectConversation opens a chat from the rail.
func (a *App) selectConversation(conversationID int) {
	a.applyWait(func() {
		a.clearUnread(conversationID)
		if a.current == conversationID {
			return
		}
		a.setCurrent(conversationID)
		a.entries = nil
		a.turnStale = true
		a.live = nil
		a.approvals = nil
		a.invalidate()
	})
	a.reloadConversation(a.currentValue())
	// Remembering the chat after the switch, not before it, is what makes the
	// next launch land here.
	a.savePrefsLater()
	a.syncTitle()
}

// syncTitle puts the chat's name on the window, as a native document window
// does, so the Dock and the window menu say which chat is open.
func (a *App) syncTitle() {
	if a.win == nil {
		return
	}
	a.win.SetTitle(a.titleText())
}

// submit sends what the composer holds, as the Send button does.
func (a *App) submit() {
	a.sendText(a.draft)
}

// sendText sends one turn and clears the draft. The composer calls it with the
// text the editor holds, which is ahead of the bound value until the next
// frame.
func (a *App) sendText(text string) {
	images := a.pendingImages
	a.apply(func() { a.recall(text) })
	mode := api.ModeFollowUp
	if a.steer {
		mode = api.ModeSteer
	}
	a.apply(func() {
		a.draft = ""
		if a.current == 0 || (text == "" && len(images) == 0) || a.busy {
			return
		}
		a.busy = true
		a.pendingImages = nil
		a.invalidate()
	})
	if text == "" && len(images) == 0 {
		return
	}
	conversationID := a.currentValue()
	go func() {
		err := a.api.Submit(context.Background(), api.SubmitRequest{
			ConversationID: conversationID,
			Text:           text,
			Mode:           mode,
			Images:         submitImages(images),
		})
		a.apply(func() {
			a.busy = false
			a.invalidate()
		})
		if err != nil {
			a.fail("submit: %v", err)
		}
	}()
}

// abort stops the run in flight.
func (a *App) abort() {
	conversationID := a.currentValue()
	if conversationID == 0 {
		return
	}
	go func() {
		if err := a.api.Abort(context.Background(), conversationID); err != nil {
			a.fail("abort: %v", err)
		}
	}()
}

// decideApproval answers a pending tool call; session extends the grant to the
// rest of this conversation.
func (a *App) decideApproval(callID string, approved, session bool) {
	conversationID := a.currentValue()
	a.apply(func() {
		kept := a.approvals[:0]
		for _, wait := range a.approvals {
			if wait.CallID != callID {
				kept = append(kept, wait)
			}
		}
		a.approvals = kept
		a.invalidate()
	})
	go func() {
		err := a.api.DecideApproval(context.Background(), api.ApprovalDecision{
			CallID:         callID,
			Approved:       approved,
			Session:        session,
			ConversationID: conversationID,
		})
		if err != nil {
			a.fail("approval: %v", err)
		}
	}()
}

// View draws one frame.
func (a *App) View(c *ui.Context) {
	a.applyThemeChoice(c)
	t := theme.Current(c)
	theme.Apply(c, t)
	// the earlier client's view:16 binds meta/ctrl+F window-wide; OnWindowShortcut fires
	// with no element focused, which is the same reach.
	// ui.Cmd is Cmd on darwin and Ctrl elsewhere; a combined Super|Ctrl mask
	// matches neither, since modifiers are matched exactly.
	c.OnWindowShortcut(ui.Cmd, ui.KeyF, func() {
		a.apply(func() { a.findOpen = true; a.invalidate() })
	})
	// Cmd+K is the quick switcher, the one shortcut a native client can offer
	// that a web page cannot: it jumps to any chat without touching the rail.
	c.OnWindowShortcut(ui.Cmd, ui.KeyK, func() {
		a.apply(func() { a.openSwitcher() })
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyN, func() {
		a.apply(func() { a.newConversation() })
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyUp, func() { a.scrollPage(-1) })
	c.OnWindowShortcut(ui.Cmd, ui.KeyDown, func() { a.scrollPage(1) })
	for i, key := range quickKeys {
		i, key := i, key
		c.OnWindowShortcut(ui.Cmd, key, func() {
			if id := a.chatForQuickKey(i); id != 0 {
				a.apply(func() { a.selectConversation(id) })
			}
		})
	}
	// ⌘W closes the window the way the red button does, which is what a
	// desktop app's owner expects from the key.
	c.OnWindowShortcut(ui.Cmd, ui.KeyQ, func() { a.askQuit() })
	c.OnWindowShortcut(ui.Cmd, ui.KeyComma, func() {
		a.apply(func() { a.settingsOpen = true; a.invalidate() })
	})
	// Linear's j/k, moved to ⌥ so a bare j can still be typed.
	c.OnWindowShortcut(ui.Alt, ui.KeyDown, func() { a.stepRail(1) })
	c.OnWindowShortcut(ui.Alt, ui.KeyUp, func() { a.stepRail(-1) })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyP, func() { a.togglePinSelected() })
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyBackspace, func() { a.archiveSelected() })
	c.OnWindowShortcut(ui.Cmd, ui.KeyW, func() {
		if a.win != nil {
			a.win.Close()
		}
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeySlash, func() {
		a.apply(func() { a.shortcutsOpen = true; a.invalidate() })
	})
	c.OnWindowShortcut(ui.Cmd|ui.Shift, ui.KeyN, func() {
		a.openInNewWindow(a.currentValue())
	})
	c.OnWindowShortcut(ui.Cmd, ui.KeyB, func() {
		a.apply(func() { a.toggleSide(); a.invalidate() })
	})
	// A window can come up narrower than the saved rail; clamp before the split
	// so the header and the composer keep their room.
	if width, _ := c.Size(); width > 0 {
		a.fitPanes(width)
		a.recoverTinyWindow(width)
	}
	root := ui.Split(c, &a.railWidth, func() { a.rail(c) }, func() { a.pane(c) }).Fill()
	// Files dragged from Finder land on the whole window: images become
	// attachments, anything else opens in the viewer, which is more than the
	// earlier web client could do with a drop.
	if paths := root.DroppedFiles(); len(paths) > 0 {
		a.dropPaths(paths)
	}
	a.switcherDialog(c, t)
	a.chatMenuDialog(c, t)
	a.taskDialog(c)
	a.quitDialog(c, t)
	a.settingsDialog(c, t)
	a.shortcutsDialog(c, t)
	a.logsDialog(c, t)
	a.imageDialog(c, t)
	a.previewDialog(c, t)
	a.errorPanel(c, t)
	a.toasts(c)
	// The split writes the dragged width into railWidth; keep a copy once it
	// settles, so the next launch opens the same size.
	if diff := a.railWidth - a.prefs.RailWidth; diff > 2 || diff < -2 {
		a.prefs.RailWidth = a.railWidth
		a.savePrefsLater()
	}
}

// applyThemeChoice honours the user's light/dark/system pick before the
// tokens are read, as ThemeToggle did by writing the class on <html>.
func (a *App) applyThemeChoice(c *ui.Context) {
	if a.prefs.Theme != "light" && a.prefs.Theme != "dark" {
		return
	}
	base := *c.Theme()
	base.Dark = a.prefs.Theme == "dark"
	c.SetTheme(&base)
}

// toggleTheme flips between light and dark and remembers it.
// toggleTheme walks the three states the earlier client's view:8-20 cycles: follow the
// system, light, dark, and back to following the system.
func (a *App) toggleTheme() {
	a.apply(func() {
		a.prefs.Theme = nextTheme(a.prefs.Theme)
		a.savePrefsLater()
		a.invalidate()
	})
}

func nextTheme(theme string) string {
	switch theme {
	case "":
		return "light"
	case "light":
		return "dark"
	}
	return ""
}

// toggleLang switches between Chinese and English, which the settings panel and
// the ⌘ palette also do.
func (a *App) toggleLang() {
	a.apply(func() {
		if a.prefs.Lang == string(i18n.ZH) {
			a.prefs.Lang = string(i18n.EN)
		} else {
			a.prefs.Lang = string(i18n.ZH)
		}
		i18n.Set(i18n.Lang(a.prefs.Lang))
		a.savePrefsLater()
		a.invalidate()
	})
}

// main draws the middle pane: the chat's header, the transcript, the composer.
// pane is everything right of the rail: the chat, plus the side panel when it
// is open. The side panel owns its own width so dragging it does not move the
// rail.
func (a *App) pane(c *ui.Context) {
	if !a.sideOpen {
		a.main(c)
		a.sideSynced = false
		return
	}
	// main | side, with the divider between them draggable and remembered, as
	// the earlier client's PanelResizeHandle is (the earlier client's view:74-96). Split sizes its first pane
	// in DIPs, so mainPane is what the divider moves; the panel width is the
	// remainder, which is what prefs keeps.
	if a.mainPane <= 0 {
		a.mainPane = 760
	}
	pane := ui.Split(c, &a.mainPane, func() { a.main(c) }, func() { a.sidePanel(c) }).
		Fill().SplitReset(760)
	bounds := pane.Bounds()
	if bounds.W <= 0 {
		return
	}
	a.syncPanes(float32(bounds.W))
}

// syncPanes shares the room Split laid out between the chat and the panel. The
// divider is what changes the panel's width: a window that grew or shrank hands
// the difference to the chat, so resizing does not quietly widen the panel and
// leave the transcript where it was.
func (a *App) syncPanes(room float32) {
	if room <= 0 {
		return
	}
	if !a.sideSynced {
		// The room Split has is only known once laid out: take the saved panel
		// width out of it on the first frame that shows the panel.
		a.sideSynced = true
		a.sideRoom = room
		a.mainPane = room - float32(a.sideWidthOf())
		return
	}
	if a.sideRoom != room {
		// The window changed size under the panel: it keeps the width it has.
		a.sideRoom = room
		a.mainPane = room - float32(a.sideWidthOf())
		return
	}
	if side := room - a.mainPane; side > 0 && absf(side-float32(a.sideWidthOf())) > 2 {
		// The divider moved: that is the width the panel keeps, live and saved,
		// so the next resize hands the difference to the chat.
		a.sideWidth = side
		a.prefs.SideWidth = side
		a.savePrefsLater()
	}
}

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func (a *App) main(c *ui.Context) {
	if a.view == ViewFile {
		// The file viewer takes the chat's place, as the earlier client swaps the transcript
		// for FileViewer instead of opening a window.
		a.fileView(c)
		return
	}
	ui.Column(c).Fill().Children(func() {
		a.mainHeader(c)
		ui.Divider(c)
		a.transcript(c)
		a.jumpBar(c)
		if a.modelOpen {
			a.modelPalette(c)
		}
		a.composer(c)
	})
}

// mainHeader names the chat and holds the actions that act on it, as the earlier client's view
// puts reset and stop above the transcript.
func (a *App) mainHeader(c *ui.Context) {
	t := theme.Current(c)
	// h-12 px-3.5 gap-2, title 13/medium over a 10 px mono line: the earlier client's view's header.
	ui.Row(c).Height(48).PaddingX(14).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, a.currentTitle()).FontSize(13).FontWeight(500).
				TextColor(t.Foreground).MaxLines(1)
			ui.Text(c, a.currentMeta()).FontSize(10).Font("monospace").
				TextColor(t.MutedFg).MaxLines(1)
		})
		if a.findOpen {
			a.findBar(c, t)
		}
		iconButton(c, icons.Search, i18n.T("findShort")).OnClick(func() {
			a.apply(func() { a.findOpen = !a.findOpen; a.invalidate() })
		})
		// The the earlier client titles are the commands themselves: /new and /stop.
		iconButton(c, icons.SquarePen, "/new").OnClick(func() { a.resetConversation() })
		stop := iconButton(c, icons.Square, "/stop")
		if a.busy || a.live != nil {
			// Busy turns the stop red, as the header tints it text-rose-500.
			stop.OnClick(func() { a.abort() })
		}
		// Last on the right: it is the edge the panel opens from.
		panel := "Show panel"
		if a.sideOpen {
			panel = "Hide panel"
		}
		side := iconButtonTint(c, icons.PanelRight, panel, t.MutedFg)
		if a.sideOpen {
			side = iconButtonTint(c, icons.PanelRight, panel, t.Primary)
		}
		side.OnClick(func() { a.toggleSide() })
	})
}

// iconButton is a borderless button holding one icon, as the earlier client header's
// ghost icon buttons are: no fill until the pointer is on it, 24 square.
func iconButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	return iconButtonTint(c, icon, label, theme.Current(c).MutedFg)
}

// iconButtonTint is the same button with the icon in a given color, which the
// right-panel toggle uses to show the panel is open.
func iconButtonTint(c *ui.Context, icon *ui.SVG, label string, tint ui.Color) ui.Element {
	t := theme.Current(c)
	// The background fades under the pointer instead of switching, as the
	// the earlier client header's transition-colors does.
	button := ui.ButtonBase(c).Size(24, 24).Radius(6).Cursor(ui.CursorPointer).
		Transition(motion.Hover())
	button.Label(label)
	button.Tooltip(label)
	if button.Hovered() {
		button.Background(t.Accent)
	}
	button.Children(func() {
		ui.Icon(c, icon).Size(12, 12).TextColor(tint)
	})
	return button
}

// currentTitle is the open chat's name.
func (a *App) currentTitle() string {
	for _, surface := range a.surfaces {
		if surface.ConversationID == a.current {
			return surfaceTitle(surface)
		}
	}
	return ""
}

// currentMeta is the chat's ids, which the Feishu thread and the CLI both use.
func (a *App) currentMeta() string {
	if a.current == 0 {
		return ""
	}
	for _, surface := range a.surfaces {
		if surface.ConversationID == a.current {
			return surface.SurfaceID + "  #" + itoa(a.current)
		}
	}
	return "#" + itoa(a.current)
}

// resetConversation clears the chat's history on the agent, as /new does.
func (a *App) resetConversation() {
	id := a.currentValue()
	if id == 0 {
		return
	}
	a.goLoad(func() error {
		if err := a.api.Reset(contextBG(), id); err != nil {
			return fmt.Errorf("reset conversation %d: %w", id, err)
		}
		a.reloadConversation(id)
		return nil
	})
}

// surfaceExists reports whether the rail still lists a chat, which a restart
// checks before reopening it.
func (a *App) surfaceExists(conversationID int) bool {
	for _, s := range a.surfaces {
		if s.ConversationID == conversationID && !s.Archived {
			return true
		}
	}
	return false
}
