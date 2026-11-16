package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// TestSidePanelOpensFileAndStagesNote covers the two things the changes tab
// does with a file: it opens it from the bridge, and a comment typed on a line
// becomes a review note the next message carries.
func TestSidePanelOpensFileAndStagesNote(t *testing.T) {
	var requested string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		requested = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"path":"a.go","content":"package a\n","lines":1,"size":10}`))
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.current = 11
	a.openFile("a.go")
	tt := ui.NewTester(a.View, 1200, 800)
	tt.Frame()
	if a.file == nil || a.view != ViewFile {
		t.Fatalf("file view did not open: view=%v file=%v err=%q", a.view, a.file, a.fileErr)
	}
	if requested != "/api/file?path=a.go&conversationId=11" {
		t.Fatalf("requested %q", requested)
	}
	if !tt.HasText("a.go") {
		t.Fatalf("file header missing; texts=%q", tt.Texts())
	}

	a.stageReviewNote("a.go", "3", "rename this")
	a.stageReviewNote("a.go", "3", "rename that")
	if len(a.review) != 1 || a.review[0].Note != "rename that" {
		t.Fatalf("staging did not replace the note on the same line: %+v", a.review)
	}
	a.dropReviewNote(0)
	if len(a.review) != 0 {
		t.Fatalf("note not dropped: %+v", a.review)
	}

	a.closeFile()
	tt.Frame()
	if a.view != ViewTranscript {
		t.Fatalf("closing the file did not return to the transcript")
	}
}

// TestSidePanelSwitchesTabs checks that opening a tab loads what it draws and
// that the tab strip survives a redraw.
func TestSidePanelSwitchesTabs(t *testing.T) {
	loaded := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		loaded <- r.URL.Path
		switch r.URL.Path {
		case "/api/memory":
			_, _ = w.Write([]byte(`{"global":[{"id":"1","text":"speak Chinese","createdAt":1}],"work":[],"chat":[]}`))
		case "/api/access":
			_, _ = w.Write([]byte(`{"chats":[{"chat":"owner","level":"owner"}],"envChats":[],"ownerChat":"owner"}`))
		case "/api/audit":
			_, _ = w.Write([]byte(`{"events":[]}`))
		case "/api/usage-calendar":
			_, _ = w.Write([]byte(`{"days":[{"date":"2026-11-16","cost":1.5,"tokens":1000,"turns":3}]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.current = 11
	tt := ui.NewTester(a.View, 1400, 860)

	a.openSideTab(SideMemory)
	for i := 0; i < 40 && len(a.memory.Global) == 0; i++ {
		tt.Frame()
	}
	if len(a.memory.Global) != 1 {
		t.Fatalf("memory not loaded: %+v errs=%+v", a.memory, a.errs)
	}
	tt.Frame()
	if !tt.HasText("speak Chinese") {
		t.Fatalf("memory note missing; texts=%q", tt.Texts())
	}

	a.openSideTab(SideAccess)
	for i := 0; i < 40 && a.access == nil; i++ {
		tt.Frame()
	}
	if a.access == nil || len(a.access.Chats) != 1 {
		t.Fatalf("access not loaded: %+v errs=%+v", a.access, a.errs)
	}

	a.openSideTab(SideUsage)
	for i := 0; i < 40 && len(a.usageDays) == 0; i++ {
		tt.Frame()
	}
	if len(a.usageDays) != 1 {
		t.Fatalf("usage not loaded: %+v errs=%+v", a.usageDays, a.errs)
	}

	a.toggleSide()
	if a.sideOpen {
		t.Fatalf("toggleSide did not close the panel")
	}
}

// TestProjectBarAddsAndSwitches covers the switcher: picking a project changes
// the selection, and adding a path reports what the bridge said.
func TestProjectBarAddsAndSwitches(t *testing.T) {
	var added string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects":
			var body struct {
				Path string `json:"path"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			added = body.Path
			_, _ = w.Write([]byte(`{"status":"added","project":{"id":"p2","name":"pitta","path":"/Users/me/work/pitta"}}`))
		case "/api/state":
			_, _ = w.Write([]byte(`{"surfaces":[],"tasks":[],"projects":[{"id":"p1","name":"workspace","path":"/Users/me/work/ws","builtin":true}],"feishu":{}}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	if err := a.loadState(t.Context()); err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got := a.currentProject(); got == nil || got.ID != "p1" {
		t.Fatalf("default project = %+v", got)
	}

	a.projectPath = "/Users/me/work/pitta"
	a.submitProject()
	for i := 0; i < 200 && added == ""; i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if added != "/Users/me/work/pitta" {
		t.Fatalf("posted path %q", added)
	}
	if a.notice == "" {
		t.Fatalf("no notice after adding a project")
	}

	a.setProject("p2")
	if a.project != "p2" {
		t.Fatalf("project = %q", a.project)
	}

	// The menu draws the projects with the current one checked.
	a.projectMenu = true
	tt := ui.NewTester(func(c *ui.Context) {
		a.projectBar(c, theme.Light())
	}, 320, 300)
	tt.Frame()
	if !tt.HasText("workspace") || !tt.HasText("/Users/me/work/ws") {
		t.Fatalf("project menu missing rows; texts=%q", tt.Texts())
	}
	if !tt.HasText("Open project…") && !tt.HasText("Open project") && !tt.HasText("打开项目") {
		t.Fatalf("open-project row missing; texts=%q", tt.Texts())
	}
}

// TestCommitDialogSubmits verifies the commit path posts the typed message and
// refreshes the commit list.
func TestCommitDialogSubmits(t *testing.T) {
	var posted struct {
		ConversationID int    `json:"conversationId"`
		Message        string `json:"message"`
	}
	gitCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/commit":
			_ = json.NewDecoder(r.Body).Decode(&posted)
			_, _ = w.Write([]byte(`{"hash":"abc123"}`))
		case "/api/git":
			gitCalls++
			_, _ = w.Write([]byte(`{"branch":"agent/conv-11","commits":[],"base":"main"}`))
		case "/api/changes":
			_, _ = w.Write([]byte(`{"repo":true,"branch":"agent/conv-11","files":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.current = 11
	a.openCommitDialog()
	if !a.commitDialogOpen {
		t.Fatalf("commit dialog did not open")
	}
	a.commitMsg = "fix the parser"
	a.submitCommit()
	for i := 0; i < 40 && a.commitDialogOpen; i++ {
		tt := ui.NewTester(a.View, 900, 700)
		tt.Frame()
	}
	if posted.Message != "fix the parser" || posted.ConversationID != 11 {
		t.Fatalf("posted %+v", posted)
	}
	if a.commitDialogOpen {
		t.Fatalf("dialog stayed open after a successful commit")
	}
	if gitCalls == 0 {
		t.Fatalf("commit did not refresh the commit list")
	}
	_ = protocol.Changes{}
}
