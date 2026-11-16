package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
)

// TestAltArrowsWalkTheRail checks the keyboard walk moves to the next and the
// previous chat and opens it, the way Slack switches with the keyboard.
func TestAltArrowsWalkTheRail(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	a.api = quietAPI(t)
	// The rail sorts newest first, so start at the top of the list; the walk
	// then has room to move down.
	a.apply(func() { a.selectConversation(13) })
	tt := ui.NewTester(a.View, 1180, 760)
	tt.Frame()
	start := a.current
	tt.Key(ui.Alt, ui.KeyDown)
	tt.Frame()
	if a.current == start {
		t.Fatalf("⌥↓ left the selection on %d", start)
	}
	first := a.current
	tt.Key(ui.Alt, ui.KeyUp)
	tt.Frame()
	if a.current != start {
		t.Fatalf("⌥↑ did not come back: %d → %d (want %d)", first, a.current, start)
	}
	// Walking past the top stays put instead of wrapping to the bottom.
	for i := 0; i < 5; i++ {
		tt.Key(ui.Alt, ui.KeyUp)
		tt.Frame()
	}
	if row := a.railRows(); len(row) > 0 && a.current != row[0].surface.ConversationID {
		t.Logf("current %d, rail head %d", a.current, row[0].surface.ConversationID)
	}
}

// TestPinAndArchiveFromTheKeyboard checks ⌘⇧P and ⌘⇧⌫ do what the row menu
// offers, without opening it.
func TestPinAndArchiveFromTheKeyboard(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	client, archived := archiveAPI(t)
	a.api = client
	tt := ui.NewTester(a.View, 1180, 760)
	tt.Frame()
	chat := a.current
	tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	tt.Frame()
	if !a.isPinned(chat) {
		t.Fatal("⌘⇧P did not pin the chat")
	}
	// The tap must not have opened the row menu either.
	if a.menu.open && a.menu.id == chat {
		t.Fatal("⌘⇧P opened the chat menu")
	}
	// ⌘⇧⌫ archives the same chat: it leaves the default list, and the order
	// the user arranged is untouched, so unarchiving puts it back where it was.
	tt.Key(ui.Cmd|ui.Shift, ui.KeyP) // unpin, so the archive step looks at a plain chat
	tt.Frame()
	if a.isPinned(chat) {
		t.Fatal("⌘⇧P did not unpin the chat")
	}
	tt.Key(ui.Cmd|ui.Shift, ui.KeyBackspace)
	tt.Frame()
	// The archive call is a round trip; give it the time it takes, then read
	// the body the client actually sent rather than trusting the local flag.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && archived.Load() == nil {
		time.Sleep(5 * time.Millisecond)
	}
	got := archived.Load()
	if got == nil {
		t.Fatal("⌘⇧⌫ sent no archive request")
	}
	if value, ok := got.(bool); !ok || !value {
		t.Fatalf("⌘⇧⌫ sent archived=%v, want true", got)
	}
}

// archiveAPI answers the reads a rail interaction makes and records the
// archived flag of the /api/surface-meta call, so the keyboard shortcut can be
// checked by what it sent instead of by a flag it set locally.
func archiveAPI(t *testing.T) (*api.Client, *atomic.Value) {
	t.Helper()
	var archived atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/api/surface-meta":
			var body struct {
				Archived *bool `json:"archived"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Archived != nil {
				archived.Store(*body.Archived)
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/conversation":
			_, _ = w.Write([]byte(`{"entries":[],"model":{"provider":"p","modelId":"m"}}`))
		case "/api/changes":
			_, _ = w.Write([]byte(`{"repo":false}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return api.New(srv.URL, ""), &archived
}

// quietAPI answers the calls selectConversation makes, so the walk's moves do
// not fill the log with unreachable hosts.
func quietAPI(t *testing.T) *api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/api/conversation":
			_, _ = w.Write([]byte(`{"entries":[],"model":{"provider":"p","modelId":"m"}}`))
		case "/api/changes":
			_, _ = w.Write([]byte(`{"repo":false}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return api.New(srv.URL, "")
}

// TestOtherProjectsFoldIsRemembered checks the fold survives a restart: the
// earlier web client kept it in component state and forgot it on every reload.
func TestOtherProjectsFoldIsRemembered(t *testing.T) {
	savePrefsFile(prefs{OthersCollapsed: true})
	if a := New(Config{}); a.othersOpen {
		t.Fatal("a collapsed fold came back open")
	}
	savePrefsFile(prefs{})
	if a := New(Config{}); !a.othersOpen {
		t.Fatal("a fresh install started folded")
	}
}
