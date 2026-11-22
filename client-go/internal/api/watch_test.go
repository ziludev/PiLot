package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
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
		if got := r.URL.Query().Get("ops"); got != "1" {
			t.Errorf("ops = %q, want 1 (the client asked for deltas)", got)
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

// The view before the delta stream and the view it must end on, as whole
// documents: both are what the bridge would send as `value` frames.
const initialView = `{
	"entries":[{"id":1,"kind":"pi.user","model":null,"data":{"text":"hi"}}],
	"docs":{
		"pi.live":{"run":{"status":"running"},"generation":null,"tools":[]},
		"pi.usage":{"cost":1}
	},
	"meta":{"title":"t"}
}`

const finalView = `{
	"entries":[
		{"id":1,"kind":"pi.user","model":null,"data":{"text":"hi"}},
		{"id":2,"kind":"pi.assistant","model":null,"data":{"text":"hello"}}
	],
	"docs":{
		"pi.live":{
			"run":{"status":"running"},
			"generation":{"message":{"content":[{"type":"text","text":"abc"}]}},
			"tools":[{"name":"bash"}],
			"busy":true
		},
		"pi.usage":{"cost":1}
	},
	"meta":{}
}`

// Every op verb the bridge can emit on this stream, applied in order.
var deltaOps = []string{
	`["p",["entries"],1,0,[{"id":2,"kind":"pi.assistant","model":null,"data":{"text":"hello"}}]]`,
	`["s",["docs","pi.live","busy"],true]`,
	`["p",["docs","pi.live","tools"],0,0,[{"name":"bash"}]]`,
	`["s",["docs","pi.live","generation"],{"message":{"content":[{"type":"text","text":"a"}]}}]`,
	`["a",["docs","pi.live","generation","message","content",0,"text"],"b"]`,
	`["a",["docs","pi.live","generation","message","content",0,"text"],"c"]`,
	`["d",["meta","title"]]`,
	`["m",["entries"],[1,0]]`,
	`["m",["entries"],[1,0]]`,
}

// TestWatchOpsMatchesFull is the important one: the same conversation arrives
// once as a baseline plus a delta stream and once as a single full frame. The
// final states must be deep-equal, so any semantic drift in an op shows up as a
// failing comparison rather than as a subtly wrong screen.
func TestWatchOpsMatchesFull(t *testing.T) {
	deltaMux := http.NewServeMux()
	deltaMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if err := conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"value","value":`+initialView+`}`)); err != nil {
			return
		}
		for _, op := range deltaOps {
			frame := `{"type":"ops","ops":[` + op + `]}`
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(frame)); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		<-r.Context().Done()
	})
	deltaServer := httptest.NewServer(deltaMux)
	defer deltaServer.Close()

	fullMux := http.NewServeMux()
	fullMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if err := conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"value","value":`+finalView+`}`)); err != nil {
			return
		}
		<-r.Context().Done()
	})
	fullServer := httptest.NewServer(fullMux)
	defer fullServer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	deltaFrames := New(deltaServer.URL, "").Watch(ctx, 1)
	fullFrames := New(fullServer.URL, "").Watch(ctx, 1)

	var last *protocol.FrameValue
	for i := 0; i < len(deltaOps)+1; i++ {
		frame := receiveFrame(t, deltaFrames)
		if frame.Value == nil {
			t.Fatalf("delta frame %d has no value", i)
		}
		last = frame.Value
	}
	want := receiveFrame(t, fullFrames)
	if want.Value == nil {
		t.Fatal("full frame has no value")
	}
	if last == nil || !reflect.DeepEqual(*last, *want.Value) {
		t.Fatalf("delta result = %#v\nfull result  = %#v", last, want.Value)
	}
	if got := last.Entries[0].ID; got != 1 {
		t.Fatalf("first entry = %d, the permutation did not land back", got)
	}
	if got := last.Entries[1].ID; got != 2 {
		t.Fatalf("second entry = %d, want 2", got)
	}
	if got := string(last.Entries[1].Data); got != `{"text":"hello"}` {
		t.Fatalf("second entry data = %s", got)
	}
}

// A delta before any baseline cannot be applied: the client must reconnect and
// take the first full frame of the new socket.
func TestWatchOpsBeforeBaselineReconnects(t *testing.T) {
	var connections atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		id := int(connections.Add(1))
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if id == 1 {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"ops","ops":[["s",["entries"],[]]]}`)); err != nil {
				return
			}
			return
		}
		if err := conn.Write(r.Context(), websocket.MessageText, []byte(frameJSON(2, "after reconnect", `{"status":"idle"}`))); err != nil {
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
	if first.Value == nil || first.Value.Entries[0].ID != 2 {
		t.Fatalf("first frame = %+v, want id 2 from the reconnected socket", first.Value)
	}
	if got := connections.Load(); got < 2 {
		t.Fatalf("connections = %d, want a reconnect", got)
	}
}

// A delta that fails to apply leaves the tree suspect. The client must drop the
// socket instead of projecting half a tree, then resume from a fresh baseline.
func TestWatchReconnectsOnBadOps(t *testing.T) {
	var connections atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		id := int(connections.Add(1))
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		if id == 1 {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(frameJSON(1, "baseline", `{"status":"running"}`))); err != nil {
				return
			}
			// "entries" is an array; a string key cannot index it.
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(`{"type":"ops","ops":[["s",["entries","x"],1]]}`)); err != nil {
				return
			}
			<-r.Context().Done()
			return
		}
		if err := conn.Write(r.Context(), websocket.MessageText, []byte(frameJSON(2, "repaired", `{"status":"idle"}`))); err != nil {
			return
		}
		<-r.Context().Done()
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	frames := New(server.URL, "").Watch(ctx, 1)

	first := receiveFrame(t, frames)
	if first.Value == nil || first.Value.Entries[0].ID != 1 {
		t.Fatalf("first frame = %+v, want the baseline id 1", first.Value)
	}
	second := receiveFrame(t, frames)
	if second.Value == nil || second.Value.Entries[0].ID != 2 {
		t.Fatalf("second frame = %+v, want the repaired baseline id 2", second.Value)
	}
	if got := connections.Load(); got < 2 {
		t.Fatalf("connections = %d, want a reconnect after the bad delta", got)
	}
}
