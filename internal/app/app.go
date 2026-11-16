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
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"pitta/internal/api"
	"pitta/internal/avatar"
	"pitta/internal/diff"
	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// Config is what the window needs to reach the agent.
type Config struct {
	BaseURL string
	Token   string
	Home    string
}

// SideTab is one panel of the right-hand column, as store.ts's tab field names
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

	// currentID mirrors current for goroutines: with a window, currentChanges
	// only on the UI thread, so a background read of it would race.
	currentID atomic.Int64

	// Layout and widget state: UI thread only, never read from a goroutine.
	railWidth     float32
	railListState ui.ListState
	prefs         prefs
	md            markdown.Options
	steer         bool
	turnWindow    int

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
	merge            *protocol.MergeResult
	changesLoading   bool
	pushNote         string

	menu     chatMenu
	findOpen bool

	// focused is false while the window is behind another one; nothing is
	// notified while the reader is looking at the answer.
	focused    bool
	notifiedID string

	// errsOpen shows the panel behind the corner error badge.
	errsOpen bool

	// pendingImages are the pictures the next turn carries, and imagePath is
	// what the path dialog holds while it is open.
	pendingImages []imagePart
	imagePath     string
	imageOpen     bool
	previewImage  int
	previewSet    []imagePart

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
	modelOpen    bool
	stepOpen     map[int]bool
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
	mood      avatar.Mood
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
	return &App{
		cfg:          cfg,
		api:          api.New(cfg.BaseURL, cfg.Token),
		railWidth:    width,
		sideWidth:    sideWidth,
		sideTab:      SideChanges,
		prefs:        p,
		focused:      true,
		stepOpen:     map[int]bool{},
		previewImage: -1,
		turnWindow:   60,
		commitDiffs:  map[string]string{},
		modelsFav:    p.Favorites,
		modelsRecent: p.Recents,
	}
}

// Attach takes the window and starts the first load.
func (a *App) Attach(win *mygo.Window) {
	a.win = win
	// A finished answer should reach the owner when the window is behind
	// another one, as the React client notifies only while hidden.
	a.focused = win.IsFocused()
	win.OnFocus(func() { a.apply(func() { a.focused = true }) })
	win.OnBlur(func() { a.apply(func() { a.focused = false }) })
	go a.init()
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
	a.openDefaultConversation()
	go a.pollApprovals()
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
// the React client does on boot.
func (a *App) openDefaultConversation() {
	picked := 0
	a.applyWait(func() {
		if id := defaultConversationID(a.surfaces); id != 0 {
			a.setCurrent(id)
			picked = id
		}
	})
	a.reloadConversation(picked)
}

// defaultConversationID is the newest conversation that is not archived, which
// is the chat the React client reopens.
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
		a.surfaces = st.Surfaces
		a.tasks = st.Tasks
		a.projects = st.Projects
		a.feishu = st.Feishu
	})
	return nil
}

// reloadConversation loads the open conversation and (re)subscribes its socket.
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
		a.model = conv.Model
		a.thinking = conv.ThinkingLevel
		if conv.Usage != nil {
			a.usage = *conv.Usage
		}
		a.waits = conv.ApprovalWaits
		a.live = nil
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
	ctx := context.Background()
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
			a.entries = value.Entries
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
		if a.current == conversationID {
			return
		}
		a.setCurrent(conversationID)
		a.entries = nil
		a.live = nil
		a.approvals = nil
		a.invalidate()
	})
	a.reloadConversation(a.currentValue())
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
	// ConvSearch.tsx:16 binds meta/ctrl+F window-wide; OnWindowShortcut fires
	// with no element focused, which is the same reach.
	// ui.Cmd is Cmd on darwin and Ctrl elsewhere; a combined Super|Ctrl mask
	// matches neither, since modifiers are matched exactly.
	c.OnWindowShortcut(ui.Cmd, ui.KeyF, func() {
		a.apply(func() { a.findOpen = true; a.invalidate() })
	})
	ui.Split(c, &a.railWidth, func() { a.rail(c) }, func() { a.pane(c) }).Fill()
	a.chatMenuDialog(c, t)
	a.taskDialog(c)
	a.imageDialog(c, t)
	a.previewDialog(c)
	a.errorPanel(c, t)
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
func (a *App) toggleTheme() {
	a.apply(func() {
		if a.prefs.Theme == "dark" {
			a.prefs.Theme = "light"
		} else {
			a.prefs.Theme = "dark"
		}
		a.savePrefsLater()
		a.invalidate()
	})
}

// toggleLang switches between Chinese and English, as the rail's Languages
// button does.
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
		return
	}
	// A row with a growing chat and a fixed-width panel, not a nested Split:
	// inside Split's first pane a percentage width has no base to resolve
	// against, which collapsed the composer to its content.
	ui.Row(c).Fill().Children(func() {
		ui.Row(c).Grow(1).Children(func() { a.main(c) })
		ui.Column(c).Width(a.sideWidthOf()).FillHeight().Children(func() { a.sidePanel(c) })
	})
}

func (a *App) main(c *ui.Context) {
	if a.view == ViewFile {
		// The file viewer takes the chat's place, as React swaps the transcript
		// for FileViewer instead of opening a window.
		a.fileView(c)
		return
	}
	ui.Column(c).Fill().Children(func() {
		a.mainHeader(c)
		ui.Divider(c)
		a.transcript(c)
		if a.modelOpen {
			a.modelPalette(c)
		}
		a.composer(c)
	})
}

// mainHeader names the chat and holds the actions that act on it, as App.tsx
// puts reset and stop above the transcript.
func (a *App) mainHeader(c *ui.Context) {
	t := theme.Current(c)
	ui.Row(c).Padding(8, 14).Gap(10).Children(func() {
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, a.currentTitle()).FontSize(12).TextColor(t.Foreground)
			ui.Text(c, a.currentMeta()).FontSize(9).TextColor(t.MutedFg)
		})
		if a.findOpen {
			a.findBar(c, t)
		}
		ui.Row(c).Grow(1).Children(func() {})
		iconButton(c, icons.Search).OnClick(func() {
			a.apply(func() { a.findOpen = !a.findOpen; a.invalidate() })
		})
		if a.current != 0 {
			iconButton(c, icons.MessagePlus).OnClick(func() { a.openChatMenu() })
		}
		iconButton(c, icons.RotateCw).OnClick(func() { a.resetConversation() })
		iconButton(c, icons.PanelRight).OnClick(func() { a.toggleSide() })
		stop := iconButton(c, icons.Square)
		if a.busy || a.live != nil {
			stop.OnClick(func() { a.abort() })
		}
	})
}

// iconButton is a borderless button holding one icon, as the React header's
// ghost icon buttons are.
func iconButton(c *ui.Context, icon *ui.SVG) ui.Element {
	return ui.Button(c, "").Padding(5, 7).Children(func() {
		ui.Icon(c, icon).Size(12, 12)
	})
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
