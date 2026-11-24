package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// changesFixture is the drawer with a two-file patch, opened
// without a network call: the panel reads what the bridge sent, and what the
// bridge sent is what these tests hand it.
func changesFixture(t *testing.T) *App {
	t.Helper()
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.changes.open = true
	a.changes.conv = a.active
	a.changes.branch = "app/right-panel"
	a.changes.files = []protocol.FileChange{
		{Path: "app.go", Status: "modified", Added: 3, Removed: 1},
		{Path: "notes.md", Status: "untracked", Added: 2},
	}
	a.changes.patch = "" +
		"diff --git a/app.go b/app.go\n" +
		"index 111..222 100644\n" +
		"--- a/app.go\n" +
		"+++ b/app.go\n" +
		"@@ -1,2 +1,2 @@\n" +
		"-old line\n" +
		"+new line\n" +
		"diff --git a/notes.md b/notes.md\n" +
		"new file mode 100644\n" +
		"--- /dev/null\n" +
		"+++ b/notes.md\n" +
		"@@ -0,0 +1,2 @@\n" +
		"+first\n" +
		"+second\n"
	return a
}

// TestSplitPatchCutsByFile: the bridge sends one patch, the drawer shows one
// file. The cut has to be exact, including files it invented for untracked ones.
func TestSplitPatchCutsByFile(t *testing.T) {
	a := changesFixture(t)
	sections := splitPatch(a.changes.patch)
	if len(sections) != 2 {
		t.Fatalf("sections %d", len(sections))
	}
	notes := filePatch(a.changes.patch, "notes.md")
	if len(notes) == 0 || !strings.HasPrefix(notes[0], "diff --git a/notes.md") {
		t.Fatalf("notes.md slice does not start at its header: %v", notes)
	}
	if notes[len(notes)-1] != "+second" && notes[len(notes)-2] != "+second" {
		t.Fatalf("notes.md slice lost its last added line: %v", notes)
	}
	if got := filePatch(a.changes.patch, "nope.go"); got != nil {
		t.Fatalf("a path the patch does not mention returned %v", got)
	}
	if path := patchPath("diff --git a/one/two.go b/one/two.go"); path != "one/two.go" {
		t.Fatalf("patchPath %q", path)
	}
}

// TestChangesDrawerListsThenShowsAPatch: the list is a summary, clicking a file
// shows its lines, and 返回 goes back.
func TestChangesDrawerListsThenShowsAPatch(t *testing.T) {
	a := changesFixture(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	// The counts are two columns now, not one summary string: added and
	// removed are separate facts with a colour each.
	for _, want := range []string{"改动", "文件", "提交", "app/right-panel", "app.go", "notes.md", "+3", "−1", "+2"} {
		if !tt.HasText(want) {
			t.Fatalf("list missing %q; drew %v", want, tt.Texts())
		}
	}

	a.openChangeFile("app.go")
	tt.Frame()
	if !tt.HasText("-old line") || !tt.HasText("+new line") {
		t.Fatalf("patch did not draw; drew %v", tt.Texts())
	}
	if tt.HasText("notes.md") {
		t.Fatal("the other file's list stayed on screen behind the patch")
	}

	tt.Click("返回")
	tt.Frame()
	if !tt.HasText("notes.md") {
		t.Fatalf("返回 did not go back to the list; drew %v", tt.Texts())
	}
}

// TestChangesDrawerSaysWhenTheWorktreeIsClean: an empty list is an answer, not
// a blank column.
func TestChangesDrawerSaysWhenTheWorktreeIsClean(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.changes.open = true
	a.changes.conv = a.active

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("工作区没有改动") {
		t.Fatalf("clean worktree said nothing; drew %v", tt.Texts())
	}

}

// TestChangesDrawerFollowsTheChat: the patch belongs to the open chat's worktree,
// so a reply for the chat the reader left must not land in the drawer.
func TestChangesDrawerFollowsTheChat(t *testing.T) {
	a := changesFixture(t)
	a.active = 12
	a.applyChanges(11, &protocol.Changes{
		Branch: "someone/else",
		Files:  []protocol.FileChange{{Path: "x.go", Status: "modified"}},
	}, nil)
	if a.changes.branch == "someone/else" || len(a.changes.files) != 2 {
		t.Fatalf("a patch for another chat landed: %+v %v", a.changes.branch, a.changes.files)
	}
}

// TestOpeningOneDrawerShutsTheOther: both live on the right edge, and two of
// them would leave the transcript a stripe. The bridge is a stub, because the
// point of the test is the drawer latch, not the patch.
func TestOpeningOneDrawerShutsTheOther(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.api = api.New(stubBridge(t, `{"repo":true}`, `{"commits":[]}`), "")

	a.mem.open = true
	a.toggleChanges()
	if !a.changes.open || a.mem.open {
		t.Fatalf("opening changes left both drawers: changes=%v memory=%v", a.changes.open, a.mem.open)
	}

	a.toggleMemory()
	if !a.mem.open || a.changes.open {
		t.Fatalf("opening memory left both drawers: changes=%v memory=%v", a.changes.open, a.mem.open)
	}
}

// stubBridge answers any GET with the next canned body, so a test can drive the
// drawers' goroutines without a real bridge.
func stubBridge(t *testing.T, bodies ...string) string {
	t.Helper()
	var next int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		body := "{}"
		if next < len(bodies) {
			body = bodies[next]
			next++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// TestWholeFileShowsTheFile: a patch answers what moved, not what the file says
// now, and the drawer has to be able to answer the second question.
func TestWholeFileShowsTheFile(t *testing.T) {
	a := changesFixture(t)
	tt := ui.NewTester(a.View, 1180, 820)
	a.openChangeFile("notes.md")
	tt.Frame()
	if !tt.HasText("first") {
		t.Fatalf("the patch did not draw; %v", tt.Texts())
	}

	// The handler answers on the server's goroutine and the test reads what it
	// was asked below, so the URI is behind a lock.
	var asked string
	var askedMu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		askedMu.Lock()
		asked = r.URL.RequestURI()
		askedMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"path":"notes.md","size":42,"truncated":false,"lines":3,"content":"package app\n\nfunc main() {}\n"}`))
	}))
	defer server.Close()
	a.api = api.New(server.URL, "")

	a.showWholeFile(true)
	deadline := time.Now().Add(2 * time.Second)
	for {
		askedMu.Lock()
		got := asked
		askedMu.Unlock()
		if got != "" || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// No frame loop here, so the reply is drained by hand, the way a frame
	// would have done it.
	time.Sleep(50 * time.Millisecond)
	for len(a.inbox) > 0 {
		ev := <-a.inbox
		if ev.kind == evFile {
			a.applyFileText(ev.convID, ev.path, ev.fileText, ev.truncated, ev.err)
		}
	}
	if len(a.changes.text) != 3 {
		t.Fatalf("the preview kept %d lines", len(a.changes.text))
	}
	if !strings.Contains(asked, "/api/file?") || !strings.Contains(asked, "path=notes.md") {
		t.Fatalf("the preview asked for %q", asked)
	}
	if !strings.Contains(asked, "conversationId="+itoa(a.changes.conv)) {
		t.Fatalf("the preview read from the wrong workspace: %q", asked)
	}

	tt.Frame()
	for _, want := range []string{"package app", "func main() {}", "1", "3"} {
		if !tt.HasText(want) {
			t.Errorf("the preview does not show %q; drew %v", want, tt.Texts())
		}
	}
}

// TestFileTextIsRetiredWhenTheDrawerMoves: a slow read for a file the user left
// must not paint itself over the file they are looking at.
func TestFileTextIsRetiredWhenTheDrawerMoves(t *testing.T) {
	a := changesFixture(t)
	a.openChangeFile("app.go")
	a.showWholeFile(true)
	asked := a.changes.textPath
	a.openChangeFile("notes.md")
	if a.changes.whole {
		t.Fatal("opening another file kept the file view")
	}
	a.applyFileText(a.changes.conv, asked, "package other\n", false, nil)
	if a.changes.text != nil {
		t.Fatalf("a reply for %s landed on %s", asked, a.changes.openFile)
	}
}

// TestSnapshotChanges draws the table the drawer now is: 35 tall rows, the file
// name at 13, the two counts right-aligned.
func TestSnapshotChanges(t *testing.T) {
	a := changesFixture(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/clientv2-changes.png", tt.Image())
}

// TestChangesFootSumsTheTable: the counts are per row, so the only place the
// drawer can say what the work totals is a foot row.
func TestChangesFootSumsTheTable(t *testing.T) {
	a := changesFixture(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{"2 个文件", "+5", "−1"} {
		if !tt.HasText(want) {
			t.Errorf("the foot does not say %q; drew %v", want, tt.Texts())
		}
	}
}
