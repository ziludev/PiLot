package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// TestRunSearchFillsHits covers the submit path of the rail search: the box
// filters titles while typing, and submitting asks the bridge for transcript
// matches (SearchBox.tsx behaviour).
func TestRunSearchFillsHits(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.Query().Get("q")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"hits": []map[string]any{
				{"conversationId": 12, "entryId": 91, "kind": "pi.assistant", "snippet": "the parser lives in diff.go"},
			},
		})
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL})
	a.previewImage = -1
	a.filter = "  parser  "
	a.runSearch()

	if gotQuery != "parser" {
		t.Fatalf("bridge saw q=%q, want the trimmed query", gotQuery)
	}
	if len(a.searchHits) != 1 || a.searchHits[0].EntryID != 91 {
		t.Fatalf("hits = %+v, want the single hit", a.searchHits)
	}
}

// TestRunSearchSkipsShortQueries keeps the bridge out of a one-letter search,
// as SearchBox.tsx does with its two-character floor.
func TestRunSearchSkipsShortQueries(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL})
	a.previewImage = -1
	a.filter = "a"
	a.searchHits = []protocol.SearchHit{{ConversationID: 1}}
	a.runSearch()

	if called {
		t.Fatal("bridge was asked for a one-letter query")
	}
	if a.searchHits != nil {
		t.Fatalf("stale hits kept: %+v", a.searchHits)
	}
}

// TestJumpToHitMarksTheTurn is the whole promise of a hit: open that chat and
// mark the turn holding the entry.
func TestJumpToHitMarksTheTurn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/conversation":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries": turnEntries(42, "question", "answer"),
			})
		default:
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL})
	a.previewImage = -1
	a.jumpToHit(protocol.SearchHit{ConversationID: 42, EntryID: 7})

	if a.currentValue() != 42 {
		t.Fatalf("current = %d, want the hit's conversation", a.currentValue())
	}
	if a.focusEntry != 7 {
		t.Fatalf("focusEntry = %d, want 7", a.focusEntry)
	}
	if a.searchHits != nil {
		t.Fatal("hits panel stayed open after the jump")
	}
}

// TestTurnOfEntryFindsToolResults covers the entry kinds a hit can point at.
func TestTurnOfEntryFindsToolResults(t *testing.T) {
	entries := turnEntries(1, "question", "answer")
	built := turns.GroupTurns(entries, nil)
	if len(built) != 1 {
		t.Fatalf("turns = %d, want 1", len(built))
	}

	if got := turnOfEntry(built, entries[0].ID); got != 0 {
		t.Fatalf("user entry -> turn %d, want 0", got)
	}
	if got := turnOfEntry(built, entries[len(entries)-1].ID); got != 0 {
		t.Fatalf("answer entry -> turn %d, want 0", got)
	}
	if got := turnOfEntry(built, 999999); got != -1 {
		t.Fatalf("unknown entry -> turn %d, want -1", got)
	}
}

// TestRailShowsFeishuOutage covers the banner Rail.tsx draws once the
// transport has been down for more than a minute.
func TestRailShowsFeishuOutage(t *testing.T) {
	render := func(feishu *protocol.Feishu) []string {
		a := New(Config{})
		a.previewImage = -1
		a.feishu = feishu
		a.surfaces = []protocol.Surface{{ConversationID: 1, SurfaceID: "s1", Title: "chat"}}
		tt := ui.NewTester(func(c *ui.Context) {
			theme.Apply(c, theme.Dark())
			a.rail(c)
		}, 300, 700)
		tt.Frame()
		return tt.Texts()
	}

	joined := strings.Join(render(&protocol.Feishu{Ready: false, DownMs: 120_000, LastError: "ws closed"}), "\n")
	if !strings.Contains(joined, "2m") {
		t.Fatalf("rail does not mention a 2 minute outage:\n%s", joined)
	}

	for _, quiet := range []*protocol.Feishu{nil, {Ready: false, DownMs: 30_000}, {Ready: true, DownMs: 600_000}} {
		joined = strings.Join(render(quiet), "\n")
		if strings.Contains(joined, "2m") || strings.Contains(joined, "10m") {
			t.Fatalf("rail shows an outage for %+v:\n%s", quiet, joined)
		}
	}
}
