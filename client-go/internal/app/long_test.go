package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"pitta/internal/diff"
	"pitta/internal/protocol"
	"pitta/internal/turns"
)

// TestLiveLongConversation measures the long-chat path against the real
// bridge: a 200-entry conversation parsed into turns, a patch parsed out of the
// tool results, and one frame's cost. It writes /tmp/pitta-long.png so the
// Markdown can be looked at.
func TestLiveLongConversation(t *testing.T) {
	if os.Getenv("PITTA_LIVE") == "" {
		t.Skip("set PITTA_LIVE=1 to read the real bridge")
	}
	a := New(liveConfig())
	a.previewImage = -1
	a.init()
	conversationID := longestConversation(t, a)
	if value := os.Getenv("PITTA_CONV"); value != "" {
		conversationID = atoi(value)
	}
	// Load over HTTP instead of selectConversation: the WS watch snapshot can
	// carry fewer entries than the 200 the HTTP endpoint returns, and this test
	// measures the 200-entry path.
	loadEntries(t, a, conversationID)
	if len(a.entries) == 0 {
		t.Skipf("conversation %d has no entries", conversationID)
	}

	built := turns.GroupTurns(a.entries, a.waits)
	patchLines := 0
	files := 0
	for index := range built {
		for _, step := range built[index].Steps {
			if step.Result == nil {
				continue
			}
			text, _ := turns.ToolResult(*step.Result)
			parsed := diff.Parse(text)
			if added, removed, paths := diff.Stats(parsed); added+removed > 0 && paths > 0 {
				files += paths
				for _, file := range parsed {
					for _, hunk := range file.Hunks {
						patchLines += len(hunk.Lines)
					}
				}
			}
		}
	}

	tt := ui.NewTester(a.View, 1180, 820)
	started := time.Now()
	tt.Frame()
	first := time.Since(started)
	writePNG(t, "/tmp/pitta-long.png", tt.Image())

	// Ten more frames: the second and later frames are what scrolling costs.
	started = time.Now()
	for i := 0; i < 10; i++ {
		tt.Frame()
	}
	perFrame := time.Since(started) / 10

	answers := 0
	for index := range built {
		answers += len(built[index].Finals)
	}
	t.Logf("conversation %d: entries=%d turns=%d answers=%d patchFiles=%d patchLines=%d firstFrame=%s laterFrame=%s",
		conversationID, len(a.entries), len(built), answers, files, patchLines,
		first.Round(time.Millisecond), perFrame.Round(time.Millisecond))
	if answers == 0 {
		t.Fatal("no answer drew")
	}
	if first > 2*time.Second {
		t.Fatalf("first frame %s is too slow for a 200-entry chat", first)
	}
	if perFrame > 200*time.Millisecond {
		t.Fatalf("later frame %s is too slow", perFrame)
	}
	if !tt.HasText("say") && !tt.HasText("Say") {
		t.Log("composer placeholder not found by text match; snapshot has the frame")
	}
}

// loadEntries fills the transcript from the HTTP endpoint, which is the widest
// view of a conversation the bridge offers (200 entries).
func loadEntries(t *testing.T, a *App, conversationID int) {
	t.Helper()
	conversation, err := a.api.Conversation(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("load conversation %d: %v", conversationID, err)
	}
	a.apply(func() {
		a.current = conversationID
		a.entries = conversation.Entries
		a.model = conversation.Model
		a.waits = conversation.ApprovalWaits
	})
}

// patchiestConversation finds the conversation whose tool results carry the
// most patch lines, so the diff renderer is exercised on real patches.
func patchiestConversation(t *testing.T, a *App) (conversationID, files, lines int) {
	t.Helper()
	for _, surface := range a.surfaces {
		if surface.ConversationID == 0 {
			continue
		}
		conversation, err := a.api.Conversation(context.Background(), surface.ConversationID)
		if err != nil || conversation == nil {
			continue
		}
		found, count := 0, 0
		for _, entry := range conversation.Entries {
			text, _ := turns.ToolResult(entry)
			if text == "" {
				continue
			}
			parsed := diff.Parse(text)
			if added, removed, paths := diff.Stats(parsed); added+removed > 0 && paths > 0 {
				found += paths
				for _, file := range parsed {
					for _, hunk := range file.Hunks {
						count += len(hunk.Lines)
					}
				}
			}
		}
		if count > lines {
			conversationID, files, lines = surface.ConversationID, found, count
		}
	}
	if conversationID == 0 {
		t.Skip("no conversation carries a patch")
	}
	t.Logf("patchiest conversation: %d with %d files and %d patch lines", conversationID, files, lines)
	return conversationID, files, lines
}

// TestLivePatchRender draws a real patch through the diff view.
func TestLivePatchRender(t *testing.T) {
	if os.Getenv("PITTA_LIVE") == "" {
		t.Skip("set PITTA_LIVE=1 to read the real bridge")
	}
	a := New(liveConfig())
	a.previewImage = -1
	a.init()
	conversationID, _, _ := patchiestConversation(t, a)
	loadEntries(t, a, conversationID)
	// Expand every step so the patches draw.
	a.apply(func() {
		for _, entry := range a.entries {
			a.stepOpen[entry.ID] = true
		}
	})
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/pitta-patch.png", tt.Image())
	t.Logf("patch frame drew for conversation %d", conversationID)
}

// TestLiveStreamingFrames counts the frames one real turn produces, which is
// the streaming path: every frame must land in the transcript without a panic.
// longestConversation reads every surface's conversation and returns the one
// with the most entries, which is the chat the window has to stay fast on.
func longestConversation(t *testing.T, a *App) int {
	t.Helper()
	best, most := 0, 0
	for _, surface := range a.surfaces {
		if surface.ConversationID == 0 {
			continue
		}
		conversation, err := a.api.Conversation(context.Background(), surface.ConversationID)
		if err != nil || conversation == nil {
			continue
		}
		if len(conversation.Entries) > most {
			best, most = surface.ConversationID, len(conversation.Entries)
		}
	}
	if best == 0 {
		t.Skip("no conversation with entries")
	}
	t.Logf("longest conversation: %d with %d entries", best, most)
	return best
}

func TestLiveStreamingFrames(t *testing.T) {
	if os.Getenv("PITTA_E2E") == "" {
		t.Skip("set PITTA_E2E=1 to spend a real model call")
	}
	a := New(liveConfig())
	a.previewImage = -1
	a.init()
	conversationID, surfaceID, err := a.api.NewConversation(context.Background(), a.project)
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	a.selectConversation(conversationID)
	t.Logf("conversation=%d surface=%s", conversationID, surfaceID)

	frames := make(chan int, 1)
	previous := 0
	go func() {
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			build := turns.GroupTurns(a.entries, a.waits)
			if len(build) > 0 && build[len(build)-1].Status(a.live) == turns.StatusDone {
				frames <- previous
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		frames <- previous
	}()

	a.sendText("Reply with exactly the word: ok")
	// Count value changes as the turn streams: each is a frame the UI drew.
	framesSeen := 0
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		entries := lastEntryCount(a)
		if entries != previous {
			previous = entries
			framesSeen++
		}
		build := turns.GroupTurns(a.entries, a.waits)
		if len(build) > 0 && build[len(build)-1].Status(a.live) == turns.StatusDone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	answer := ""
	build := turns.GroupTurns(a.entries, a.waits)
	if len(build) > 0 {
		for _, final := range build[len(build)-1].Finals {
			answer += turns.Text(final)
		}
	}
	if answer == "" {
		t.Fatal("no answer arrived")
	}
	t.Logf("streamed %d entry changes, answer=%q", framesSeen, answer)
}

// lastEntryCount reads the entry count on the UI thread, so the polling loop
// does not race the watch goroutine.
func lastEntryCount(a *App) int {
	count := make(chan int, 1)
	a.apply(func() { count <- len(a.entries) })
	return <-count
}

// TestLiveMarkdownRender renders the answers of a real conversation through
// Markdown, which is where an unsupported node would panic.
func TestLiveMarkdownRender(t *testing.T) {
	if os.Getenv("PITTA_LIVE") == "" {
		t.Skip("set PITTA_LIVE=1 to read the real bridge")
	}
	a := New(liveConfig())
	a.previewImage = -1
	a.init()
	loadEntries(t, a, longestConversation(t, a))
	built := turns.GroupTurns(a.entries, a.waits)
	rendered := 0
	for index := range built {
		for _, final := range built[index].Finals {
			text := turns.Text(final)
			if text == "" {
				continue
			}
			source := text
			tt := ui.NewTester(func(c *ui.Context) {
				markdown.View(c, source, &a.md)
			}, 760, 400)
			tt.Frame()
			rendered++
		}
	}
	if rendered == 0 {
		t.Skip("no answers to render")
	}
	t.Logf("rendered %d answers through Markdown", rendered)
}

var _ = protocol.Entry{}

// TestLiveOlderPagesLoad walks a real chat back past its first page: the client
// asks the bridge for the entries above the oldest one it holds, and what comes
// back lands above them, once, in the chat's order.
func TestLiveOlderPagesLoad(t *testing.T) {
	if os.Getenv("PITTA_LIVE") == "" {
		t.Skip("set PITTA_LIVE=1 to read the real bridge")
	}
	a := New(liveConfig())
	a.previewImage = -1
	a.init()
	conversationID := 512
	if value := os.Getenv("PITTA_CONV"); value != "" {
		conversationID = atoi(value)
	}
	loadEntries(t, a, conversationID)
	first, oldest := len(a.entries), a.oldestEntryID()
	a.apply(func() { a.historyMore = true })
	if first == 0 {
		t.Skipf("conversation %d has no entries", conversationID)
	}

	a.loadOlder()
	// The request runs in a goroutine; without a window it hands the page back
	// inline, so a short wait is enough for the handler to land.
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if len(a.entries) > first || !a.historyMore {
			break
		}
	}
	if len(a.entries) <= first {
		t.Fatalf("a page of older entries added nothing: %d entries", len(a.entries))
	}
	if a.entries[len(a.entries)-first].ID != oldest {
		t.Fatalf("the page was not put above the transcript: %d entries", len(a.entries))
	}
	for index := 1; index < len(a.entries); index++ {
		if a.entries[index].ID <= a.entries[index-1].ID {
			t.Fatalf("entry %d is out of order: %d after %d", index, a.entries[index].ID, a.entries[index-1].ID)
		}
	}
	t.Logf("conversation %d: %d entries after one page, oldest %d, more=%v",
		conversationID, len(a.entries), a.entries[0].ID, a.historyMore)
}
