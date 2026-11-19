package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/notify"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// notifySenderFor points the notification sender at a channel for the length of
// one test, and returns the function that puts it back.
func notifySenderFor(t *testing.T, sent chan [2]string) func() {
	t.Helper()
	previous := notify.Sender
	notify.Sender = func(_ string, title, body string) error {
		select {
		case sent <- [2]string{title, body}:
		default:
		}
		return nil
	}
	return func() { notify.Sender = previous }
}

// timeout is how long a test waits for a goroutine the app started.
func timeout(t *testing.T) <-chan time.Time {
	t.Helper()
	return time.After(2 * time.Second)
}

// undoServer records the surface-meta writes the client makes and answers the
// state read that follows one.
func undoServer(t *testing.T, meta chan api.SurfaceMeta) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/surface-meta":
			var body api.SurfaceMeta
			_ = json.NewDecoder(r.Body).Decode(&body)
			meta <- body
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/api/state":
			_, _ = w.Write([]byte(`{"surfaces":[],"tasks":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
}

// TestArchiveToastUndoesItself pins the way back. The row folds out and says
// nothing about where the chat went; the archived filter is not a place anyone
// looks on purpose, so the toast it leaves behind has to carry the undo.
func TestArchiveToastUndoesItself(t *testing.T) {
	i18n.Set(i18n.EN)
	meta := make(chan api.SurfaceMeta, 1)
	srv := undoServer(t, meta)
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.previewImage = -1
	a.prefs = prefs{Theme: "light"}
	a.surfaces = []protocol.Surface{{ConversationID: 9, Kind: "feishu", Title: "Feishu planning"}}
	a.afterArchive(9)

	tt := ui.NewTester(func(c *ui.Context) {
		tok := theme.Light()
		theme.Apply(c, tok)
		ui.Column(c).Fill().Background(tok.Background).Children(func() {
			ui.Text(c, "under the glass").Padding(24, 24)
		})
		a.flushToasts(c)
		a.toasts(c)
	}, 420, 320)
	tt.Frame()
	if !tt.HasText(i18n.T("undo")) {
		t.Fatalf("the archive toast carries no undo, shows %q", tt.Texts())
	}
	if err := tt.Click(i18n.T("undo")); err != nil {
		t.Fatalf("click undo: %v", err)
	}

	select {
	case body := <-meta:
		if body.ConversationID != 9 {
			t.Fatalf("undo wrote surface %d, want 9", body.ConversationID)
		}
		if body.Archived == nil || *body.Archived {
			t.Fatalf("undo did not unarchive the chat: %+v", body)
		}
	case <-timeout(t):
		t.Fatalf("undo wrote nothing")
	}
}

// TestFinishedJobAnnouncesItself is the gap the rail's dot left: a job that runs
// for minutes ends while the reader is elsewhere, and dropping out of the rail
// is not a report. The toast names it, and the notification reaches past the
// window when the window is in the background.
func TestFinishedJobAnnouncesItself(t *testing.T) {
	i18n.Set(i18n.EN)
	sent := make(chan [2]string, 1)
	notifySender := notifySenderFor(t, sent)

	for _, focused := range []bool{false, true} {
		a := New(Config{})
		a.previewImage = -1
		a.prefs = prefs{Theme: "light"}
		a.focused = focused
		a.surfaces = []protocol.Surface{{ConversationID: 9, Kind: "feishu", Title: "Feishu planning"}}
		before := []protocol.Task{{ID: 7, Kind: "app.digest", ConversationID: 9, State: "running"}}

		a.announceFinished(before, map[int]bool{})

		if len(a.pendingToasts) != 1 {
			t.Fatalf("focused=%v: the finished job raised %d toasts, want 1", focused, len(a.pendingToasts))
		}
		toast := a.pendingToasts[0]
		if toast.Action != i18n.T("open") {
			t.Fatalf("focused=%v: the toast's action is %q, want one that opens the job", focused, toast.Action)
		}
		if toast.OnAction == nil {
			t.Fatalf("focused=%v: the toast's action does nothing", focused)
		}
		if !focused {
			select {
			case body := <-sent:
				if body[1] == "" {
					t.Fatalf("the notification has no body")
				}
			case <-timeout(t):
				t.Fatalf("a job that finished in the background said nothing to the system")
			}
			continue
		}
		select {
		case <-sent:
			t.Fatalf("a job that finished while the window is up also sent a notification")
		default:
		}
	}
	notifySender()
}

// TestFinishedJobLeavesChatsAlone keeps the announcement to jobs: a chat that
// answered is the answer path's business, and a turn still running has not
// finished anything.
func TestFinishedJobLeavesChatsAlone(t *testing.T) {
	i18n.Set(i18n.EN)
	a := New(Config{})
	a.previewImage = -1
	a.focused = true
	a.announceFinished([]protocol.Task{
		{ID: 8, Kind: "pi.generation", ConversationID: 11, State: "running"},
		{ID: 9, Kind: "app.digest", ConversationID: 12, State: "running"},
	}, map[int]bool{12: true})
	if len(a.pendingToasts) != 0 {
		t.Fatalf("a generation and a job still running raised %d toasts", len(a.pendingToasts))
	}
}

// TestJobRowShowsHowLongItHasBeenGoing pins the clock: a job four minutes in and
// one forty minutes in read the same without it, and the rail shows the word the
// filter row already carries.
func TestJobRowShowsHowLongItHasBeenGoing(t *testing.T) {
	i18n.Set(i18n.EN)
	a := railFixture(t)
	a.prefs.SurfaceFilter = "tasks"
	a.tasks = []protocol.Task{{ID: 7, Kind: "app.digest", ConversationID: 12, State: "running"}}
	a.taskSince = map[int]time.Time{7: time.Now().Add(-4 * time.Minute)}
	tt := railTest(t, a)
	if !tt.HasText("4m") {
		t.Fatalf("the job's row does not say how long it has been going, shows %q", tt.Texts())
	}
}
