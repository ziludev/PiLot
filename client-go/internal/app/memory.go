package app

// The memory drawer: what the agent remembers about Alex, in the three scopes
// the bridge keeps, plus the curation pass that proposes new lines from the
// last day of transcripts.
//
// It is a drawer and not a rail tab because notes are sentences. A 260-point
// rail wraps them into noise, and the pass that turns a day of work into a
// handful of memory lines needs room to lay out the lines it wants to keep.

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/icons"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

const (
	// memWidth is the drawer's width: enough for a wrapped sentence at 11.5pt.
	memWidth = 340
	// memHours is the window curation reads. One day is cheap enough to run on a
	// whim and is about what just happened.
	memHours = 24
	// memNoteLines caps a note: the memory files cap lines too, but a note that
	// was written before a cap changed should not push the rest off screen.
	memNoteLines = 6
)

// memoryPanel is the drawer's state. notes hold the last read of each scope;
// suggesting is set once a pass has answered, and the drawer then shows the
// proposal instead of the notes until the user saves or cancels it.
type memoryPanel struct {
	open     bool
	tab      int
	chatID   int
	notes    [3][]protocol.Note
	list     ui.ListState
	loading  bool
	failed   string
	curating bool
	saving   bool

	suggesting bool
	sugg       []protocol.MemorySuggestion
	picked     []bool
	scanned    int
	tokens     int
	err        string
	saved      string
}

// memTabKeys are the drawer's tabs in the order the bridge scopes them.
var memTabKeys = [3]string{"memGlobal", "memWork", "memChat"}

// toggleMemory opens and closes the drawer. Opening reads the notes: the files
// change while the window is up (the agent writes them mid-turn), so a stale
// copy from last time would be a small lie.
func (a *App) toggleMemory() {
	a.mem.open = !a.mem.open
	if a.mem.open {
		// One right edge, one drawer.
		a.changes.open = false
		a.projects.open = false
		a.tl.open = false
		a.mem.saved = ""
		a.loadMemory()
	}
}

// loadMemory reads the three lists for the open chat. The chat list belongs to
// one conversation, so the request carries the id and the reply is dropped if
// the user moved on before it landed.
func (a *App) loadMemory() {
	if a.mem.loading {
		return
	}
	conv := a.active
	a.mem.chatID = conv
	a.mem.loading = true
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 10*time.Second)
		defer cancel()
		memory, err := a.api.Memory(ctx, conv)
		a.post(event{kind: evMemory, convID: conv, memory: memory, err: err})
	}()
}

func (a *App) applyMemory(convID int, memory *protocol.Memory, err error) {
	a.mem.loading = false
	if err != nil {
		a.mem.failed = err.Error()
		return
	}
	a.mem.failed = ""
	a.mem.notes[0], a.mem.notes[1] = memory.Global, memory.Work
	if convID == a.active && convID == a.mem.chatID {
		a.mem.notes[2] = memory.Chat
	}
}

// curateMemory asks the bridge to read the last day and propose lines. It is a
// model call, so the drawer says it is working and the reply is allowed to
// arrive with its own failure in it.
func (a *App) curateMemory() {
	if a.mem.curating {
		return
	}
	a.mem.curating = true
	a.mem.saved = ""
	a.mem.err = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 3*time.Minute)
		defer cancel()
		curation, err := a.api.CurateMemory(ctx, memHours)
		a.post(event{kind: evCurate, curation: curation, err: err})
	}()
}

// applyCuration shows the proposal. Every line starts kept: the pass is the
// filter, and unchecking the few that are wrong beats ticking the many that are
// right.
func (a *App) applyCuration(curation *protocol.Curation, err error) {
	a.mem.curating = false
	a.mem.suggesting = true
	a.mem.sugg, a.mem.picked = nil, nil
	a.mem.err = ""
	if err != nil {
		a.mem.err = err.Error()
		return
	}
	a.mem.sugg = curation.Suggestions
	a.mem.picked = make([]bool, len(curation.Suggestions))
	for i := range a.mem.picked {
		a.mem.picked[i] = true
	}
	a.mem.scanned, a.mem.tokens = curation.Scanned, curation.Tokens
	if curation.Error != "" {
		a.mem.err = curation.Error
	}
}

// saveMemory writes the lines still ticked, then rereads the notes so the drawer
// shows them where they landed.
func (a *App) saveMemory() {
	if a.mem.saving {
		return
	}
	kept := a.keptLines()
	if len(kept) == 0 {
		a.mem.suggesting = false
		a.mem.sugg, a.mem.picked = nil, nil
		return
	}
	a.mem.saving = true
	a.mem.err = ""
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		applied, err := a.api.ApplyMemory(ctx, kept)
		a.post(event{kind: evMemorySaved, applied: applied, err: err})
	}()
}

// keptLines is the proposal minus what the user unchecked, in the order the pass
// proposed them.
func (a *App) keptLines() []protocol.MemorySuggestion {
	var kept []protocol.MemorySuggestion
	for i, line := range a.mem.sugg {
		if i < len(a.mem.picked) && a.mem.picked[i] {
			kept = append(kept, line)
		}
	}
	return kept
}

func (a *App) applyMemorySaved(applied *protocol.MemoryApplied, err error) {
	a.mem.saving = false
	if err != nil {
		a.mem.err = err.Error()
		return
	}
	a.mem.suggesting = false
	a.mem.sugg, a.mem.picked = nil, nil
	a.mem.saved = i18n.TF("memSaved", map[string]any{"n": applied.Added})
	a.loadMemory()
}

// closeSuggestions goes back to the notes without writing anything.
func (a *App) closeSuggestions() {
	if a.mem.saving {
		return
	}
	a.mem.suggesting = false
	a.mem.sugg, a.mem.picked = nil, nil
	a.mem.err = ""
}

// memoryView is the drawer itself.
func (a *App) memoryView(c *ui.Context, t theme.Tokens) {
	ui.Column(c).Width(memWidth).Shrink(1).MinWidth(260).FillHeight().
		Radius(theme.RadiusWindow).Border(1, t.Line).Background(t.Surface).Clip().
		Children(func() {
			a.memoryHeader(c, t)
			ui.Box(c).Width(memWidth).Height(1).Background(t.Line)
			if a.mem.suggesting {
				a.memoryProposal(c, t)
			} else {
				a.memoryTabs(c, t)
				a.memoryNotes(c, t)
			}
			a.memoryFooter(c, t)
		})
}

func (a *App) memoryHeader(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Height(48).Padding(0, 14).AlignItems(ui.Center).Gap(8).Children(func() {
		title := i18n.T("memory")
		if a.mem.suggesting {
			title = i18n.T("memSuggest")
		}
		ui.Text(c, title).FontSize(theme.TextTitle).FontWeight(500).TextColor(t.Ink).MaxLines(1)
		ui.Spacer(c)
		if a.mem.loading && !a.mem.suggesting {
			ui.Icon(c, icons.Clock).Size(10, 10).TextColor(t.MutedFg)
		}
		close := ui.ButtonBase(c).Width(20).Height(20).Radius(theme.RadiusChip).Cursor(ui.CursorPointer)
		close.Children(func() {
			ui.Text(c, "×").FontSize(theme.TextTitle).TextColor(t.MutedFg)
		})
		close.OnClick(func() { a.mem.open = false })
	})
}

// memoryTabs switches scope. Three equal segments, like the rail's switch
// between chats and tasks, because these are peers too.
func (a *App) memoryTabs(c *ui.Context, t theme.Tokens) {
	ui.Row(c).FillWidth().Padding(0, 12, 8, 12).Gap(2).Children(func() {
		for index, key := range memTabKeys {
			at := index
			on := a.mem.tab == index
			tab := ui.ButtonBase(c).Grow(1).Height(24).Radius(theme.RadiusChip).Cursor(ui.CursorPointer)
			if on {
				tab.Background(t.Background)
			}
			tab.Children(func() {
				color := t.MutedFg
				if on {
					color = t.Foreground
				}
				ui.Text(c, i18n.T(key)).FontSize(theme.TextSmall).FontWeight(500).TextColor(color).
					MaxLines(1).Width(memWidth/3 - 16 - 2).TextAlign(ui.Center)
			})
			tab.OnClick(func() {
				a.mem.tab = at
				a.mem.list = ui.ListState{}
			})
		}
	})
}

func (a *App) memoryNotes(c *ui.Context, t theme.Tokens) {
	if a.mem.failed != "" {
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, a.mem.failed).FontSize(theme.TextSmall).TextColor(t.Destructive).MaxLines(4)
		})
		return
	}
	notes := a.mem.notes[a.mem.tab]
	if len(notes) == 0 {
		ui.Column(c).FillWidth().Padding(12, 12).Children(func() {
			ui.Text(c, i18n.T("memEmpty")).FontSize(theme.TextSmall).TextColor(t.MutedFg).MaxLines(3)
		})
		return
	}
	ui.Column(c).FillWidth().Padding(0, 12).Grow(1).Children(func() {
		ui.List(c, &a.mem.list, len(notes), func(row int) {
			a.memoryNote(c, t, notes[row])
		}).Grow(1).Width(memWidth - 24)
	})
}

func (a *App) memoryNote(c *ui.Context, t theme.Tokens, note protocol.Note) {
	ui.Column(c).FillWidth().Gap(3).Padding(7, 0).Children(func() {
		ui.Text(c, note.Text).FontSize(theme.TextSmall).TextColor(t.Foreground).MaxLines(memNoteLines)
		ui.Row(c).FillWidth().Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Text(c, memWhen(note.At)).FontSize(theme.TextMicro).TextColor(t.MutedFg).MaxLines(1)
			a.memoryReceipt(c, t, note.Source)
		})
	})
}

// memoryReceipt says where a line came from. The first thing a wrong note makes
// you ask is "where did you get that", and the answer should be a place you can
// open, not another guess — so a line the agent wrote while talking in a chat
// links back to that chat. The chat you are in needs no receipt, and a note older
// than receipts says nothing rather than inventing one.
func (a *App) memoryReceipt(c *ui.Context, t theme.Tokens, source string) {
	label, conv := memSource(source)
	if label == "" || (conv != 0 && conv == a.active) {
		return
	}
	openable := conv != 0 && a.surfaceOf(conv) != nil
	if !openable {
		ui.Text(c, "· "+label).FontSize(theme.TextMicro).TextColor(t.MutedFg).MaxLines(1)
		return
	}
	btn := ui.ButtonBase(c).Height(16).Radius(theme.RadiusChip).Padding(0, 3).Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Text(c, "· "+label).FontSize(theme.TextMicro).TextColor(t.MutedFg).MaxLines(1)
	})
	btn.OnClick(func() {
		a.mem.open = false
		a.selectConversation(conv)
	})
}

// memSource turns a receipt into words: the chat it was said in, the pass that
// wrote it down, or the memo typed in Feishu. The conversation comes back too so
// the view can offer to open it; "" means this note predates receipts.
func memSource(source string) (string, int) {
	switch {
	case strings.HasPrefix(source, "chat:"):
		id, err := strconv.Atoi(strings.TrimPrefix(source, "chat:"))
		if err != nil || id <= 0 {
			return "", 0
		}
		return i18n.TF("memFromChat", map[string]any{"n": id}), id
	case source == "curator":
		return i18n.T("memFromPass"), 0
	case source == "feishu":
		return i18n.T("memFromFeishu"), 0
	}
	return "", 0
}

// memoryProposal is the pass's answer: each line with a box to keep it, and the
// scope it would land in, because where a line goes decides whether it comes
// back into every conversation.
func (a *App) memoryProposal(c *ui.Context, t theme.Tokens) {
	if len(a.mem.sugg) == 0 {
		ui.Column(c).FillWidth().Padding(12, 12).Gap(6).Children(func() {
			text := i18n.T("memNothing")
			if a.mem.err != "" {
				text = i18n.TF("memFailed", map[string]any{"err": oneLine(a.mem.err)})
			}
			ui.Text(c, text).FontSize(theme.TextSmall).TextColor(t.MutedFg).MaxLines(4)
		})
		return
	}
	ui.Column(c).FillWidth().Padding(0, 12).Grow(1).Children(func() {
		ui.List(c, &a.mem.list, len(a.mem.sugg), func(row int) {
			a.memoryPick(c, t, row)
		}).Grow(1).Width(memWidth - 24)
	})
}

func (a *App) memoryPick(c *ui.Context, t theme.Tokens, row int) {
	line := a.mem.sugg[row]
	ui.Row(c).FillWidth().Gap(8).Padding(7, 0).AlignItems(ui.Start).Children(func() {
		box := ui.ButtonBase(c).Width(15).Height(15).Radius(theme.RadiusChip).Border(1, t.Border).
			Cursor(ui.CursorPointer)
		if row < len(a.mem.picked) && a.mem.picked[row] {
			box.Background(t.Primary)
		}
		box.Children(func() {
			if row < len(a.mem.picked) && a.mem.picked[row] {
				ui.Text(c, "✓").FontSize(theme.TextMicro).TextColor(t.PrimaryFg)
			}
		})
		box.OnClick(func() {
			if row < len(a.mem.picked) {
				a.mem.picked[row] = !a.mem.picked[row]
			}
		})
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, line.Text).FontSize(theme.TextSmall).TextColor(t.Foreground).MaxLines(memNoteLines)
			ui.Text(c, memScope(line.Scope)).FontSize(theme.TextMicro).TextColor(t.MutedFg).MaxLines(1)
		})
	})
}

// memoryFooter is the drawer's one action: curate, then save or cancel once a
// proposal is on screen.
func (a *App) memoryFooter(c *ui.Context, t theme.Tokens) {
	ui.Column(c).FillWidth().Padding(8, 12, 10, 12).Gap(6).Children(func() {
		if note := a.memoryStatus(); note != "" {
			ui.Text(c, note).FontSize(theme.TextMicro).TextColor(t.MutedFg).MaxLines(2)
		}
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			if a.mem.suggesting {
				a.memoryButton(c, t, i18n.T("cancel"), t.Secondary, t.Foreground, func() {
					a.closeSuggestions()
				})
				kept := 0
				for _, on := range a.mem.picked {
					if on {
						kept++
					}
				}
				label := i18n.TF("memKeep", map[string]any{"n": kept})
				if a.mem.saving {
					label = i18n.T("memSaving")
				}
				a.memoryButton(c, t, label, t.Primary, t.PrimaryFg, func() { a.saveMemory() })
				return
			}
			label := i18n.TF("memCurate", map[string]any{"h": memHours})
			if a.mem.curating {
				label = i18n.T("memCurating")
			}
			a.memoryButton(c, t, label, t.Secondary, t.Foreground, func() { a.curateMemory() })
		})
	})
}

func (a *App) memoryButton(c *ui.Context, t theme.Tokens, label string, bg, fg ui.Color, onClick func()) {
	btn := ui.ButtonBase(c).Grow(1).Height(26).Radius(theme.RadiusChip).Background(bg).Cursor(ui.CursorPointer)
	btn.Children(func() {
		ui.Text(c, label).FontSize(theme.TextSmall).FontWeight(500).TextColor(fg).MaxLines(1).
			Width((memWidth - 24 - 6) / 2).TextAlign(ui.Center)
	})
	btn.OnClick(onClick)
}

// memoryStatus is the one line above the footer: what the last pass read, or
// what just landed in the notes.
func (a *App) memoryStatus() string {
	switch {
	case a.mem.curating:
		return i18n.T("memCurating")
	case a.mem.saved != "":
		return a.mem.saved
	case a.mem.suggesting && a.mem.err != "":
		return i18n.TF("memFailed", map[string]any{"err": oneLine(a.mem.err)})
	case a.mem.suggesting && a.mem.scanned > 0:
		return i18n.TF("memScanned", map[string]any{"n": a.mem.scanned, "t": a.mem.tokens})
	}
	return ""
}

// memWhen is a note's age in the rail's language. The bridge stores ISO strings;
// one that does not parse is shown as it came rather than as 1970.
func memWhen(at string) string {
	ts, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return at
	}
	return relTime(time.Now(), ts)
}

// memScope names the file a line lands in.
func memScope(scope string) string {
	switch scope {
	case "work":
		return i18n.T("memWork")
	case "chat":
		return i18n.T("memChat")
	}
	return i18n.T("memGlobal")
}
