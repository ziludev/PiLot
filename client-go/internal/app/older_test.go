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

// TestThePageBehindIsReadAhead: the second page is fetched while the reader is
// still at the bottom, so walking up finds it already merged instead of a
// pause — and the prefetch draws no "reading" line, since nothing is waiting.
func TestThePageBehindIsReadAhead(t *testing.T) {
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
		// Slow enough that a frame lands while the prefetch is in flight.
		time.Sleep(40 * time.Millisecond)
		switch before {
		case "":
			io.WriteString(w, pageBody(200, 319, true))
		case "200":
			io.WriteString(w, pageBody(80, 199, true))
		default:
			io.WriteString(w, pageBody(0, 79, false))
		}
	}))
	t.Cleanup(server.Close)

	a := newApp(Config{BaseURL: server.URL})
	a.api = api.New(server.URL, "")
	a.setSurfaces([]protocol.Surface{{ConversationID: 5, Kind: "client", Title: "long"}})
	a.active = 5
	// Opening a chat follows its end: the prefetch must not wait for a scroll.
	a.vm.threadList.FollowEnd = true
	a.loadThread(5)

	th := a.thread(5)
	tt := ui.NewTester(a.View, 1180, 820)
	for i := 0; i < 200 && !th.loaded; i++ {
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if !th.loaded {
		t.Fatal("the first page never arrived")
	}
	tt.Frame()

	mu.Lock()
	n := len(befores)
	mu.Unlock()
	if n < 2 || n > 3 {
		t.Fatalf("opening a chat read %d pages, want the first and the pages ahead", n)
	}
	if tt.HasText("Reading…") {
		t.Fatal("the prefetch drew a reading line nobody waits on")
	}

	// The prefetched page lands on its own, without scrolling.
	for i := 0; i < 400 && len(th.entries) < 320; i++ {
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if len(th.entries) != 320 {
		t.Fatalf("entries %d, want 320 after the prefetch", len(th.entries))
	}
	if th.hasMore {
		t.Fatal("the last page still claims more")
	}

	// Walking to the top has nothing left to fetch.
	for i := 0; i < 8; i++ {
		tt.Scroll(800, 400, 0, -1200)
		tt.Frame()
	}
	time.Sleep(120 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if len(befores) != 3 {
		t.Fatalf("scrolling to the top read again: %v", befores)
	}
}
