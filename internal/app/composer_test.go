package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// composerServer records the submissions the composer makes.
func composerServer(t *testing.T) (*httptest.Server, <-chan struct {
	ConversationID int    `json:"conversationId"`
	Text           string `json:"text"`
}) {
	t.Helper()
	sent := make(chan struct {
		ConversationID int    `json:"conversationId"`
		Text           string `json:"text"`
	}, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/submit" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			ConversationID int    `json:"conversationId"`
			Text           string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		sent <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, sent
}

// TestCmdEnterSendsEnterBreaksTheLine is the composer's keyboard contract, as
// the React one has it: Cmd+Enter sends, Enter and Shift+Enter break the line
// so a Chinese input session can confirm candidates with Enter.
func TestCmdEnterSendsEnterBreaksTheLine(t *testing.T) {
	srv, sent := composerServer(t)
	a := New(Config{BaseURL: srv.URL})
	a.current = 7

	tt := ui.NewTester(func(c *ui.Context) { a.composer(c) }, 480, 180)
	tt.Frame()
	tt.ClickAt(40, 40)
	tt.Type("hello")
	tt.Key(ui.Super, ui.KeyEnter)
	tt.Frame()

	select {
	case got := <-sent:
		if got.Text != "hello" || got.ConversationID != 7 {
			t.Fatalf("submitted %q to %d, want hello to 7", got.Text, got.ConversationID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Enter did not submit the draft")
	}
	if a.draft != "" {
		t.Fatalf("draft after send = %q, want empty", a.draft)
	}

	a.busy = false
	tt.Type("one")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	tt.Type("two")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Frame()
	tt.Type("three")
	tt.Frame()
	if a.draft != "one\ntwo\nthree" {
		t.Fatalf("draft after Enter and Shift+Enter = %q, want %q", a.draft, "one\ntwo\nthree")
	}
	select {
	case got := <-sent:
		t.Fatalf("Enter alone submitted %q", got.Text)
	default:
	}
}
