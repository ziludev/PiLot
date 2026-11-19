package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// changesFixture is the drawer with a two-file patch and two commits, opened
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
	a.changes.commits = []protocol.Commit{
		{Hash: "abc1234", Short: "abc1234", Subject: "app: one", Files: 2, Added: 5, Removed: 1},
	}
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
	for _, want := range []string{"改动", "提交", "app/right-panel", "app.go", "notes.md", "+3 −1", "+2"} {
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

	a.changes.tab = 1
	tt.Frame()
	if !tt.HasText("这个分支还没有提交") {
		t.Fatalf("empty commit list said nothing; drew %v", tt.Texts())
	}
}

// TestChangesDrawerShowsACommitPatch: the commits tab lists the log, and a
// commit's patch arrives from its own request.
func TestChangesDrawerShowsACommitPatch(t *testing.T) {
	a := changesFixture(t)
	a.changes.tab = 1
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{"abc1234", "app: one", "2 个文件"} {
		if !tt.HasText(want) {
			t.Fatalf("commit list missing %q; drew %v", want, tt.Texts())
		}
	}

	a.changes.openCommit = "abc1234"
	a.applyCommitDiff("abc1234", "diff --git a/x b/x\n+hello\n", nil)
	tt.Frame()
	if !tt.HasText("+hello") {
		t.Fatalf("commit patch did not draw; drew %v", tt.Texts())
	}
}

// TestCommitPatchForAClosedCommitIsDropped: a slow reply must not open a commit
// the reader already left.
func TestCommitPatchForAClosedCommitIsDropped(t *testing.T) {
	a := changesFixture(t)
	a.changes.openCommit = "beef999"
	a.changes.lines = nil
	a.applyCommitDiff("abc1234", "+from the other commit\n", nil)
	if a.changes.lines != nil {
		t.Fatalf("a stale commit filled the pane: %v", a.changes.lines)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := "{}"
		if next < len(bodies) {
			body = bodies[next]
			next++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server.URL
}
