// Package app is the desktop window: a rail of chats and tasks, one read-only
// transcript, and a composer that turns into an approval card when a tool call
// is waiting. Every frame draws from the mirror of the bridge kept current by
// the events below; nothing here decides what the agent should do, it only
// shows and forwards.
package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"hash/fnv"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
	"pilot/internal/protocol"
	"pilot/internal/theme"
)

// Config is what the window needs to find its bridge.
type Config struct {
	BaseURL string
	Token   string
}

// App holds the mirror of the bridge and the view-model that hangs off it.
//
// Everything is mutated on the frame goroutine: background work posts events to
// inbox and View drains them before drawing. That is the whole synchronization
// story — no frame ever sees a half-applied page. The one exception is the
// surface list, which the tail prober reads under mu, because probing every
// chat is too much work to hand back to the frame.
type App struct {
	cfg Config
	api *api.Client
	win *mygo.Window
	// windowGrew is the width this app has added to the window for the open
	// drawer, so closing it gives back exactly that and no more.
	windowGrew float32
	// drawerOverlay is true when the window could not grow for the open drawer:
	// the drawer then floats above the chat instead of narrowing it.
	drawerOverlay bool

	ctx    context.Context
	cancel context.CancelFunc
	inbox  chan event

	mu       sync.RWMutex
	surfaces []protocol.Surface

	tasks   []protocol.Task
	pending []protocol.PendingApproval
	// queued is the bridge's queue split by conversation: the rail badges the
	// chat a waiting message sits behind.
	queued map[int]int
	// runningConvs is the chats with a turn in flight, from the bridge's
	// health: the rail marks them so a chat that is working does not look
	// idle.
	runningConvs map[int]bool
	online       bool
	// stamp changes whenever the rail's inputs change, so railRows can be cached.
	stamp uint64

	active  int
	threads map[int]*threadStore

	// md is the transcript's Markdown options, rebuilt only when the theme's
	// colours change: markdown.View throws its state away when the pointer does.
	md mdCache

	// prefs is what the window keeps between launches: language and theme, which
	// have no dialog of their own.
	prefs prefs
	// tl is the timeline drawer and the events it last derived.
	tl timelineState
	// notes is what the last snapshot said about the chats nobody is looking at.
	// It only exists to tell a finished turn from one that finished long ago.
	notes notifier
	// zoom is the picture opened from a message, if one is open over the window.
	zoom zoomState
	// delivery is when and how often an alert may interrupt: quiet hours, and a
	// ceiling on finished turns a day.
	delivery delivery
	// banner is where an alert goes. The window hands it to the system; tests
	// keep it, because whether a banner is right is a question about snapshots,
	// not about Notification Center.
	banner func(alert)
	// dark is the theme the last frame drew with, so a toggle flips what is on
	// screen rather than what macOS says.
	dark bool
	// railW is the rail's width now, which the saved preference seeds and the
	// edge drags; dragging is true while the pointer holds it.
	railW    float32
	dragging bool
	// railSearch is true while the rail's search field is open: Beautiful UI's
	// sidebar keeps it folded behind its icon until it is asked for, so the chat
	// list has the width when nobody is typing.
	railSearch bool
	// railCollapsed folds the rail to its icons. railWide is the width it had
	// before, so expanding puts it back rather than back to the default.
	railCollapsed bool
	railWide      float32

	// watch is the live subscription for the active chat.
	watchCancel context.CancelFunc
	watchConv   int
	wsLive      bool
	refreshBusy atomic.Bool
	tailBusy    atomic.Bool

	rail   railSection
	filter string
	// ext is the extensions list the rail shows in place of the chat rows.
	ext extPanel
	// taskFilter narrows the task tab to one state; empty means all three.
	taskFilter string
	now        time.Time
	vm         viewModel
	// focusComposer asks the next frame to put the cursor in the composer.
	focusComposer bool

	// composer
	draft      string
	draftImage []imageRef
	history    []string
	histIndex  int
	notice     string
	// noticeOK marks the notice as news rather than a complaint, so the line is
	// not painted in the color of failure.
	noticeOK bool
	busy     map[string]bool // call id → decision in flight

	// palette is the ⌘K field: find a chat, or name an action.
	palette paletteState

	// cheatsheet is the shortcut sheet, ⌘⇧/.
	cheatsheet bool

	// repoFiles is what ⌘P lists: the checkout's paths, fetched when it opens.
	repoFiles repoFiles
	// mention is the @ popover in the composer.
	mention mentionState

	// search is the cross-chat search behind the filter box. Its hits belong to
	// query, and seq retires the replies of queries the user has typed past.
	search searchState
	// rename is the dialog the row menu opens: which chat it renames and the
	// text in it, so a frame that redraws the dialog keeps what was typed.
	rename renameState

	// model is the chat's own model, and thinking its level; both come from the
	// conversation page, which is the only place the bridge states them.
	model        *protocol.ModelRef
	thinking     string
	defaultModel *protocol.ModelRef
	pick         modelPick

	// mem is the memory drawer: closed until the header button opens it.
	mem memoryPanel
	// settings is the preferences dialog, closed until the footer gear opens it.
	settings settingsPanel
	// find is the Cmd-F bar over the transcript.
	find findState
	// changes is the changes drawer: the worktree patch and the branch.
	changes changesPanel
	// detail is the dialog a task row opens, and usage the calendar of what the
	// last four months cost.
	detail taskDetailPanel
	usage  usagePanel
	// write is the commit / push / merge the drawer offers and what the last
	// one answered.
	write writePanel
	// projects is the drawer of repositories a chat can live in.
	projects projectsPanel
	// newChatPanel is the dialog that picks a project and a branch before a chat
	// starts: the branch name is what ties one requirement across projects.
	newChatPanel newChatState
	// stageStrip is the pipeline strip for the open chat: derived by the bridge
	// from git and GitLab, drawn here and never decided here.
	stageStrip *protocol.Stages
	// stageOpen unfolds the whole pipeline. Folded is the default: the strip
	// answers "what now", and three of its four steps are not that.
	stageOpen bool
}

type viewModel struct {
	// tails is the newest entry time per chat, as probed from the bridge.
	tails  map[int]time.Time
	unread map[int]bool
	// seen is the newest entry the user has had on screen in a chat, and newFrom
	// is where that stood when they opened the chat they are in now. The line
	// that marks what arrived while they were away is drawn from the gap.
	seen    map[int]int
	newFrom map[int]int
	// folds is the disclosure state of folded step rows, keyed by
	// (conversationID, turnID).
	folds map[string]bool
	// groups is the disclosure state of a run's per-tool groups, keyed by
	// (row id, tool name). A group not in the map is closed: the first thing
	// an opened run shows is what it used, not every call it made.
	groups map[string]bool
	// longOpen is whether a long answer has been opened to its full length,
	// keyed by row id. A message not in the map is folded to a preview.
	longOpen   map[any]bool
	projection map[int]projectionCache
	railCache  []railRow
	railStamp  uint64
	railFilter string
	// railState is the task tab's state filter the cache was built with.
	railState string

	railList   ui.ListState
	threadList ui.ListState
}

type threadStore struct {
	convID  int
	entries []protocol.Entry
	rev     map[int]uint64
	live    protocol.LiveDoc
	// inbox is pi.inbox for this chat: messages waiting behind the running turn.
	inbox   []protocol.InboxItem
	loaded  bool
	failed  bool
	version uint64
	// usage is the chat's running total: what it has cost and spent, as the
	// bridge last stated it on the page.
	usage *protocol.Usage
	// hasMore says the bridge holds entries older than this page, and
	// loadingOlder keeps one fetch of them in flight.
	hasMore      bool
	loadingOlder bool
	// olderTried is when the last read backwards started: a page that failed
	// must not be retried on every frame.
	olderTried time.Time
	// prefetched counts the pages read ahead for this chat: the pages behind
	// the newest one are fetched while the reader is still at the bottom, so
	// walking up finds them already here instead of a pause.
	prefetched int
	// prefetching marks the read ahead in flight, which draws no line: nothing
	// is waiting on it yet.
	prefetching bool
}

// New builds the window state and starts the work that feeds it: a five-second
// reconcile and a staggered tail probe for the rail's relative times.
func New(cfg Config) *App {
	a := newApp(cfg)
	// The saved language and theme are the window's own, read once at startup so
	// the frames a test draws by hand stay independent of the owner's settings.
	a.prefs = loadPrefs()
	a.prefs.applyLang()
	a.delivery = deliveryFor(a.prefs, time.Now)
	a.railW = a.prefs.railWidth()
	for key, entry := range a.prefs.Seen {
		if convID, err := strconv.Atoi(key); err == nil {
			a.vm.seen[convID] = entry
		}
	}
	go a.reconcileLoop()
	go a.elapsedLoop()
	go a.tailLoop()
	return a
}

// newApp is New without the goroutines, for the frames a test draws by hand.
func newApp(cfg Config) *App {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		cfg:       cfg,
		api:       api.New(cfg.BaseURL, cfg.Token),
		ctx:       ctx,
		cancel:    cancel,
		inbox:     make(chan event, 256),
		threads:   map[int]*threadStore{},
		busy:      map[string]bool{},
		histIndex: -1,
		railW:     railWidth,
		vm: viewModel{
			tails:      map[int]time.Time{},
			unread:     map[int]bool{},
			seen:       map[int]int{},
			newFrom:    map[int]int{},
			folds:      map[string]bool{},
			groups:     map[string]bool{},
			longOpen:   map[any]bool{},
			projection: map[int]projectionCache{},
		},
	}
	a.banner = a.showBanner
	return a
}

// Attach gives the app its window, so background work can ask for a repaint.
func (a *App) Attach(win *mygo.Window) { a.win = win }

// Close stops every goroutine the app owns.
func (a *App) Close() {
	a.cancel()
	if a.watchCancel != nil {
		a.watchCancel()
	}
}

func (a *App) invalidate() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

// post hands one result to the frame goroutine. A full inbox means the window
// is not drawing at all, and dropping the event is the only thing left to do.
func (a *App) post(ev event) {
	select {
	case a.inbox <- ev:
	default:
	}
	// A frame is what drains the inbox, so every event asks for one; without
	// this the window would only catch up on the next mouse move.
	a.invalidate()
}

type eventKind int

const (
	evSnapshot eventKind = iota
	evThread
	evOlder
	evFrame
	evWatchDown
	evTail
	evNotice
	evFiles
	evDecided
	evSelect
	evProbeTails
	evDraft
	// The rail's two writes on a chat, and the search box's round trip: all
	// posted from menus, dialogs and a debounce timer, applied on the frame.
	evRenameChat
	evArchiveChat
	// A pin is the window's own bookkeeping, so it never leaves the machine.
	evPinChat
	evMetaFailed
	evSearch
	evSearchDone
	// The model panel: the catalog's reply, and the write's outcome.
	evModels
	evModelSet
	// The memory drawer: its three lists, a curation pass, and the write.
	evMemory
	evCurate
	evMemorySaved
	// The changes drawer: the worktree patch and the branch it sits on.
	evChanges
	evGit
	// The branch tab: the ref list, and one write's outcome.
	evBranches
	evBranchWrite
	// A task's detail, its stop, its close from a rail row, and the usage
	// calendar's daily samples.
	evTaskDetail
	evTaskLog
	evTaskStop
	evTaskCancel
	evTaskReschedule
	evUsage
	// A file's text, for the preview beside a patch.
	evFile
	// A write on the worktree: its commit, its push, its merge.
	evWrite
	// The projects drawer: the list, a path added, a project forgotten.
	evProjects
	evProjectAdd
	evProjectRemove
	// The projects drawer's lower half: leftover checkouts, and one adopted.
	evStrays
	evAdopt
	evExtensions
	evExtToggle
	evWorkflow
	evWorkflowSet
	// The merge tab: the open request, and the answer to opening or merging one.
	evMerge
	evMergeWrite
	evPipeline
	evJobLog
	evPipelineWrite
	evBranchName
	evNewChat
	evStages
	// A picture opened from a message, once it has decoded.
	evZoom
	// ⌘P's answer: the checkout whose files were read, and which opening asked.
	evRepoFiles
)

type event struct {
	kind eventKind

	snapshot *snapshotResult
	page     *pageResult
	older    *olderResult
	frame    *frameResult
	tail     *tailResult
	notice   string
	ok       bool
	text     string
	convID   int
	callID   string
	images   []imageRef
	// A menu choice carries where the chat was going; a search reply carries
	// the hits, the query they answer, and the generation it belongs to.
	archived bool
	seq      int
	// closing marks a task stop that closes rather than interrupts: the task was
	// asleep or waiting, and the dialog names the notice for what it did.
	closing  bool
	hits     []protocol.SearchHit
	undo     *surfaceUndo
	models   *protocol.Models
	memory   *protocol.Memory
	curation *protocol.Curation
	applied  *protocol.MemoryApplied
	changes  *protocol.Changes
	git      *protocol.Git
	// branches is the ref list; branchOut is what a create or a delete answered.
	branches  *protocol.Branches
	branchOut *api.BranchResult
	// taskID and task are the dialog's round trip: which task was asked about,
	// and what came back.
	taskID int
	task   *api.TaskDetail
	// newID is the task a reschedule created: the bridge replaces a sleeping
	// routine rather than moving its deadline.
	newID int
	// stages is the task's step list, fetched separately because it is read from
	// the store rather than from the task's own record.
	stages []api.TaskLogStage
	// fileText is the preview's answer: the path it was asked for and the text.
	path      string
	fileText  string
	truncated bool
	// A write's answer: which one, whether it went well, and what to read.
	writeKind writeKind
	writeBad  bool
	// projects is the drawer's catalogue; the path that asked to join it rides
	// in path, above.
	projects []protocol.Project
	// worktrees is the projects drawer's leftover-checkout list.
	worktrees []protocol.Worktree
	// extensions is the installed set; the name a switch asked about rides in
	// text, above.
	extensions []protocol.Extension
	// workflow is a workflow read or write; text carries the project it belongs to.
	workflow *protocol.Workflow
	// merge is the merge tab's read, and mergeWrite its write.
	merge      *protocol.MRStatus
	mergeWrite *protocol.MRWrite
	// pipeline is the CI tab's read; jobID and the log fields carry a job's
	// trace back to the row that asked for it.
	pipeline    *protocol.Pipeline
	branchName  *protocol.BranchName
	stageReport *protocol.Stages
	jobID       int
	logCut      bool
	logTail     string
	usage       []api.UsageDay
	// zoom is a decoded picture on its way to the overlay.
	zoom *ui.Bitmap
	// files is ⌘P's answer: the checkout's paths, relative to its root.
	files []string
	err   error
}

type snapshotResult struct {
	state     *protocol.State
	tasks     []protocol.Task
	pending   []protocol.PendingApproval
	queued    map[int]int
	running   map[int]bool
	ok        bool
	tasksOK   bool
	pendingOK bool
	err       error
}

type pageResult struct {
	convID  int
	entries []protocol.Entry
	// model and thinking belong to the chat, not the window: the picker in the
	// composer draws them, and this page is where the bridge states them.
	model    *protocol.ModelRef
	thinking string
	usage    *protocol.Usage
	hasMore  bool
	err      error
}

type frameResult struct {
	convID int
	frame  protocol.Frame
}

type tailResult struct {
	convID int
	at     time.Time
}

// reconcileLoop is the five-second safety net: it re-reads the rail's snapshot
// whether or not the socket is healthy, and reloads the open chat while the
// socket is not carrying it.
func (a *App) reconcileLoop() {
	a.requestRefresh()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.requestRefresh()
		}
	}
}

// elapsedLoop is the one-second heartbeat behind the running turn's clock: a
// five-second refresh is fine for the rail, but a stopwatch that jumps by fives
// is not a stopwatch.
func (a *App) elapsedLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if a.running() {
				a.invalidate()
			}
		}
	}
}

// requestRefresh reads the snapshot in parallel, one round at a time.
func (a *App) requestRefresh() {
	if !a.refreshBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.refreshBusy.Store(false)
		ctx, cancel := context.WithTimeout(a.ctx, 8*time.Second)
		defer cancel()
		result := &snapshotResult{}
		var wg sync.WaitGroup
		wg.Add(4)
		go func() {
			defer wg.Done()
			state, err := a.api.State(ctx)
			if err != nil {
				result.err = err
				return
			}
			result.state, result.ok = state, true
		}()
		go func() {
			defer wg.Done()
			tasks, err := a.api.Tasks(ctx)
			if err == nil {
				result.tasks, result.tasksOK = tasks, true
			}
		}()
		go func() {
			defer wg.Done()
			pending, err := a.api.Approvals(ctx)
			if err == nil {
				result.pending, result.pendingOK = pending, true
			}
		}()
		go func() {
			defer wg.Done()
			health, err := a.api.Health(ctx)
			if err == nil {
				queued := make(map[int]int, len(health.Queued))
				for id, count := range health.Queued {
					if conversation, err := strconv.Atoi(id); err == nil && conversation > 0 {
						queued[conversation] = count
					}
				}
				result.queued = queued
				running := make(map[int]bool, len(health.RunningNow))
				for id, on := range health.RunningNow {
					if conversation, err := strconv.Atoi(id); err == nil && conversation > 0 && on {
						running[conversation] = true
					}
				}
				result.running = running
			}
		}()
		wg.Wait()
		a.post(event{kind: evSnapshot, snapshot: result})
	}()
}

// tailLoop re-probes every chat's newest entry once a minute, so the rail's
// relative times stay true as the window sits open.
func (a *App) tailLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			// The prober reads the view-model, so it is asked for on the frame
			// goroutine rather than run from this one.
			a.post(event{kind: evProbeTails})
		}
	}
}

// probeTails reads the newest entry of every chat without a known time yet, or
// of all of them, one request every 150ms.
func (a *App) probeTails() {
	if !a.tailBusy.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer a.tailBusy.Store(false)
		known := a.tailTimes()
		var pending []protocol.Surface
		for _, s := range a.surfaceSnapshot() {
			if _, ok := known[s.ConversationID]; !ok {
				pending = append(pending, s)
			}
		}
		// A window that has never run holds no times at all, and the rail sorts
		// by them: probing one chat every 150ms means a hundred chats take a
		// quarter of a minute to settle, with rows hopping under the cursor the
		// whole way. Six at a time lands the same set in about a second.
		work := make(chan protocol.Surface)
		var wg sync.WaitGroup
		for range tailProbes {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for s := range work {
					ctx, cancel := context.WithTimeout(a.ctx, 4*time.Second)
					page, err := a.api.ConversationTail(ctx, s.ConversationID)
					cancel()
					if err == nil {
						a.post(event{kind: evTail, tail: &tailResult{convID: s.ConversationID, at: newestTime(page.Entries)}})
					}
				}
			}()
		}
		for _, s := range pending {
			select {
			case <-a.ctx.Done():
			case work <- s:
			}
		}
		close(work)
		wg.Wait()
	}()
}

// tailProbes is how many chats' newest times are asked for at once.
const tailProbes = 6

// tailTimes is a snapshot of which chats already have a time, taken on the
// frame goroutine so the prober does not read the view-model.
type tailMap map[int]struct{}

func (a *App) tailTimes() tailMap {
	out := tailMap{}
	for _, s := range a.surfaceSnapshot() {
		if at, ok := a.vm.tails[s.ConversationID]; ok && !at.IsZero() {
			out[s.ConversationID] = struct{}{}
		}
	}
	return out
}

func (a *App) surfaceSnapshot() []protocol.Surface {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]protocol.Surface, len(a.surfaces))
	copy(out, a.surfaces)
	return out
}

func (a *App) setSurfaces(surfaces []protocol.Surface) {
	a.mu.Lock()
	a.surfaces = surfaces
	a.mu.Unlock()
}

// loadThread reads one chat's newest page; the watch follows it.
func (a *App) loadThread(convID int) {
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		conversation, err := a.api.Conversation(ctx, convID)
		result := &pageResult{convID: convID, err: err}
		if err == nil {
			result.entries = conversation.Entries
			result.model = conversation.Model
			result.thinking = conversation.ThinkingLevel
			result.usage = conversation.Usage
			result.hasMore = conversation.HasMore
		}
		a.post(event{kind: evThread, page: result})
	}()
}

// watch keeps the active chat live until it is replaced or the app closes.
func (a *App) watch(convID int) {
	if a.watchCancel != nil {
		a.watchCancel()
		a.watchCancel = nil
	}
	a.watchConv = convID
	a.wsLive = false
	if convID == 0 {
		return
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.watchCancel = cancel
	frames := a.api.Watch(ctx, convID)
	go func() {
		for frame := range frames {
			a.post(event{kind: evFrame, frame: &frameResult{convID: convID, frame: frame}})
		}
		a.post(event{kind: evWatchDown, convID: convID})
	}()
}

// drain applies everything that arrived since the last frame.
func (a *App) drain() {
	for {
		select {
		case ev := <-a.inbox:
			a.apply(ev)
		default:
			return
		}
	}
}

func (a *App) apply(ev event) {
	switch ev.kind {
	case evSnapshot:
		a.applySnapshot(ev.snapshot)
	case evThread:
		a.applyPage(ev.page)
	case evOlder:
		a.applyOlder(ev.older)
	case evFrame:
		a.applyFrame(ev.frame)
	case evWatchDown:
		if ev.convID == a.active {
			a.wsLive = false
			a.loadThread(ev.convID)
		}
	case evTail:
		a.applyTail(ev.tail)
	case evNotice:
		a.notice = ev.notice
		a.noticeOK = ev.ok
	case evFiles:
		a.addImages(ev.images)
	case evDraft:
		// Appended rather than replacing: a file dropped on a half-written
		// message is one more thing to say, not a reason to lose the first.
		switch {
		case strings.TrimSpace(a.draft) == "":
			a.draft = ev.text
		default:
			a.draft = a.draft + "\n\n" + ev.text
		}
		a.histIndex = len(a.history)
	case evDecided:
		delete(a.busy, ev.callID)
	case evSelect:
		a.selectConversation(ev.convID)
	case evProbeTails:
		a.probeTails()
	case evRenameChat:
		a.openRename(ev.convID)
	case evArchiveChat:
		a.setArchived(ev.convID, ev.archived)
	case evPinChat:
		a.prefs.pinChat(ev.convID, ev.archived)
		savePrefs(a.prefs)
		a.invalidate()
	case evMetaFailed:
		a.undoSurface(ev.undo)
	case evSearch:
		a.startSearch(ev.seq, ev.text)
	case evSearchDone:
		a.applySearch(ev.seq, ev.text, ev.hits)
	case evModels:
		a.applyModels(ev.models, ev.err)
	case evModelSet:
		a.applyModelSet(ev.convID, ev.err)
	case evMemory:
		a.applyMemory(ev.convID, ev.memory, ev.err)
	case evCurate:
		a.applyCuration(ev.curation, ev.err)
	case evMemorySaved:
		a.applyMemorySaved(ev.applied, ev.err)
	case evChanges:
		a.applyChanges(ev.convID, ev.changes, ev.err)
	case evGit:
		a.applyGit(ev.convID, ev.git, ev.err)
	case evBranches:
		a.applyBranches(ev.convID, ev.branches, ev.err)
	case evBranchWrite:
		a.applyBranchWrite(ev.convID, ev.text, ev.branchOut, ev.err)
	case evTaskDetail:
		a.applyTaskDetail(ev.taskID, ev.seq, ev.task, ev.err)
	case evTaskLog:
		a.applyTaskLog(ev.taskID, ev.seq, ev.stages, ev.err)
	case evTaskStop:
		a.applyTaskStop(ev.taskID, ev.seq, ev.closing, ev.err)
	case evTaskCancel:
		a.cancelTaskOffRail(ev.taskID)
	case evTaskReschedule:
		a.applyTaskReschedule(ev.taskID, ev.seq, ev.newID, ev.err)
	case evUsage:
		a.applyUsage(ev.seq, ev.usage, ev.err)
	case evZoom:
		a.applyZoom(ev.zoom, ev.err)
	case evFile:
		a.applyFileText(ev.convID, ev.path, ev.fileText, ev.truncated, ev.err)
	case evWrite:
		a.applyWrite(ev.convID, ev.writeKind, ev.text, ev.writeBad)
	case evRepoFiles:
		a.applyRepoFiles(ev.seq, ev.path, ev.files, ev.err)
	case evProjects:
		a.applyProjects(ev.seq, ev.projects, ev.err)
	case evStrays:
		a.applyStrays(ev.seq, ev.text, ev.worktrees, ev.err)
	case evAdopt:
		a.applyAdopt(ev.path, ev.convID, ev.err)
	case evExtensions:
		a.applyExtensions(ev.seq, ev.convID, ev.extensions, ev.err)
	case evExtToggle:
		a.applyExtToggle(ev.convID, ev.text, ev.extensions, ev.err)
	case evWorkflow:
		a.applyWorkflow(ev.convID, ev.workflow, ev.err)
	case evWorkflowSet:
		a.applyWorkflowSet(ev.text, ev.workflow, ev.err)
	case evMerge:
		a.applyMerge(ev.convID, ev.merge, ev.err)
	case evMergeWrite:
		a.applyMergeWrite(ev.convID, ev.mergeWrite, ev.err)
	case evPipeline:
		a.applyPipeline(ev.convID, ev.pipeline, ev.err)
	case evJobLog:
		a.applyJobLog(ev.jobID, ev.text, ev.logCut, ev.logTail)
	case evPipelineWrite:
		a.applyPipelineWrite(ev.text)
	case evBranchName:
		a.applyBranchName(ev.convID, ev.branchName, ev.err)
	case evNewChat:
		a.applyNewChat(ev.convID, ev.err)
	case evStages:
		a.applyStages(ev.convID, ev.stageReport, ev.err)
	case evProjectAdd:
		a.applyProjectAdd(ev.path, ev.text, ev.writeBad)
	case evProjectRemove:
		a.applyProjectRemove(ev.text, ev.writeBad)
	}
}

func (a *App) applySnapshot(result *snapshotResult) {
	if result.ok {
		a.online = true
		a.setSurfaces(result.state.Surfaces)
		a.stamp++
	} else {
		a.online = false
	}
	if result.tasksOK {
		a.tasks = result.tasks
		a.stamp++
	}
	if result.pendingOK {
		a.pending = result.pending
		a.stamp++
	}
	if result.queued != nil {
		a.queued = result.queued
		a.stamp++
	}
	if result.running != nil {
		a.runningConvs = result.running
		a.stamp++
	}

	// Banners read the whole snapshot, so they come after every part of it is
	// in: a chat whose turn ended is only knowable against both the task list
	// and the surfaces that name it.
	for _, al := range a.notes.observe(a.surfaceSnapshot(), a.tasks, a.pending, a.watchingChat) {
		if !a.delivery.spend(al, a.delivery.at()) {
			continue
		}
		a.banner(al)
	}

	if result.ok {
		// The first snapshot names the chats; without this the rail would wait
		// a minute for its times.
		a.probeTails()
	}
	if a.active == 0 && a.online {
		if surfaces := a.surfaceSnapshot(); len(surfaces) > 0 {
			a.selectConversation(surfaces[0].ConversationID)
		}
	}
	if a.active != 0 && !a.wsLive {
		// The socket is not carrying this chat, so the five-second tick is the
		// only source of new entries and it reloads the page.
		a.loadThread(a.active)
	}
}

func (a *App) applyPage(result *pageResult) {
	if result.convID != a.active {
		return
	}
	th := a.thread(result.convID)
	if result.err != nil {
		th.failed = true
		return
	}
	th.failed = false
	th.loaded = true
	a.model = result.model
	a.thinking = result.thinking
	th.usage = result.usage
	th.hasMore = result.hasMore
	th.entries = result.entries
	th.rev = make(map[int]uint64, len(result.entries))
	for _, entry := range result.entries {
		th.rev[entry.ID] = fingerprint(entry)
		a.noteSeen(result.convID, entry.ID)
	}
	th.version++
	// The reader is at the newest page, most likely reading it: the page behind
	// is fetched now rather than the moment they reach for it.
	a.prefetchOlder(result.convID)
}

// applyFrame merges the socket's window into the page already held. It never
// replaces the list: the bridge's window is shorter than the page, so a
// wholesale swap would walk the transcript back.
func (a *App) applyFrame(result *frameResult) {
	if result.convID != a.active {
		return
	}
	wasRunning := a.running()
	defer func() {
		// A turn that just ended may have committed, pushed or merged, and the
		// strip is derived from the repository: re-derive it rather than leave
		// yesterday's answer sitting over today's branch.
		if wasRunning && !a.running() {
			a.loadStages(a.active)
		}
	}()
	th := a.thread(result.convID)
	a.wsLive = true
	if result.frame.Value == nil {
		return
	}
	changed := false
	for _, entry := range result.frame.Value.Entries {
		stamp := fingerprint(entry)
		if prev, seen := th.rev[entry.ID]; seen && prev == stamp {
			continue
		}
		th.rev[entry.ID] = stamp
		th.upsert(entry)
		a.noteSeen(result.convID, entry.ID)
		changed = true
	}
	if result.frame.Value.Docs != nil {
		th.live = result.frame.Value.Docs["pi.live"]
		th.inbox = result.frame.Value.Docs["pi.inbox"].Items
		changed = true
	}
	if changed {
		th.version++
	}
}

func (a *App) applyTail(result *tailResult) {
	if result.at.IsZero() {
		return
	}
	prev := a.vm.tails[result.convID]
	if !prev.IsZero() && result.at.After(prev) && result.convID != a.active {
		// Activity grew in a chat the user is not looking at. The probe is the
		// only witness, so this flag is a heuristic and does not survive a
		// restart — P0 takes that.
		a.vm.unread[result.convID] = true
	}
	if prev.IsZero() || result.at.After(prev) {
		a.vm.tails[result.convID] = result.at
		a.stamp++
	}
}

// remember writes the read mark of a chat to disk, on the way out of it: the map
// is touched on every frame, and a write per frame is a write per keystroke.
func (a *App) remember(convID int) {
	if convID == 0 || a.vm.seen[convID] == 0 {
		return
	}
	key := strconv.Itoa(convID)
	if a.prefs.Seen == nil {
		a.prefs.Seen = map[string]int{}
	}
	if a.vm.seen[convID] <= a.prefs.Seen[key] {
		return
	}
	a.prefs.Seen[key] = a.vm.seen[convID]
	savePrefs(a.prefs)
}

// noteSeen records that the user has had this entry on screen. Only the open
// chat counts: the rest is what the line in the transcript is about.
func (a *App) noteSeen(convID, entry int) {
	if convID != a.active || entry <= a.vm.seen[convID] {
		return
	}
	a.vm.seen[convID] = entry
}

// thread is the store for one chat, created on first use.
func (a *App) thread(convID int) *threadStore {
	if th, ok := a.threads[convID]; ok {
		return th
	}
	th := &threadStore{convID: convID, rev: map[int]uint64{}}
	a.threads[convID] = th
	return th
}

func (th *threadStore) upsert(entry protocol.Entry) {
	i := sort.Search(len(th.entries), func(i int) bool { return th.entries[i].ID >= entry.ID })
	if i < len(th.entries) && th.entries[i].ID == entry.ID {
		th.entries[i] = entry
		return
	}
	th.entries = append(th.entries, protocol.Entry{})
	copy(th.entries[i+1:], th.entries[i:])
	th.entries[i] = entry
}

// selectConversation opens a chat: its page, its live socket, and no unread.
func (a *App) selectConversation(convID int) {
	if convID == 0 || convID == a.active {
		return
	}
	a.remember(a.active)
	// The draft belongs to the chat being left, so it is filed before the
	// window starts pointing at the one being opened.
	a.prefs.setDraft(a.active, a.draft)
	a.active = convID
	a.vm.newFrom[convID] = a.vm.seen[convID]
	a.revealChatSection(convID)
	a.vm.unread[convID] = false
	delete(a.vm.unread, convID)
	a.draft, a.draftImage = a.prefs.draftAt(convID), nil
	savePrefs(a.prefs)
	// The strip belongs to the chat on screen, so it is re-derived on every switch
	// rather than carried over: a stale "blocked" would stop work that is fine.
	a.stageStrip = nil
	a.loadStages(convID)
	a.histIndex = len(a.history)
	a.vm.threadList.FollowEnd = true
	a.focusComposer = true
	a.stamp++
	if a.watchConv != convID {
		a.watch(convID)
	}
	if th, ok := a.threads[convID]; !ok || !th.loaded {
		a.loadThread(convID)
	}
	// The memory drawer's chat scope belongs to the chat on screen, so opening
	// another one makes that tab stale.
	if a.mem.open {
		a.mem.notes[2] = nil
		a.loadMemory()
	}
	a.invalidate()
}

// activeConv is the chat on screen.
func (a *App) activeConv() (protocol.Surface, bool) {
	for _, s := range a.surfaceSnapshot() {
		if s.ConversationID == a.active {
			return s, true
		}
	}
	return protocol.Surface{}, false
}

// surfaceOf is the chat row for a conversation, so a link can check the chat it
// points at still exists before offering to open it.
func (a *App) surfaceOf(convID int) *protocol.Surface {
	for _, s := range a.surfaceSnapshot() {
		if s.ConversationID == convID {
			found := s
			return &found
		}
	}
	return nil
}

// running reports whether the active chat has a turn in flight.
func (a *App) running() bool {
	th, ok := a.threads[a.active]
	if !ok {
		return false
	}
	run := th.live.Run
	return len(run) > 0 && string(run) != "null"
}

// runSoFar is how long the turn on screen has been going, taken from the task
// it runs on: the harness stamps startedAt when it takes the work, so this
// survives opening the chat mid-turn and reloading the window.
func (a *App) runSoFar() (time.Duration, bool) {
	th, ok := a.threads[a.active]
	if !ok || len(th.live.Run) == 0 {
		return 0, false
	}
	var run struct {
		TaskID int `json:"taskId"`
	}
	if json.Unmarshal(th.live.Run, &run) != nil || run.TaskID == 0 {
		return 0, false
	}
	for _, task := range a.tasks {
		if task.ID == run.TaskID && task.StartedAt > 0 {
			return a.now.Sub(unixTime(task.StartedAt)), true
		}
	}
	return 0, false
}

// View is the frame: apply what arrived, then draw.
// minWindowWidth matches the window's own minimum: the chat card must keep a
// reading measure even when the drawer is open on a small display.
const minWindowWidth = 720

// windowSizer is the part of a window this file needs, so the growth can be
// tested without an app running.
type windowSizer interface {
	Size() (int, int)
	SetSize(width, height int)
	Bounds() mygo.Rectangle
	IsMaximized() bool
}

// callerWindow and workArea are variables so the tester, which has no window,
// can stand in for one.
var callerWindow = func(ctx context.Context) windowSizer {
	w := mygo.CallerWindow(ctx)
	if w == nil {
		return nil
	}
	return w
}

// workArea is the screen's right edge in screen coordinates, or 0 when it cannot
// be read: the window grows to the right and has to stop there.
var workArea = func(bounds mygo.Rectangle) float32 {
	if mygo.Screen == nil {
		return 0
	}
	area := mygo.Screen.DisplayMatching(bounds).WorkArea
	return float32(area.X + area.Width)
}

// drawerSpan is how much width the open drawer claims, gap included.
func (a *App) drawerSpan() float32 {
	switch {
	case a.changes.open:
		return changesWidth + paneInset
	case a.tl.open:
		return timelineWidth + paneInset
	case a.mem.open:
		return memWidth + paneInset
	case a.projects.open:
		return memWidth + paneInset
	}
	return 0
}

// fitWindowToDrawer widens the window by the drawer it just opened, and gives the
// width back when it closes. A drawer that eats the chat's column makes the chat
// narrower the moment you ask for a diff, which is the opposite of helpful: the
// window grows outward instead, and the reading column keeps its measure.
func (a *App) fitWindowToDrawer() {
	want := a.drawerSpan()
	if want == a.windowGrew {
		return
	}
	w := callerWindow(a.ctx)
	if w == nil {
		a.windowGrew = want
		return
	}
	// A zoomed window is at the display's edge by definition, and resizing it
	// would undo the zoom: the drawer floats instead.
	if w.IsMaximized() {
		a.windowGrew = 0
		a.drawerOverlay = want > 0
		return
	}
	width, height := w.Size()
	next := width + int(want-a.windowGrew)
	if next < minWindowWidth {
		next = minWindowWidth
	}
	// The window grows to the right, so it must stop at the display's edge rather
	// than sliding under it.
	if bounds := w.Bounds(); workArea != nil {
		if edge := workArea(bounds); edge > 0 {
			if limit := int(edge-float32(bounds.X)) - 24; next > limit {
				next = limit
			}
		}
	}
	if next != width {
		w.SetSize(next, height)
	}
	grew := float32(next - width)
	a.windowGrew = grew
	// Short of the room the drawer wants, the chat would pay for it: float the
	// sheet above instead.
	a.drawerOverlay = want > grew+0.5
}

func (a *App) View(c *ui.Context) {
	a.fitWindowToDrawer()
	// The menus the rail opens — the row's ⋯ and the others — are drawn by the
	// window rather than by the system, so their items take the theme's type and
	// colors instead of macOS's.
	c.SetMenuPresentation(ui.PopoverMenus)
	a.drain()
	a.now = time.Now()
	a.dark = a.prefs.dark(c.Theme().Dark)
	t := theme.Use(c, a.dark)
	// ⌘K is the way in: the window has more verbs than fit on a rail, and a
	// reader who knows the name of a chat should not have to hunt for it.
	c.OnWindowShortcut(ui.Super, ui.KeyK, a.openPalette)
	c.OnWindowShortcut(ui.Super, ui.KeyP, a.openFiles)
	c.OnWindowShortcut(ui.Super|ui.Shift, ui.KeyE, a.exportMarkdown)
	c.OnWindowShortcut(ui.Super, ui.KeyJ, a.scrollToLatest)
	c.OnWindowShortcut(ui.Super, ui.KeyF, a.ToggleFind)
	// One key per drawer, on shift so they cannot be typed into a message by
	// accident: the drawers are places to go, not characters.
	c.OnWindowShortcut(ui.Super|ui.Shift, ui.KeyM, a.toggleMemory)
	c.OnWindowShortcut(ui.Super|ui.Shift, ui.KeyC, a.toggleChanges)
	c.OnWindowShortcut(ui.Super|ui.Shift, ui.KeyT, a.toggleTimeline)
	c.OnWindowShortcut(ui.Super, ui.KeyBackslash, a.toggleRailCollapsed)
	c.OnWindowShortcut(ui.Super|ui.Shift, ui.KeySlash, a.toggleCheatsheet)
	a.shell(c, t)
	a.paletteDialog(c, t)
	a.modelDialog(c, t)
	a.cheatsheetDialog(c, t)
	a.renameDialog(c, t)
	a.newChatDialog(c, t)
	a.settingsDialog(c, t)
	a.taskDetailDialog(c, t)
	a.zoomDialog(c, t)
	a.usageDialog(c, t)
	a.writeDialog(c, t)
}

// fingerprint of an entry, for deciding whether a frame carried a new version.
func fingerprint(entry protocol.Entry) uint64 {
	h := fnv.New64a()
	h.Write([]byte(entry.Kind))
	h.Write(entry.Model)
	h.Write(entry.Data)
	return h.Sum64()
}

func newestTime(entries []protocol.Entry) time.Time {
	var newest time.Time
	for _, entry := range entries {
		for _, msg := range messagesOf(entry) {
			if at := unixTime(msg.Timestamp); at.After(newest) {
				newest = at
			}
		}
	}
	return newest
}

func messagesOf(entry protocol.Entry) []modelMessage {
	if len(entry.Model) == 0 {
		return nil
	}
	var messages []modelMessage
	if err := json.Unmarshal(entry.Model, &messages); err != nil {
		return nil
	}
	return messages
}

func attachmentImages(images []imageRef) []api.Image {
	out := make([]api.Image, 0, len(images))
	for _, img := range images {
		out = append(out, api.Image{
			Data:     base64.StdEncoding.EncodeToString(img.data),
			MimeType: img.mime,
		})
	}
	return out
}

// NewChat, AttachImages and RestartAgent are the menu's verbs; they run the

// prefetchPages is how far ahead of the reader a chat is read: three pages of
// 120 is a few screens of scrolling, without fetching a year of history nobody
// scrolls to.
const prefetchPages = 2

// olderResult is one page read backwards from the oldest entry on screen.
type olderResult struct {
	convID   int
	entries  []protocol.Entry
	hasMore  bool
	prefetch bool
	err      error
}

// loadOlder reads the page before the oldest entry on screen. The bridge pages
// backwards by entry id, so a chat longer than one page is read whole by
// walking up rather than by asking for everything at once.
func (a *App) loadOlder(convID int) {
	a.readOlder(convID, false)
}

// prefetchOlder reads the page behind the newest one once per chat, while the
// reader is still at the bottom. It draws nothing and waits for nobody; its
// only job is that walking up does not pause.
func (a *App) prefetchOlder(convID int) {
	th := a.threads[convID]
	if th == nil || th.prefetched >= prefetchPages || !th.hasMore || th.loadingOlder {
		return
	}
	th.prefetched++
	a.readOlder(convID, true)
}

func (a *App) readOlder(convID int, prefetch bool) {
	th := a.threads[convID]
	if th == nil || !th.hasMore || th.loadingOlder || len(th.entries) == 0 {
		return
	}
	oldest := th.entries[0].ID
	for _, entry := range th.entries {
		if entry.ID < oldest {
			oldest = entry.ID
		}
	}
	th.loadingOlder = true
	th.prefetching = prefetch
	if !prefetch {
		a.invalidate()
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		page, err := a.api.ConversationBefore(ctx, convID, oldest)
		result := &olderResult{convID: convID, err: err, prefetch: prefetch}
		if err == nil {
			result.entries = page.Entries
			result.hasMore = page.HasMore
		}
		a.post(event{kind: evOlder, older: result})
	}()
}

// applyOlder prepends the page read backwards, keeping the entries already on
// screen: a page overlaps nothing by the bridge's contract, but the check is
// cheap and a duplicate would double a message.
func (a *App) applyOlder(result *olderResult) {
	if result == nil {
		return
	}
	th := a.threads[result.convID]
	if th == nil {
		return
	}
	th.loadingOlder = false
	th.prefetching = false
	if result.err != nil {
		// A prefetch that fails is not news: the reader has not asked for that
		// page yet, and walking up will try again.
		if !result.prefetch {
			a.notice = i18n.T("loadOlderFailed") + " · " + result.err.Error()
			a.noticeOK = false
		}
		a.invalidate()
		return
	}
	seen := make(map[int]bool, len(th.entries))
	for _, entry := range th.entries {
		seen[entry.ID] = true
	}
	for _, entry := range result.entries {
		if seen[entry.ID] {
			continue
		}
		seen[entry.ID] = true
		th.entries = append(th.entries, entry)
	}
	th.hasMore = result.hasMore
	th.version++
	a.invalidate()
	// One page ahead is not enough for a chat read over a while: the next one
	// is already on its way by the time the reader reaches this page's top.
	if result.prefetch && th.hasMore {
		a.prefetchOlder(result.convID)
	}
}
