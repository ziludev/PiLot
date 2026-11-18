package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

func TestThemeToggleCyclesThreeStates(t *testing.T) {
	i18n.Set(i18n.EN)
	a := New(Config{})
	// The cycle has a defined start; a theme another test wrote must not move it.
	a.prefs.Theme = ""
	seen := []string{}
	for i := 0; i < 4; i++ {
		a.toggleTheme()
		seen = append(seen, a.prefs.Theme)
	}
	want := []string{"light", "dark", "", "light"}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("toggle walked %v, want %v", seen, want)
		}
	}
	// Following the system means the palette is the one the window reports.
	tt := ui.NewTester(func(c *ui.Context) { a.applyThemeChoice(c) }, 100, 60)
	tt.SetDark(true)
	tt.Frame()
}

func TestRemoveWorktreeReportsUnmergedBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/worktree/remove" {
			_, _ = w.Write([]byte(`{"status":"unmerged","branch":"agent/x","commits":3}`))
			return
		}
		_, _ = w.Write([]byte(`{"surfaces":[]}`))
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.current = 11
	a.menu.open = true
	a.removeWorktree()
	deadline := time.Now().Add(2 * time.Second)
	for a.notice == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if a.notice != i18n.T("unmergedWork") {
		t.Fatalf("notice after an unmerged removal = %q", a.notice)
	}
	if !a.menu.open {
		t.Fatal("an unmerged branch closed the menu as if the checkout was gone")
	}
}

// testTokens is the palette a view needs outside App.View.
func testTokens(c *ui.Context) theme.Tokens { return theme.Current(c) }

func TestCommitsBaseSelectCallsSetGitBase(t *testing.T) {
	i18n.Set(i18n.EN)
	posted := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/git/base" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			posted <- body
		}
		if r.URL.Path == "/api/git" {
			_, _ = w.Write([]byte(`{"branch":"agent/x","base":"develop","branches":[{"name":"main"},{"name":"develop"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.previewImage = -1
	a.current = 11
	a.git = &protocol.Git{
		Branch:   "agent/x",
		Base:     "main",
		Branches: []protocol.Branch{{Name: "main", Current: true}, {Name: "develop"}},
	}
	tt := ui.NewTester(func(c *ui.Context) { a.sideChanges(c, testTokens(c)) }, 420, 700)
	tt.Frame()
	if err := tt.Click("main"); err != nil {
		t.Fatalf("open the base select: %v", err)
	}
	tt.Frame()
	if err := tt.Click("develop"); err != nil {
		t.Fatalf("pick a base: %v", err)
	}
	select {
	case body := <-posted:
		if body["base"] != "develop" {
			t.Fatalf("posted base = %v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("choosing a base never reached /api/git/base")
	}
}

// manyTurns builds n question/answer pairs, enough to overflow the pane.
func manyTurns(n int) []protocol.Entry {
	entries := make([]protocol.Entry, 0, 2*n)
	for i := 1; i <= n; i++ {
		entries = append(entries, turnEntries(i, "question "+itoa(i), "answer "+itoa(i))...)
	}
	return entries
}

// showsExactly reports whether the last frame drew a label whose whole text is
// want — HasText matches substrings, which is too loose for numbered fixtures.
func showsExactly(tt *ui.Tester, want string) bool {
	for _, text := range tt.Texts() {
		if text == want {
			return true
		}
	}
	return false
}

// TestTranscriptFollowsNewestTurn pins the behaviour the transcript view:231/286
// has: the list starts stuck to the end, keeps following while entries arrive,
// and only counts new messages once the user scrolled away.
func TestTranscriptFollowsNewestTurn(t *testing.T) {
	i18n.Set(i18n.EN)
	a := New(Config{})
	a.previewImage = -1
	a.current = 11
	a.entries = manyTurns(40)
	tt := ui.NewTester(func(c *ui.Context) { a.main(c) }, 800, 400)
	tt.Frame()
	tt.Frame()
	if !a.atBottom || !a.turnList.FollowEnd {
		t.Fatalf("a fresh chat is not at its end: atBottom=%v followEnd=%v", a.atBottom, a.turnList.FollowEnd)
	}
	if !a.turnList.AtEnd() {
		t.Fatal("the list does not show its last row")
	}

	// A streamed answer arrives while following: still at the end, nothing
	// counted as unseen.
	a.growEntries(append(a.entries, protocol.Entry{ID: 9001, Kind: "pi.user"},
		protocol.Entry{ID: 9002, Kind: "pi.assistant"}))
	tt.Frame()
	tt.Frame()
	if a.newMessages != 0 {
		t.Fatalf("following the stream counted %d unseen entries", a.newMessages)
	}
	if !a.atBottom {
		t.Fatal("the view stopped following the stream")
	}

	// The user scrolls up, then two turns arrive: they are counted and the bar
	// offers to jump to them.
	tt.Scroll(400, 200, 0, -400)
	tt.Frame()
	tt.Frame()
	if a.atBottom {
		t.Fatal("scrolling up did not stop the view from following")
	}
	a.growEntries(append(a.entries, protocol.Entry{ID: 9003, Kind: "pi.user"},
		protocol.Entry{ID: 9004, Kind: "pi.assistant"}))
	tt.Frame()
	if a.newMessages == 0 {
		t.Fatal("entries that arrived while scrolled away were not counted")
	}
	if !tt.HasText(itoa(a.newMessages) + " " + i18n.T("newMessages")) {
		t.Fatalf("jump bar texts = %q", tt.Texts())
	}

	// Jumping back re-sticks the view to the end.
	a.scrollToNewest()
	tt.Frame()
	if !a.atBottom {
		t.Fatal("jump to latest left the view away from the end")
	}
}

func TestTranscriptEmptyStateAndJumpBar(t *testing.T) {
	i18n.Set(i18n.EN)
	a := New(Config{})
	a.previewImage = -1
	tt := ui.NewTester(func(c *ui.Context) { a.transcript(c) }, 900, 400)
	tt.Frame()
	if !tt.HasText(i18n.T("noConversation")) {
		t.Fatalf("an empty chat says nothing: %q", tt.Texts())
	}

	a.current = 11
	a.newMessages = 3
	a.atBottom = false
	bar := ui.NewTester(func(c *ui.Context) { a.jumpBar(c) }, 900, 60)
	bar.Frame()
	if !bar.HasText("3 " + i18n.T("newMessages")) {
		t.Fatalf("jump bar texts = %q", bar.Texts())
	}
	if err := bar.Click("3 " + i18n.T("newMessages")); err != nil {
		t.Fatalf("click the jump bar: %v", err)
	}
	if a.newMessages != 0 || !a.turnList.FollowEnd {
		t.Fatalf("after the jump: newMessages=%d followEnd=%v", a.newMessages, a.turnList.FollowEnd)
	}
}
