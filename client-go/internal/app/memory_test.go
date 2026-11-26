package app

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
	"pilot/internal/protocol"
)

// memoryFixture is the drawer with notes in all three scopes, opened on the
// chat fixtureApp already selected.
func memoryFixture(t *testing.T) *App {
	t.Helper()
	a := fixtureApp(t, false)
	a.mem.open = true
	a.mem.chatID = a.active
	a.mem.notes[0] = []protocol.Note{
		{At: time.Now().Add(-26 * time.Hour).Format(time.RFC3339), Text: "Alex 说中文，结论先行"},
		{At: time.Now().Add(-3 * 24 * time.Hour).Format(time.RFC3339), Text: "别编造链接和数字"},
	}
	a.mem.notes[1] = []protocol.Note{
		{At: time.Now().Add(-2 * time.Hour).Format(time.RFC3339), Text: "代理 7890，阿里云要直连"},
	}
	a.mem.notes[2] = []protocol.Note{
		{At: time.Now().Add(-5 * time.Minute).Format(time.RFC3339), Text: "折叠归用户，展开归打开会话"},
	}
	return a
}

// TestMemoryDrawerShowsEachScope checks the drawer reads as the three scopes,
// one at a time: a tab switch has to change what is on screen, because the
// scopes are the whole point of the panel.
func TestMemoryDrawerShowsEachScope(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := memoryFixture(t)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/memory-notes.png", tt.Image())
	for _, want := range []string{"记忆", "全局", "工作", "本会话", "Alex 说中文，结论先行"} {
		if !tt.HasText(want) {
			t.Fatalf("global tab missing %q; drew %v", want, tt.Texts())
		}
	}
	if tt.HasText("代理 7890，阿里云要直连") {
		t.Fatal("the work scope leaked into the global tab")
	}
	// The note's age is the rail's language, not the ISO string the bridge sent.
	if !tt.HasText("1 天前") {
		t.Fatalf("note age unreadable; drew %v", tt.Texts())
	}

	a.mem.tab = 1
	tt.Frame()
	if !tt.HasText("代理 7890，阿里云要直连") {
		t.Fatalf("work tab missing its note; drew %v", tt.Texts())
	}
	if tt.HasText("Alex 说中文，结论先行") {
		t.Fatal("the global scope stayed on screen after the tab switch")
	}

	a.mem.tab = 2
	tt.Frame()
	if !tt.HasText("折叠归用户，展开归打开会话") {
		t.Fatalf("chat tab missing its note; drew %v", tt.Texts())
	}
}

// TestMemoryDrawerSaysWhenItIsEmpty covers the two empty states a first launch
// hits: no notes yet, and a pass that found nothing.
func TestMemoryDrawerSaysWhenItIsEmpty(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.mem.open = true

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("这里还没有记忆") {
		t.Fatalf("empty drawer said nothing; drew %v", tt.Texts())
	}

	a.mem.suggesting = true
	tt.Frame()
	if !tt.HasText("这一轮没有值得写下的东西") {
		t.Fatalf("empty proposal said nothing; drew %v", tt.Texts())
	}
}

// TestMemoryProposalKeepsLinesByDefault checks the proposal arrives ticked: the
// pass is the filter, so the user unchecks the few wrong lines instead of
// ticking the many right ones.
func TestMemoryProposalKeepsLinesByDefault(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := memoryFixture(t)
	a.applyCuration(&protocol.Curation{
		Scanned: 9,
		Tokens:  2411,
		Suggestions: []protocol.MemorySuggestion{
			{Text: "提测走 release/test 自己解冲突", Scope: "work", Reason: "重复三次"},
			{Text: "喜欢耳廓狐", Scope: "global"},
		},
	}, nil)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/memory-proposal.png", tt.Image())
	for _, want := range []string{"整理建议", "提测走 release/test 自己解冲突", "写入 2 条", "工作", "读了 9 段对话、2411 tokens"} {
		if !tt.HasText(want) {
			t.Fatalf("proposal missing %q; drew %v", want, tt.Texts())
		}
	}

	// Unchecking the second line is what the row's button does.
	a.mem.picked[1] = false
	tt.Frame()
	if !tt.HasText("写入 1 条") {
		t.Fatalf("count did not follow the boxes; drew %v", tt.Texts())
	}
	if kept := a.keptLines(); len(kept) != 1 || kept[0].Text != "提测走 release/test 自己解冲突" {
		t.Fatalf("kept %+v", kept)
	}

	// Nothing ticked means nothing to write: the drawer goes back to the notes
	// instead of posting an empty list.
	a.mem.picked[0] = false
	a.saveMemory()
	if a.mem.saving || a.mem.suggesting {
		t.Fatalf("an empty save posted; saving=%v suggesting=%v", a.mem.saving, a.mem.suggesting)
	}
}

// TestMemoryCurationFailureIsShownNotSwallowed: the pass is a model call and it
// fails, so the drawer has to say so and stay usable.
func TestMemoryCurationFailureIsShownNotSwallowed(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := memoryFixture(t)
	a.mem.curating = true
	a.applyCuration(nil, errFake("model timed out"))

	if a.mem.curating {
		t.Fatal("a failed pass left the drawer spinning")
	}
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("整理失败：model timed out") {
		t.Fatalf("failed pass said nothing; drew %v", tt.Texts())
	}
	// The way out is still there.
	a.closeSuggestions()
	tt.Frame()
	if !tt.HasText("Alex 说中文，结论先行") {
		t.Fatalf("cancel did not go back to the notes; drew %v", tt.Texts())
	}
}

// TestMemoryReplyForAnOldChatIsDropped: the chat scope belongs to the open chat,
// so a reply that lands after the user switched must not take over the tab.
func TestMemoryReplyForAnOldChatIsDropped(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := memoryFixture(t)
	a.active = 12
	a.mem.chatID = 12

	a.applyMemory(11, &protocol.Memory{
		Global: []protocol.Note{{Text: "still fine"}},
		Chat:   []protocol.Note{{Text: "a sentence from the chat left behind"}},
	}, nil)

	if len(a.mem.notes[0]) != 1 || a.mem.notes[0][0].Text != "still fine" {
		t.Fatalf("global notes not taken: %+v", a.mem.notes[0])
	}
	if len(a.mem.notes[2]) != 1 || a.mem.notes[2][0].Text != "折叠归用户，展开归打开会话" {
		t.Fatalf("an old chat's notes overwrote the tab: %+v", a.mem.notes[2])
	}
}

// TestMemoryWhenFallsBackToTheRawString: a timestamp the bridge changed shape on
// is shown as it came, never as 1970.
func TestMemoryWhenFallsBackToTheRawString(t *testing.T) {
	i18n.Set(i18n.ZH)
	if got := memWhen("not a time"); got != "not a time" {
		t.Fatalf("memWhen %q", got)
	}
	past := time.Now().Add(-48 * time.Hour).Format(time.RFC3339)
	if got := memWhen(past); got == past {
		t.Fatalf("memWhen left a real time as its ISO string")
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }

// TestMemoryNoteSaysWhereItCameFrom checks a remembered line carries its receipt.
// The note is a claim about the user; the first thing a wrong one makes you ask is
// where it was said, and "from chat #11" is an answer you can go and read.
func TestMemoryNoteSaysWhereItCameFrom(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := memoryFixture(t)
	a.mem.notes[0] = append(a.mem.notes[0],
		protocol.Note{At: time.Now().Add(-4 * time.Hour).Format(time.RFC3339), Text: "周五不提测", Source: "chat:8"},
		protocol.Note{At: time.Now().Add(-30 * time.Minute).Format(time.RFC3339), Text: "分支红线", Source: "curator"},
		protocol.Note{At: time.Now().Add(-9 * time.Minute).Format(time.RFC3339), Text: "手敲的备忘", Source: "feishu"},
		protocol.Note{At: time.Now().Add(-9 * time.Minute).Format(time.RFC3339), Text: "老记忆没有出处"},
	)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/memory-receipts.png", tt.Image())
	for _, want := range []string{"来自 会话 #8", "来自夜间整理", "来自飞书备忘"} {
		if !tt.HasText(want) {
			t.Fatalf("receipt %q not drawn; drew %v", want, tt.Texts())
		}
	}
}

// TestMemoryReceiptOpensTheChatItCameFrom checks the receipt is a door, not a
// label: asking "where did you get that" should land you in the conversation,
// with the drawer out of the way.
func TestMemoryReceiptOpensTheChatItCameFrom(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := memoryFixture(t)
	a.mem.notes[0] = append(a.mem.notes[0],
		protocol.Note{At: time.Now().Add(-4 * time.Hour).Format(time.RFC3339), Text: "周五不提测", Source: "chat:8"})

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if a.active == 8 {
		t.Fatalf("fixture already on chat 8")
	}
	if err := tt.Click("· 来自 会话 #8"); err != nil {
		t.Fatalf("receipt not clickable: %v", err)
	}
	tt.Frame()
	if a.active != 8 {
		t.Fatalf("receipt left the window on chat %d, want 8", a.active)
	}
	if a.mem.open {
		t.Fatalf("drawer stayed open over the chat it opened")
	}
}

// TestMemoryNoteFromThisChatNeedsNoReceipt checks the chat tab stays quiet: every
// line there came from the chat you are already reading.
func TestMemoryNoteFromThisChatNeedsNoReceipt(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := memoryFixture(t)
	a.mem.tab = 2
	a.mem.notes[2] = []protocol.Note{
		{At: time.Now().Add(-5 * time.Minute).Format(time.RFC3339), Text: "折叠归用户", Source: "chat:" + strconv.Itoa(a.active)},
	}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, text := range tt.Texts() {
		if strings.Contains(text, "来自 会话") {
			t.Fatalf("chat tab repeated its own receipt: %q", text)
		}
	}
}
