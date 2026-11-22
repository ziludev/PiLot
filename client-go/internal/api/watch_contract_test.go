package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A `value` frame that arrives mid-stream is a new baseline, not a repaint: the
// ops after it address the new tree, and applying them to the old one would
// either fail or, worse, succeed on a coincidentally similar path. The bridge
// only sends a mid-stream value when it has no delta, so this is the recovery
// path that keeps a long-lived socket honest.
//
// One connection throughout is the assertion that matters: a rebase that went
// through the error path would drop the socket, reconnect, and still end on the
// right frame — the frames alone would not tell the difference.
func TestWatchRebasesOnFullFrameMidStream(t *testing.T) {
	var connections atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for _, frame := range []string{
			// baseline
			`{"type":"value","value":{"entries":[{"id":1,"kind":"pi.user","model":null,"data":{"text":"one"}}],"docs":{"pi.live":{"generation":{"note":"a"},"tools":[]}}}}`,
			// delta on that baseline
			`{"type":"ops","ops":[["p",["entries"],1,0,[{"id":2,"kind":"pi.assistant","model":null,"data":{"text":"two"}}]]]}`,
			// the bridge lost the delta and sent a whole new view instead
			`{"type":"value","value":{"entries":[{"id":9,"kind":"pi.user","model":null,"data":{"text":"fresh"}}],"docs":{"pi.live":{"generation":{"note":"b"},"tools":[]}}}}`,
			// delta that only resolves against the new baseline
			`{"type":"ops","ops":[["a",["docs","pi.live","generation","note"],"!"],["s",["entries",0,"data","text"],"replaced"]]}`,
		} {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(frame)); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		<-r.Context().Done()
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	frames := New(server.URL, "").Watch(ctx, 1)

	baseline := receiveFrame(t, frames)
	applied := receiveFrame(t, frames)
	rebase := receiveFrame(t, frames)
	fourth := receiveFrame(t, frames)

	if baseline.Value == nil || len(baseline.Value.Entries) != 1 || baseline.Value.Entries[0].ID != 1 {
		t.Fatalf("baseline frame = %+v, want entry 1", baseline.Value)
	}
	if applied.Value == nil || len(applied.Value.Entries) != 2 || applied.Value.Entries[1].ID != 2 {
		t.Fatalf("second frame = %+v, want the delta applied to the first baseline", applied.Value)
	}
	if rebase.Value == nil || len(rebase.Value.Entries) != 1 || rebase.Value.Entries[0].ID != 9 {
		t.Fatalf("rebase frame = %+v, want the fresh baseline only", rebase.Value)
	}
	if fourth.Value == nil {
		t.Fatal("fourth frame has no value")
	}
	if got := len(fourth.Value.Entries); got != 1 || fourth.Value.Entries[0].ID != 9 {
		t.Fatalf("entries = %+v, want the new baseline's single entry", fourth.Value.Entries)
	}
	if got := string(fourth.Value.Entries[0].Data); got != `{"text":"replaced"}` {
		t.Fatalf("entry data = %s, want the delta applied to the new baseline", got)
	}
	if live, ok := fourth.Value.Docs["pi.live"]; !ok {
		t.Fatalf("docs = %+v", fourth.Value.Docs)
	} else if got := string(live.Generation); got != `{"note":"b!"}` {
		t.Fatalf("generation = %s, want b! (the append must land on the new baseline)", got)
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("connections = %d, want 1: a rebase must not need a reconnect", got)
	}
}

// Several delta frames on one socket must produce one frame each, in order, on
// one connection: a reconnect in the middle would surface as a duplicated
// baseline and would hide an apply error behind a working screen.
func TestWatchDeltaBurstKeepsOneConnection(t *testing.T) {
	var connections atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		connections.Add(1)
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for _, frame := range []string{
			`{"type":"value","value":{"entries":[],"docs":{"pi.live":{"tools":[]}}}}`,
			`{"type":"ops","ops":[["p",["entries"],0,0,[{"id":1,"kind":"pi.user","model":null,"data":{}}]]]}`,
			`{"type":"ops","ops":[["p",["entries"],1,0,[{"id":2,"kind":"pi.assistant","model":null,"data":{}}]]]}`,
			`{"type":"ops","ops":[["s",["entries",1,"data"],{"text":"done"}]]}`,
		} {
			if err := conn.Write(r.Context(), websocket.MessageText, []byte(frame)); err != nil {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		<-r.Context().Done()
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	frames := New(server.URL, "").Watch(ctx, 1)

	var last = receiveFrame(t, frames) // baseline, empty
	for i := 0; i < 3; i++ {
		last = receiveFrame(t, frames)
	}
	if got := len(last.Value.Entries); got != 2 {
		t.Fatalf("entries = %d, want 2", got)
	}
	if got := string(last.Value.Entries[1].Data); got != `{"text":"done"}` {
		t.Fatalf("second entry data = %s", got)
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("connections = %d, want 1", got)
	}
}
