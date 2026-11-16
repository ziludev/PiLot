package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"pitta/internal/bridge"
	"pitta/internal/protocol"
	"pitta/internal/turns"
)

// TestLiveTurnAgainstTheBridge sends one real turn through the same path the
// Send button uses and waits for the answer to stream back. It costs a model
// call, so it runs only under PITTA_E2E=1.
func TestLiveTurnAgainstTheBridge(t *testing.T) {
	if os.Getenv("PITTA_E2E") == "" {
		t.Skip("set PITTA_E2E=1 to spend a real model call")
	}

	cfg := Config{BaseURL: bridge.BaseURL(), Token: bridge.Token(), Home: bridge.Home()}
	a := New(cfg)
	a.init()
	if len(a.surfaces) == 0 {
		t.Fatalf("no surfaces loaded from %s", cfg.BaseURL)
	}
	project := ""
	if len(a.projects) > 0 {
		project = a.projects[0].ID
	}
	conversationID, surfaceID, err := a.api.NewConversation(contextBG(), project)
	if err != nil {
		t.Fatalf("new conversation: %v", err)
	}
	t.Logf("conversation=%d surface=%s", conversationID, surfaceID)
	a.selectConversation(conversationID)

	a.sendText("Reply with exactly the word: ok")

	deadline := time.Now().Add(120 * time.Second)
	streamed := false
	for time.Now().Before(deadline) {
		entries, live := snapshot(a)
		if live != nil {
			streamed = true
		}
		if answer := lastAnswer(entries); strings.Contains(strings.ToLower(answer), "ok") {
			t.Logf("answer streamed=%v entries=%d text=%q", streamed, len(entries), answer)
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	entries, live := snapshot(a)
	t.Fatalf("no answer within the deadline: entries=%d live=%v last=%q", len(entries), live != nil, lastAnswer(entries))
}

// snapshot reads what the UI thread currently holds.
func snapshot(a *App) ([]protocol.Entry, *protocol.LiveDoc) {
	var entries []protocol.Entry
	var live *protocol.LiveDoc
	a.apply(func() {
		entries = a.entries
		live = a.live
	})
	return entries, live
}

// lastAnswer is the newest assistant text, which is what a turn produces.
func lastAnswer(entries []protocol.Entry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Kind != "pi.assistant" {
			continue
		}
		if text := turns.Text(entries[i]); text != "" {
			return text
		}
	}
	return ""
}
