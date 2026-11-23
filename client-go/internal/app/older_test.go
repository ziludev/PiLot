package app

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// pageBody is a conversation page: entries newest-last, as the bridge sends
// them, with the ids given.
func pageBody(from, to int, hasMore bool) string {
	entries := make([]protocol.Entry, 0, to-from+1)
	for id := from; id <= to; id++ {
		content := fmt.Sprintf("message %d", id)
		model := fmt.Sprintf(`[{"role":"user","content":%q,"timestamp":%d}]`, content, time.Now().UnixMilli())
		entries = append(entries, protocol.Entry{ID: id, Kind: "pi.user", Model: json.RawMessage(model)})
	}
	raw, _ := json.Marshal(map[string]any{
		"conversationId": 5,
		"hasMore":        hasMore,
		"entries":        entries,
	})
	return string(raw)
}

// TestWalkingToTheTopReadsThePageBehind: a chat longer than a page is read
// whole by scrolling up — the newest page arrives first, and reaching its top
// asks for the page before it by the oldest id held.
func TestWalkingToTheTopReadsThePageBehind(t *testing.T) {
	i18n.Set(i18n.EN)
	var mu sync.Mutex
	var befores []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/conversation" {
			io.WriteString(w, `{}`)
			return
		}
		before := r.URL.Query().Get("before")
		mu.Lock()
		befores = append(befores, before)
		mu.Unlock()
		if before == "" {
			io.WriteString(w, pageBody(200, 319, true))
			return
		}
		io.WriteString(w, pageBody(80, 199, false))
	}))
	t.Cleanup(server.Close)
	_ = api.New(server.URL, "")

	a := newApp(Config{BaseURL: server.URL})
	a.api = api.New(server.URL, "")
	a.setSurfaces([]protocol.Surface{{ConversationID: 5, Kind: "client", Title: "long"}})
	a.active = 5
	// Opening a chat follows its end, which is what keeps the first page from
	// asking for the second: only walking up should.
	a.vm.threadList.FollowEnd = true
	a.loadThread(5)

	th := a.thread(5)
	tt := ui.NewTester(a.View, 1180, 820)
	// The page arrives on a goroutine; frames until it lands.
	for i := 0; i < 200 && !th.loaded; i++ {
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if !th.loaded {
		t.Fatal("the first page never arrived")
	}
	if !th.hasMore {
		t.Fatal("the first page said there was nothing older")
	}
	tt.Frame()
	tt.Frame()
	mu.Lock()
	quiet := len(befores)
	mu.Unlock()
	if quiet != 1 {
		t.Fatalf("opening a chat read %d pages before anyone scrolled", quiet)
	}

	// Walk to the top: the transcript is taller than the window, so a large
	// upward scroll lands on its first row.
	for i := 0; i < 8; i++ {
		tt.Scroll(800, 400, 0, -1200)
		tt.Frame()
	}

	for i := 0; i < 40; i++ {
		tt.Frame()
		mu.Lock()
		n := len(befores)
		mu.Unlock()
		if n > 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(befores) < 2 {
		t.Fatalf("no page was read backwards; requests %v", befores)
	}
	if befores[1] != "200" {
		t.Fatalf("read before %q, want the oldest id 200", befores[1])
	}
	if th.hasMore {
		t.Fatal("the last page still claims more")
	}
	if len(th.entries) != 240 {
		t.Fatalf("entries %d, want 240", len(a.threads[5].entries))
	}
}
