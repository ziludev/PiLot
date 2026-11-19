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

	"pitta/internal/api"
	"pitta/internal/protocol"
	"pitta/internal/theme"
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
	online bool
	// stamp changes whenever the rail's inputs change, so railRows can be cached.
	stamp uint64

	active  int
	threads map[int]*threadStore

	// prefs is what the window keeps between launches: language and theme, which
	// have no dialog of their own.
	prefs prefs
	// dark is the theme the last frame drew with, so a toggle flips what is on
	// screen rather than what macOS says.
	dark bool
	// railW is the rail's width now, which the saved preference seeds and the
	// edge drags; dragging is true while the pointer holds it.
	railW    float32
	dragging bool

	// watch is the live subscription for the active chat.
	watchCancel context.CancelFunc
	watchConv   int
	wsLive      bool
	refreshBusy atomic.Bool
	tailBusy    atomic.Bool

	rail   railSection
	filter string
	now    time.Time
	vm     viewModel
	// focusComposer asks the next frame to put the cursor in the composer.
	focusComposer bool

	// composer
	draft      string
	draftImage []imageRef
	history    []string
	histIndex  int
	notice     string
	busy       map[string]bool // call id → decision in flight

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
}

type viewModel struct {
	// tails is the newest entry time per chat, as probed from the bridge.
	tails  map[int]time.Time
	unread map[int]bool
	// folds is the disclosure state of folded step rows, keyed by
	// (conversationID, turnID).
	folds      map[string]bool
	projection map[int]projectionCache
	railCache  []railRow
	railStamp  uint64
	railFilter string

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
}

// New builds the window state and starts the work that feeds it: a five-second
// reconcile and a staggered tail probe for the rail's relative times.
func New(cfg Config) *App {
	a := newApp(cfg)
	// The saved language and theme are the window's own, read once at startup so
	// the frames a test draws by hand stay independent of the owner's settings.
	a.prefs = loadPrefs()
	a.prefs.applyLang()
	a.railW = a.prefs.railWidth()
	go a.reconcileLoop()
	go a.elapsedLoop()
	go a.tailLoop()
	return a
}

// newApp is New without the goroutines, for the frames a test draws by hand.
func newApp(cfg Config) *App {
	ctx, cancel := context.WithCancel(context.Background())
	return &App{
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
			folds:      map[string]bool{},
			projection: map[int]projectionCache{},
		},
	}
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
	// The changes drawer: the worktree patch, the branch, one commit's patch.
	evChanges
	evGit
	evCommitDiff
)

type event struct {
	kind eventKind

	snapshot *snapshotResult
	page     *pageResult
	frame    *frameResult
	tail     *tailResult
	notice   string
	text     string
	convID   int
	callID   string
	images   []imageRef
	// A menu choice carries where the chat was going; a search reply carries
	// the hits, the query they answer, and the generation it belongs to.
	archived bool
	seq      int
	hits     []protocol.SearchHit
	undo     *surfaceUndo
	models   *protocol.Models
	memory   *protocol.Memory
	curation *protocol.Curation
	applied  *protocol.MemoryApplied
	changes  *protocol.Changes
	git      *protocol.Git
	hash     string
	diff     string
	err      error
}

type snapshotResult struct {
	state     *protocol.State
	tasks     []protocol.Task
	pending   []protocol.PendingApproval
	queued    map[int]int
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
	case evCommitDiff:
		a.applyCommitDiff(ev.hash, ev.diff, ev.err)
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
	th.entries = result.entries
	th.rev = make(map[int]uint64, len(result.entries))
	for _, entry := range result.entries {
		th.rev[entry.ID] = fingerprint(entry)
	}
	th.version++
}

// applyFrame merges the socket's window into the page already held. It never
// replaces the list: the bridge's window is shorter than the page, so a
// wholesale swap would walk the transcript back.
func (a *App) applyFrame(result *frameResult) {
	if result.convID != a.active {
		return
	}
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
	a.active = convID
	a.revealChatSection(convID)
	a.vm.unread[convID] = false
	delete(a.vm.unread, convID)
	a.draft, a.draftImage = "", nil
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
func (a *App) View(c *ui.Context) {
	// The menus the rail opens — the row's ⋯ and the others — are drawn by the
	// window rather than by the system, so their items take the theme's type and
	// colors instead of macOS's.
	c.SetMenuPresentation(ui.PopoverMenus)
	a.drain()
	a.now = time.Now()
	a.dark = a.prefs.dark(c.Theme().Dark)
	t := theme.Use(c, a.dark)
	a.shell(c, t)
	a.renameDialog(c, t)
	a.settingsDialog(c, t)
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
