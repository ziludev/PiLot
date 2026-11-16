package app

import (
	"image/png"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/bridge"
)

// TestLiveBridgeWindow renders the shell against the agent that runs on this
// machine: the real rail, a conversation with hundreds of entries, and the
// composer, proving the window survives what the bridge actually sends.
//
// It only runs when asked (PITTA_LIVE=1), as it needs a running agent.
func TestLiveBridgeWindow(t *testing.T) {
	if os.Getenv("PITTA_LIVE") != "1" {
		t.Skip("set PITTA_LIVE=1 to render against the running agent")
	}
	cfg := Config{BaseURL: bridge.BaseURL(), Token: bridge.Token(), Home: bridge.Home()}
	a := New(cfg)
	a.init()
	if len(a.surfaces) == 0 {
		t.Fatalf("no surfaces loaded from %s", cfg.BaseURL)
	}
	// The biggest chat on this machine, so the transcript sees a long history.
	a.selectConversation(liveConversation(t, a))
	if len(a.entries) == 0 {
		t.Fatalf("conversation %d has no entries", a.current)
	}
	t.Logf("surfaces=%d conversation=%d entries=%d projects=%d", len(a.surfaces), a.current, len(a.entries), len(a.projects))

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	tt.Image()
	if !tt.HasText("Pitta") {
		t.Fatalf("rail title missing; texts=%q", tt.Texts())
	}
	f, err := os.Create("/tmp/pitta-live.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// liveConversation picks PITTA_CONV when set, else the longest chat on this
// machine (9753 has 500+ entries), else the first one.
func liveConversation(t *testing.T, a *App) int {
	t.Helper()
	if value := os.Getenv("PITTA_CONV"); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("PITTA_CONV=%q: %v", value, err)
		}
		return id
	}
	for _, s := range a.surfaces {
		if s.ConversationID == 9753 {
			return 9753
		}
	}
	if len(a.surfaces) > 0 {
		return a.surfaces[0].ConversationID
	}
	return 0
}

// TestLiveSidePanels opens each side tab against the running agent and writes
// a snapshot per tab, so the panels are checked against real bridge payloads
// rather than fixtures alone.
func TestLiveSidePanels(t *testing.T) {
	if os.Getenv("PITTA_LIVE") != "1" {
		t.Skip("set PITTA_LIVE=1 to render against the running agent")
	}
	cfg := Config{BaseURL: bridge.BaseURL(), Token: bridge.Token(), Home: bridge.Home()}
	a := New(cfg)
	a.init()
	a.selectConversation(liveConversation(t, a))

	tt := ui.NewTester(a.View, 1400, 860)
	for _, tab := range []SideTab{SideChanges, SideMemory, SideAccess, SideUsage} {
		a.openSideTab(tab)
		// The tab's data arrives from a goroutine; give it a moment so the
		// snapshot shows the panel rather than its loading state.
		for i := 0; i < 60 && !sideLoaded(a, tab); i++ {
			time.Sleep(50 * time.Millisecond)
		}
		tt.Frame()
		if len(a.errs) > 0 {
			t.Logf("%s errors: %+v", tabName(tab), a.errs)
		}
		path := "/tmp/pitta-side-" + tabName(tab) + ".png"
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
		t.Logf("%s → %s (changes=%v git=%v memory=%d access=%v usage=%d)",
			tabName(tab), path, a.changes != nil, a.git != nil,
			len(a.memory.Global)+len(a.memory.Work)+len(a.memory.Chat), a.access != nil, len(a.usageDays))
	}
	if a.access == nil || len(a.access.Chats) == 0 {
		t.Fatalf("access view empty: %+v", a.access)
	}
}

// sideLoaded reports whether the tab's data has arrived.
func sideLoaded(a *App, tab SideTab) bool {
	switch tab {
	case SideMemory:
		return len(a.memory.Global)+len(a.memory.Work)+len(a.memory.Chat) > 0
	case SideAccess:
		return a.access != nil
	case SideUsage:
		return len(a.usageDays) > 0
	}
	return a.changes != nil
}

// tabName is the file suffix for a tab's snapshot.
func tabName(tab SideTab) string {
	switch tab {
	case SideMemory:
		return "memory"
	case SideAccess:
		return "access"
	case SideUsage:
		return "usage"
	}
	return "changes"
}
