package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
	"pilot/internal/protocol"
)

// writeBridge answers the write endpoints and remembers what they were asked,
// so a test can read the request a button really sent rather than the one the
// code intended to send.
type writeBridge struct {
	mu     sync.Mutex
	path   string
	body   string
	answer string
}

func newWriteBridge(t *testing.T, answer string) (*writeBridge, *httptest.Server) {
	t.Helper()
	b := &writeBridge{answer: answer}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		b.path, b.body = r.URL.Path, string(raw)
		answer := b.answer
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/commit", "/api/git/push", "/api/merge":
			_, _ = w.Write([]byte(answer))
		case "/api/changes":
			_, _ = w.Write([]byte(`{"files":[],"patch":"","branch":"app/right-panel"}`))
		case "/api/git":
			_, _ = w.Write([]byte(`{"branch":"app/right-panel","base":"main","commits":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return b, server
}

// writeFixture is the drawer open on a chat whose worktree is worth writing
// to: a branch, a base to merge into, a remote to push to.
func writeFixture(t *testing.T) *App {
	t.Helper()
	a := changesFixture(t)
	a.active, a.changes.conv = 12, 12
	a.changes.base = "main"
	a.changes.remoteKind = "github"
	return a
}

func (b *writeBridge) asked() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.path, b.body
}

// drainWrites applies the answers a frame loop would have applied.
func drainWrites(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.write.running == "" && len(a.inbox) == 0 {
			return
		}
		select {
		case ev := <-a.inbox:
			if ev.kind == evWrite {
				a.applyWrite(ev.convID, ev.writeKind, ev.text, ev.writeBad)
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("the write never came back")
}

// TestCommitAsksForAMessageThenCommits: the footer's 提交 opens a dialog, and
// what it sends is the message a person typed with their own worktree's id.
func TestCommitAsksForAMessageThenCommits(t *testing.T) {
	span, server := newWriteBridge(t, `{"head":"9f2c1ab34d5e"}`)
	a := writeFixture(t)
	a.api = api.New(server.URL, "")

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	tt.Click(i18n.T("commitAction"))
	tt.Frame()
	if !a.write.open || a.write.kind != writeCommit {
		t.Fatalf("提交 did not ask anything: open=%v kind=%q", a.write.open, a.write.kind)
	}
	if !tt.HasText(i18n.T("commitTitle")) {
		t.Fatalf("the dialog drew %v", tt.Texts())
	}

	a.write.message = "  app: one thing  "
	a.confirmWrite()
	drainWrites(t, a)

	path, body := span.asked()
	if path != "/api/commit" {
		t.Fatalf("the commit went to %q", path)
	}
	var sent struct {
		ConversationID int    `json:"conversationId"`
		Message        string `json:"message"`
	}
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if sent.ConversationID != a.changes.conv {
		t.Fatalf("the commit named chat %d, drawer shows %d", sent.ConversationID, a.changes.conv)
	}
	if sent.Message != "app: one thing" {
		t.Fatalf("the message went as %q", sent.Message)
	}
	if a.write.bad || !strings.Contains(a.write.result, "9f2c1ab") {
		t.Fatalf("the result reads %q (bad=%v)", a.write.result, a.write.bad)
	}
}

// TestCommitWithoutAMessageDoesNothing: an empty commit message is how a
// worktree fills up with "wip", so the button waits for words.
func TestCommitWithoutAMessageDoesNothing(t *testing.T) {
	span, server := newWriteBridge(t, `{"head":"9f2c1ab34d5e"}`)
	a := writeFixture(t)
	a.api = api.New(server.URL, "")
	a.write.open, a.write.kind, a.write.message = true, writeCommit, "   "
	a.confirmWrite()
	time.Sleep(80 * time.Millisecond)
	if path, _ := span.asked(); path != "" {
		t.Fatalf("an empty message still sent a commit to %q", path)
	}
}

// TestMergeConflictNamesTheFilesAndStays: a conflict is not a toast. The merge
// was undone by the bridge, and the files that caused it have to stay readable.
func TestMergeConflictNamesTheFilesAndStays(t *testing.T) {
	span, server := newWriteBridge(t,
		`{"status":"conflict","branch":"app/right-panel","base":"main","conflicts":["app.go","notes.md"],"message":"merge conflict"}`)
	a := writeFixture(t)
	a.api = api.New(server.URL, "")
	a.write.open, a.write.kind = true, writeMerge
	a.confirmWrite()
	drainWrites(t, a)

	if path, _ := span.asked(); path != "/api/merge" {
		t.Fatalf("the merge went to %q", path)
	}
	if !a.write.bad {
		t.Fatal("a conflict was reported as success")
	}
	for _, want := range []string{"app.go", "notes.md"} {
		if !strings.Contains(a.write.result, want) {
			t.Errorf("%q does not name %s", a.write.result, want)
		}
	}
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("notes.md") {
		t.Fatalf("the conflict left the screen; drew %v", tt.Texts())
	}
}

// TestMergedReportsHowMany: a clean merge says what it moved.
func TestMergedReportsHowMany(t *testing.T) {
	_, server := newWriteBridge(t,
		`{"status":"merged","branch":"app/right-panel","base":"main","commits":3}`)
	a := writeFixture(t)
	a.api = api.New(server.URL, "")
	a.write.open, a.write.kind = true, writeMerge
	a.confirmWrite()
	drainWrites(t, a)
	if a.write.bad || !strings.Contains(a.write.result, "3") || !strings.Contains(a.write.result, "main") {
		t.Fatalf("the merge reads %q (bad=%v)", a.write.result, a.write.bad)
	}
}

// TestPushRefusalIsReadable: the bridge refuses for reasons a person can act
// on, so the reason travels up as the sentence, not as "status".
func TestPushRefusalIsReadable(t *testing.T) {
	_, server := newWriteBridge(t, `{"status":"error","branch":"app/right-panel","message":"rejected: non-fast-forward"}`)
	a := writeFixture(t)
	a.changes.remoteKind = "github"
	a.api = api.New(server.URL, "")
	a.write.open, a.write.kind = true, writePush
	a.confirmWrite()
	drainWrites(t, a)
	if !a.write.bad || !strings.Contains(a.write.result, "non-fast-forward") {
		t.Fatalf("the push reads %q (bad=%v)", a.write.result, a.write.bad)
	}
}

// TestAVerbWithNothingToDoIsQuiet: no files means no commit, no base means no
// merge, no remote means no push — and asking anyway does not put a dialog up.
func TestAVerbWithNothingToDoIsQuiet(t *testing.T) {
	a := writeFixture(t)
	a.changes.files = nil
	a.changes.base = ""
	a.changes.remoteKind = ""
	for _, kind := range []writeKind{writeCommit, writePush, writeMerge} {
		if a.canWrite(kind) {
			t.Errorf("%s was offered with nothing to act on", kind)
		}
		a.openWrite(kind)
		if a.write.open {
			t.Errorf("%s asked anyway", kind)
		}
	}

	a.changes.files = []protocol.FileChange{{Path: "app.go", Status: "modified"}}
	a.changes.base = "main"
	a.changes.remoteKind = "none"
	if !a.canWrite(writeCommit) {
		t.Error("a file to commit was not offered a commit")
	}
	if a.canWrite(writePush) {
		t.Error("push was offered with no remote")
	}
	if !a.canWrite(writeMerge) {
		t.Error("a base to merge into was not offered a merge")
	}
	a.changes.base = a.changes.branch
	if a.canWrite(writeMerge) {
		t.Error("merge was offered into the branch it is already on")
	}
}

// TestWriteResultBelongsToItsChat: the answer from chat A must not appear over
// chat B's worktree.
func TestWriteResultBelongsToItsChat(t *testing.T) {
	a := writeFixture(t)
	a.applyWrite(a.changes.conv+1, writeCommit, "已提交 abc1234", false)
	if a.write.result != "" {
		t.Fatalf("another chat's result landed here: %q", a.write.result)
	}
	a.applyWrite(a.changes.conv, writeCommit, "已提交 abc1234", false)
	if a.write.result == "" {
		t.Fatal("this chat's own result was dropped")
	}
}
