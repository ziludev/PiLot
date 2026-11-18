package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// TestMessageActionsCopyEditRetry covers the three buttons the transcript view:20-46
// puts on every message.
func TestMessageActionsCopyEditRetry(t *testing.T) {
	i18n.Set(i18n.EN)
	submitted := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/submit" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			submitted <- body
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.previewImage = -1
	a.current = 11
	a.entries = []protocol.Entry{
		{ID: 1, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "why is the sky blue"})},
		{ID: 2, Kind: "pi.assistant", Model: rawModel(map[string]any{"role": "assistant", "content": "Rayleigh scattering."})},
	}
	tt := ui.NewTester(func(c *ui.Context) { a.transcript(c) }, 900, 420)
	// The actions only show under the pointer, so park it on the message first.
	hover := func() {
		if msg, ok := tt.Find("why is the sky blue"); ok {
			tt.Move(msg.X+msg.W/2, msg.Y+msg.H/2)
		}
		tt.Frame()
	}
	hover()
	tt.Frame()

	if err := tt.Click(i18n.T("copy")); err != nil {
		t.Fatalf("click copy: %v", err)
	}
	if got := tt.Clipboard(); got == "" {
		t.Fatalf("copy left the clipboard empty; texts=%q", tt.Texts())
	}
	hover()
	if err := tt.Click(i18n.T("edit")); err != nil {
		t.Fatalf("click edit: %v", err)
	}
	if a.draft == "" {
		t.Fatal("edit did not push the message into the composer")
	}
	hover()
	if err := tt.Click(i18n.T("retry")); err != nil {
		t.Fatalf("click retry: %v", err)
	}
	select {
	case body := <-submitted:
		if body["text"] == "" {
			t.Fatalf("retry submitted an empty text: %v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("retry never reached the bridge")
	}
}

// TestRailReorderSavesTheOrder drags a chat above another and checks the saved
// order, which survives a reload through prefs.
func TestRailReorderSavesTheOrder(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	a.project = "p1"
	a.surfaces = []protocol.Surface{
		{ConversationID: 13, ProjectID: "p1", Title: "c13"},
		{ConversationID: 12, ProjectID: "p1", Title: "c12"},
		{ConversationID: 11, ProjectID: "p1", Title: "c11"},
	}
	tt := ui.NewTester(func(c *ui.Context) { a.rail(c) }, 260, 420)
	tt.Frame()
	if len(a.rows) < 3 {
		t.Fatalf("rail drew %d rows", len(a.rows))
	}

	// Drag the last chat (11) to the first row: it pins and moves to the top.
	a.reorderChats([]int{2}, 0)
	if len(a.prefs.Order) == 0 || a.prefs.Order[0] != 11 {
		t.Fatalf("order after the drag = %v", a.prefs.Order)
	}
	if !a.isPinned(11) {
		t.Fatalf("dropping on the top row did not pin: %v", a.prefs.Pinned)
	}
	if got := a.sortedSurfaces()[0].ConversationID; got != 11 {
		t.Fatalf("the dragged chat sorts first, got %d", got)
	}
}

// TestSearchHitMarksTheEntry checks the ring the cross-chat search leaves: the
// rail hands an entry id to the transcript, and that line is the one drawn with
// the accent border.
func TestSearchHitMarksTheEntry(t *testing.T) {
	a := snapshotData(t)
	a.current = 11
	a.entries = turnEntries(11, "why is the sky blue", "Rayleigh scattering.")
	a.focusEntry = a.entries[len(a.entries)-1].ID
	if !a.isHit(a.entries[len(a.entries)-1]) {
		t.Fatal("the matched entry is not marked")
	}
	if a.isHit(a.entries[0]) {
		t.Fatal("the question is marked too")
	}
	// A jump clears the ring once the reader moves on.
	a.focusEntry = 0
	if a.isHit(a.entries[len(a.entries)-1]) {
		t.Fatal("the ring outlives the jump")
	}
}

// TestCopyCarriesMarkdown checks the richer clipboard: a markdown editor
// pasting the answer keeps its structure.
func TestCopyCarriesMarkdown(t *testing.T) {
	copied := New(Config{})
	tt := ui.NewTester(func(c *ui.Context) {
		if copied.draft != "" {
			return
		}
		copied.copyText(c, "# Title\n\nbody")
		copied.draft = "done"
	}, 400, 200)
	tt.Frame()
	tt.Frame()
	data := tt.ClipboardData()
	markdown, err := data.Read(transfer.Markdown)
	if err != nil || !strings.Contains(string(markdown), "body") {
		t.Fatalf("markdown form is %q (%v)", markdown, err)
	}
	if plain := tt.Clipboard(); !strings.Contains(plain, "body") {
		t.Fatalf("plain form is %q", plain)
	}
}
