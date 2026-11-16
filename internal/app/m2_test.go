package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMergeDialogAsksFirstAndHandsConflictsOver(t *testing.T) {
	i18n.Set(i18n.EN)
	submitted := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/merge":
			_, _ = w.Write([]byte(`{"status":"conflict","branch":"agent/x","base":"main","conflicts":["a.go","b.go"]}`))
		case "/api/submit":
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
	a.sideOpen, a.sideTab, a.mergeOpen = true, SideChanges, true
	a.git = &protocol.Git{Branch: "agent/x", Base: "main"}
	a.changes = &protocol.Changes{Repo: true, Files: []protocol.FileChange{{Path: "a.go"}}}

	// A dirty checkout cannot merge: the button stays inert.
	tt := ui.NewTester(func(c *ui.Context) { a.sideChanges(c, testTokens(c)) }, 420, 700)
	tt.Frame()
	if !tt.HasText(i18n.T("commitFirst")) {
		t.Fatalf("dirty checkout was not refused: %q", tt.Texts())
	}

	// Clean the checkout, merge, and the conflict offers the agent.
	a.changes = &protocol.Changes{Repo: true}
	a.mergeBranch()
	deadline := time.Now().Add(2 * time.Second)
	for a.merge == nil && time.Now().Before(deadline) {
		tt.Frame()
		time.Sleep(5 * time.Millisecond)
	}
	if a.merge == nil || a.merge.Status != "conflict" || !a.mergeOpen {
		t.Fatalf("merge result = %+v open=%v", a.merge, a.mergeOpen)
	}
	tt.Frame()
	if err := tt.Click(i18n.T("askAgent")); err != nil {
		t.Fatalf("click ask agent: %v", err)
	}
	select {
	case body := <-submitted:
		text, _ := body["text"].(string)
		if !strings.Contains(text, "a.go") || !strings.Contains(text, "b.go") {
			t.Fatalf("the agent was not told the files: %q", text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("asking the agent submitted nothing")
	}
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
