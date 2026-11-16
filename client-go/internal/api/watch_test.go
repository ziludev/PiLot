package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"pitta/internal/protocol"
)

func frameJSON(id int, text, run string) string {
	return fmt.Sprintf(
		`{"type":"value","value":{"entries":[{"id":%d,"kind":"pi.user","model":null,"data":{"text":%q,"extra":true}}],`+
			`"docs":{"pi.live":{"run":%s,"generation":null,"tools":[],"future":1},`+
			`"pi.usage":{"models":{},"tools":{"x":1}}}},"ops":[]}`,
		id, text, run,
	)
}

func receiveFrame(t *testing.T, frames <-chan protocol.Frame) protocol.Frame {
	t.Helper()
	select {
	case frame, ok := <-frames:
		if !ok {
			t.Fatal("watch closed before a frame arrived")
		}
		return frame
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a frame")
		return protocol.Frame{}
	}
}

func TestWatchFrames(t *testing.T) {
	requests := make(chan *http.Request, 4)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(context.Background())
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for _, frame := range []string{
			frameJSON(1, "hello", `{"status":"running"}`),
			frameJSON(2, "hello again", `{"status":"idle"}`),
		} {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(frame)); err != nil {
				return
			}
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	frames := New(server.URL, "s3cret").Watch(ctx, 7)

	first := receiveFrame(t, frames)
	if first.Value == nil || len(first.Value.Entries) != 1 || first.Value.Entries[0].ID != 1 {
		t.Fatalf("first frame = %+v", first)
	}
	if !strings.Contains(string(first.Value.Entries[0].Data), `"extra":true`) {
		t.Fatalf("entry data = %s", first.Value.Entries[0].Data)
	}
	live, ok := first.Value.Docs["pi.live"]
	if !ok {
		t.Fatalf("docs = %+v", first.Value.Docs)
	}
	if !strings.Contains(string(live.Run), `"running"`) {
		t.Fatalf("live run = %s", live.Run)
	}

	second := receiveFrame(t, frames)
	if second.Value == nil || len(second.Value.Entries) != 1 || second.Value.Entries[0].ID != 2 {
		t.Fatalf("second frame = %+v", second)
	}

	select {
	case r := <-requests:
		if got := r.URL.Query().Get("conversationId"); got != "7" {
			t.Errorf("conversationId = %q", got)
		}
		if got := r.URL.Query().Get("token"); got != "s3cret" {
			t.Errorf("token = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("socket handler was not reached")
	}

	cancel()
	select {
	case _, ok := <-frames:
		if ok {
			t.Fatal("frame arrived after cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("watch did not close after cancel")
	}
}

func TestWatchReconnects(t *testing.T) {
	var connections atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		id := int(connections.Add(1))
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		frame := fmt.Sprintf(`{"value":{"entries":[{"id":%d,"kind":"pi.user","model":null,"data":{}}],"docs":{}}}`, id)
		if err := conn.Write(r.Context(), websocket.MessageText, []byte(frame)); err != nil {
			return
		}
		conn.Close(websocket.StatusNormalClosure, "bye")
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	frames := New(server.URL, "").Watch(ctx, 1)

	first := receiveFrame(t, frames)
	second := receiveFrame(t, frames)
	if first.Value == nil || second.Value == nil {
		t.Fatalf("frames = %+v %+v", first, second)
	}
	if got := first.Value.Entries[0].ID; got != 1 {
		t.Fatalf("first frame id = %d, want 1", got)
	}
	if got := second.Value.Entries[0].ID; got != 2 {
		t.Fatalf("second frame id = %d, want 2 (a stale connection leaked a frame)", got)
	}
	if got := connections.Load(); got < 2 {
		t.Fatalf("connections = %d, want a reconnect", got)
	}

	cancel()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-frames:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("watch did not close after cancel")
		}
	}
}
