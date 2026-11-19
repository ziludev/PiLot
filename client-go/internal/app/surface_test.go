package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// surfaceBridge is a bridge that answers surface writes and records them, so a
// test can see both what the rail did on its own and what it told the bridge.
type surfaceBridge struct {
	srv    *httptest.Server
	writes chan map[string]any
	status int
}

func newSurfaceBridge(t *testing.T) *surfaceBridge {
	t.Helper()
	b := &surfaceBridge{writes: make(chan map[string]any, 8), status: http.StatusOK}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/surface-meta" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var patch map[string]any
		_ = json.Unmarshal(body, &patch)
		b.writes <- patch
		w.WriteHeader(b.status)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(b.srv.Close)
	return b
}

// take waits for the one write the test expects.
func (b *surfaceBridge) take(t *testing.T) map[string]any {
	t.Helper()
	select {
	case patch := <-b.writes:
		return patch
	case <-time.After(2 * time.Second):
		t.Fatal("the rail never told the bridge")
		return nil
	}
}

// railApp is a two-chat rail over the test bridge.
func railApp(t *testing.T, baseURL string) *App {
	t.Helper()
	a := newApp(Config{BaseURL: baseURL})
	a.now = time.Date(2026, 11, 19, 15, 0, 0, 0, time.Local)
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 12, Kind: "feishu", Title: "sky"},
		{ConversationID: 11, Kind: "client", Title: "why is the sky blue"},
	})
	a.vm.tails[12] = a.now.Add(-2 * time.Minute)
	a.vm.tails[11] = a.now.Add(-3 * time.Minute)
	a.active = 12
	return a
}

// TestRailMenuArchive proves the whole path of the row menu: the ⋯ opens a menu
// with its three verbs, and Archive patches the mirror before the bridge answers,
// so the row has already left the sections by the time the frame is drawn.
func TestRailMenuArchive(t *testing.T) {
	i18n.Set(i18n.EN)
	bridge := newSurfaceBridge(t)
	a := railApp(t, bridge.srv.URL)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("⋯"); err != nil {
		t.Fatalf("the row menu has no entry point: %v", err)
	}
	tt.Frame()
	if got := tt.Menu(); len(got) != 3 || got[0] != "Pin to top" ||
		got[1] != "Rename" || got[2] != "Archive" {
		t.Fatalf("menu = %v, want Pin to top, Rename, Archive", got)
	}
	writePNG(t, "/tmp/clientv2-menu.png", tt.Image())
	if err := tt.ChooseMenuItem("Archive"); err != nil {
		t.Fatalf("choose archive: %v", err)
	}

	// A menu choice only queues an event; the frame after it applies the patch.
	tt.Frame()
	tt.Frame()
	for _, s := range a.surfaceSnapshot() {
		if s.ConversationID == 12 && !s.Archived {
			t.Fatalf("surface = %+v, want 12 archived", s)
		}
	}

	patch := bridge.take(t)
	if patch["conversationId"] != float64(12) || patch["archived"] != true {
		t.Fatalf("wrote %v, want conversation 12 archived", patch)
	}
	if _, ok := patch["title"]; ok {
		t.Fatalf("wrote %v, want no title in an archive", patch)
	}

	// And the rail moved it: it left the sections for the shelf, which is open
	// because the chat on screen is the one that was just filed away.
	rows := a.railRows()
	shelf := -1
	for i, row := range rows {
		if row.kind == railGroup && row.section == sectionArchived {
			shelf = i
			if row.count != 1 || !row.open {
				t.Fatalf("shelf = %+v, want the one chat it holds, open", row)
			}
		}
		if row.kind == railChat && row.conv.ConversationID == 12 && i < shelf {
			t.Fatalf("archived chat still in the sections: %+v", rows)
		}
	}
	if shelf < 0 {
		t.Fatalf("rail = %+v, want an archived shelf", rows)
	}

	// The shelf's menu is the way back: the same item, the other way round. The
	// pointer is parked off the list first: the click that archived 12 left it
	// over the row that moved up into 12's place, and two rows showing a ⋯ make
	// "the ⋯" ambiguous.
	tt.Move(900, 700)
	tt.Frame()
	if err := tt.Click("⋯"); err != nil {
		t.Fatalf("open the archived chat's menu: %v", err)
	}
	tt.Frame()
	if got := tt.Menu(); len(got) != 3 || got[2] != "Unarchive" {
		t.Fatalf("menu = %v, want Unarchive on an archived chat", got)
	}
	if err := tt.ChooseMenuItem("Unarchive"); err != nil {
		t.Fatalf("choose unarchive: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if patch := bridge.take(t); patch["archived"] != false {
		t.Fatalf("wrote %v, want the chat brought back", patch)
	}
	for _, s := range a.surfaceSnapshot() {
		if s.ConversationID == 12 && s.Archived {
			t.Fatalf("surface = %+v, want 12 back in the rail", s)
		}
	}
}

// TestRenameWritesTitle proves a rename lands on the mirror in the same frame
// it is confirmed, rather than waiting for the next snapshot.
func TestRenameWritesTitle(t *testing.T) {
	i18n.Set(i18n.EN)
	bridge := newSurfaceBridge(t)
	a := railApp(t, bridge.srv.URL)

	a.openRename(12)
	if !a.rename.open || a.rename.conv != 12 || a.rename.text != "sky" {
		t.Fatalf("dialog = %+v, want it seeded with the title", a.rename)
	}
	a.rename.text = "  the sky, renamed  "
	a.commitRename()

	if a.rename.open {
		t.Fatal("the dialog stayed open after saving")
	}
	if title := a.chatTitle(12); title != "the sky, renamed" {
		t.Fatalf("title = %q, want it renamed in the mirror", title)
	}

	patch := bridge.take(t)
	if patch["conversationId"] != float64(12) || patch["title"] != "the sky, renamed" {
		t.Fatalf("wrote %v, want the trimmed title", patch)
	}
	if _, ok := patch["archived"]; ok {
		t.Fatalf("wrote %v, want no archive in a rename", patch)
	}
}

// TestSurfaceWriteFailureUndoes: a bridge that refuses the write leaves the row
// as it was, so the rail never shows a change the bridge does not have.
func TestSurfaceWriteFailureUndoes(t *testing.T) {
	i18n.Set(i18n.EN)
	bridge := newSurfaceBridge(t)
	bridge.status = http.StatusBadRequest
	a := railApp(t, bridge.srv.URL)

	a.openRename(12)
	a.rename.text = "gone in a moment"
	a.commitRename()
	if title := a.chatTitle(12); title != "gone in a moment" {
		t.Fatalf("title = %q, want the patch applied at once", title)
	}
	bridge.take(t)

	// The refusal comes back as an event; draining is what a frame would do.
	deadline := time.Now().Add(2 * time.Second)
	for {
		a.drain()
		if title := a.chatTitle(12); title == "sky" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("title = %q, want the refusal undone", a.chatTitle(12))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSearchReplyForTheOldQueryIsDropped: typing on retires the reply in flight,
// so the hit list never shows one query's answers under another's text.
func TestSearchReplyForTheOldQueryIsDropped(t *testing.T) {
	i18n.Set(i18n.EN)
	a := railApp(t, "")
	hits := []protocol.SearchHit{{ConversationID: 12, EntryID: 4, Snippet: "deploy at six"}}

	a.filter = "deploy"
	a.watchSearch()
	a.applySearch(a.search.seq, "deploy", hits)
	if rows := a.searchRows(); len(rows) != 2 || rows[0].kind != railGroup {
		t.Fatalf("rows = %+v, want the header and the hit", rows)
	}

	// The user types on: the hits go, and the late reply for "deploy" is not
	// taken for an answer to "deploy n".
	a.filter = "deploy n"
	a.watchSearch()
	if rows := a.searchRows(); rows != nil {
		t.Fatalf("rows = %+v, want the retired hits gone", rows)
	}
	a.applySearch(a.search.seq-1, "deploy", hits)
	if rows := a.searchRows(); rows != nil {
		t.Fatalf("rows = %+v, want the late reply dropped", rows)
	}

	// Back under two characters the block goes away with the hits.
	a.filter = "d"
	a.watchSearch()
	if a.search.hits != nil || a.search.query != "" {
		t.Fatalf("search = %+v, want it cleared under two characters", a.search)
	}
}

// TestRenameThroughTheMenu walks the dialog the way a person does: the ⋯, the
// item, the box (which takes the keyboard as it opens), Save.
func TestRenameThroughTheMenu(t *testing.T) {
	i18n.Set(i18n.EN)
	bridge := newSurfaceBridge(t)
	a := railApp(t, bridge.srv.URL)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("⋯"); err != nil {
		t.Fatalf("open the row menu: %v", err)
	}
	tt.Frame()
	if err := tt.ChooseMenuItem("Rename"); err != nil {
		t.Fatalf("choose rename: %v", err)
	}
	tt.Frame()
	tt.Frame()
	if !a.rename.open || a.rename.conv != 12 || a.rename.text != "sky" {
		t.Fatalf("dialog = %+v, want it open on chat 12 with its title", a.rename)
	}

	tt.Type("!")
	tt.Frame()
	if a.rename.text != "sky!" {
		t.Fatalf("text = %q, want the typed name", a.rename.text)
	}
	if err := tt.Click("Save"); err != nil {
		t.Fatalf("press save: %v", err)
	}
	tt.Frame()
	tt.Frame()

	if a.rename.open {
		t.Fatal("the dialog stayed open after saving")
	}
	if title := a.chatTitle(12); title != "sky!" {
		t.Fatalf("title = %q, want it renamed in the same frame", title)
	}
	if patch := bridge.take(t); patch["title"] != "sky!" || patch["conversationId"] != float64(12) {
		t.Fatalf("wrote %v, want the new title", patch)
	}
}

// countText is how many text nodes say exactly this.
func countText(texts []string, s string) int {
	n := 0
	for _, text := range texts {
		if text == s {
			n++
		}
	}
	return n
}

// TestRailMenuFollowsThePointer: the ⋯ belongs to the chat on screen and to the
// row under the pointer, not to the whole list — a rail of dots is noise.
func TestRailMenuFollowsThePointer(t *testing.T) {
	i18n.Set(i18n.EN)
	a := railApp(t, "")
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if got := countText(tt.Texts(), "⋯"); got != 1 {
		t.Fatalf("⋯ count = %d, want only the chat on screen's", got)
	}

	where, ok := tt.Find("why is the sky blue")
	if !ok {
		t.Fatal("the other chat is not on the rail")
	}
	tt.Move(where.X+where.W/2, where.Y+where.H/2)
	tt.Frame()
	if got := countText(tt.Texts(), "⋯"); got != 2 {
		t.Fatalf("⋯ count = %d, want one more for the row under the pointer", got)
	}

	tt.Move(600, 600) // off the rail, over the conversation
	tt.Frame()
	if got := countText(tt.Texts(), "⋯"); got != 1 {
		t.Fatalf("⋯ count = %d, want the pointer's one gone", got)
	}
}

// TestSearchHitOpensItsChat: a hit is a way back into a chat, not just a line of
// text — pressing it is the same as pressing the chat in the rail.
func TestSearchHitOpensItsChat(t *testing.T) {
	i18n.Set(i18n.EN)
	a := railApp(t, "")
	a.active = 11
	a.filter = "deploy"
	a.search.query = "deploy"
	a.search.hits = []protocol.SearchHit{{ConversationID: 12, EntryID: 4, Snippet: "deploy at six"}}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("deploy at six"); err != nil {
		t.Fatalf("press the hit: %v", err)
	}
	tt.Frame()
	if a.active != 12 {
		t.Fatalf("active = %d, want the hit's chat", a.active)
	}
}
