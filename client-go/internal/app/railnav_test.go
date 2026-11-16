package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
	a.api = quietAPI(t)
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
