package server

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"pitta/toolbox/internal/proto"
)

// start serves a server on a socket in a temp dir and returns a dialer plus a
// cleanup that stops everything.
func start(t *testing.T) (*Server, func() net.Conn) {
	t.Helper()
	// Unix socket paths are capped at ~104 bytes on macOS, and t.TempDir() already
	// spends most of that on the test name: keep the directory short and the file
	// one character.
	dir, err := os.MkdirTemp("", "tb")
	if err != nil {
		t.Fatalf("tempdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socketPath := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	srv := New(ctx, log.New(os.Stderr, "", 0))
	srv.Register("ping", func(ctx context.Context, req proto.Request, emit Emit) (any, error) {
		return map[string]any{"pid": 1}, nil
	})
	srv.Register("echo", func(ctx context.Context, req proto.Request, emit Emit) (any, error) {
		emit(proto.Event{Type: proto.TypeChunk, Data: "first"})
		emit(proto.Event{Type: proto.TypeDiagnostic, Severity: proto.SeverityInfo, Code: "note", Message: "mid"})
		var args map[string]any
		if err := json.Unmarshal(req.Args, &args); err != nil {
			return nil, err
		}
		return args, nil
	})
	srv.Register("fail", func(ctx context.Context, req proto.Request, emit Emit) (any, error) {
		emit(proto.Event{Type: proto.TypeChunk, Data: "partial"})
		return nil, os.ErrPermission
	})

	done := make(chan struct{})
	go func() {
		if err := srv.Serve(listener); err != nil {
			t.Errorf("serve: %v", err)
		}
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		srv.Close()
		<-done
	})
	return srv, func() net.Conn {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return conn
	}
}

// readAll collects the events of one call until its terminal event.
func readAll(t *testing.T, conn net.Conn) []proto.Event {
	t.Helper()
	decoder := json.NewDecoder(conn)
	var events []proto.Event
	for {
		var event proto.Event
		if err := decoder.Decode(&event); err != nil {
			t.Fatalf("decode: %v", err)
		}
		events = append(events, event)
		if event.Type == proto.TypeResult || event.Type == proto.TypeError {
			return events
		}
	}
}

func send(t *testing.T, conn net.Conn, req proto.Request) {
	t.Helper()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		t.Fatalf("encode: %v", err)
	}
}

func TestPingRoundTrip(t *testing.T) {
	_, dial := start(t)
	conn := dial()
	send(t, conn, proto.Request{Tool: "ping", CallID: "c1"})
	events := readAll(t, conn)
	if len(events) != 1 || events[0].Type != proto.TypeResult {
		t.Fatalf("events = %+v, want one result", events)
	}
	var result map[string]any
	if err := json.Unmarshal(events[0].Result, &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	if result["pid"] != float64(1) {
		t.Fatalf("result = %v", result)
	}
}

// A streamed call keeps its order: chunks and diagnostics arrive before the
// terminal event, because the agent appends them to api.output() as they come.
func TestStreamedEventsKeepOrder(t *testing.T) {
	_, dial := start(t)
	conn := dial()
	send(t, conn, proto.Request{Tool: "echo", Args: json.RawMessage(`{"text":"hi"}`)})
	events := readAll(t, conn)
	if len(events) != 3 {
		t.Fatalf("events = %+v, want chunk, diagnostic, result", events)
	}
	if events[0].Type != proto.TypeChunk || events[0].Data != "first" {
		t.Fatalf("first = %+v", events[0])
	}
	if events[1].Type != proto.TypeDiagnostic || events[1].Code != "note" {
		t.Fatalf("second = %+v", events[1])
	}
	if events[2].Type != proto.TypeResult {
		t.Fatalf("third = %+v", events[2])
	}
}

func TestHandlerErrorBecomesErrorEvent(t *testing.T) {
	_, dial := start(t)
	conn := dial()
	send(t, conn, proto.Request{Tool: "fail"})
	events := readAll(t, conn)
	if len(events) != 2 || events[1].Type != proto.TypeError {
		t.Fatalf("events = %+v, want chunk then error", events)
	}
	if events[1].Message != os.ErrPermission.Error() {
		t.Fatalf("message = %q", events[1].Message)
	}
}

func TestUnknownTool(t *testing.T) {
	_, dial := start(t)
	conn := dial()
	send(t, conn, proto.Request{Tool: "nope"})
	events := readAll(t, conn)
	if len(events) != 1 || events[0].Type != proto.TypeError {
		t.Fatalf("events = %+v", events)
	}
}

// Calls run on their own connections and must not serialise: the harness runs
// up to four tool calls in parallel.
func TestConcurrentCalls(t *testing.T) {
	_, dial := start(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn := dial()
			send(t, conn, proto.Request{Tool: "ping"})
			events := readAll(t, conn)
			if len(events) != 1 || events[0].Type != proto.TypeResult {
				t.Errorf("events = %+v", events)
			}
		}()
	}
	wg.Wait()
}

// A request too large for a Scanner buffer must still arrive: `write` sends
// whole files through this path.
func TestLargeRequest(t *testing.T) {
	_, dial := start(t)
	conn := dial()
	content := make([]byte, 512*1024)
	for i := range content {
		content[i] = 'a'
	}
	args, err := json.Marshal(map[string]string{"text": string(content)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	send(t, conn, proto.Request{Tool: "echo", Args: args})
	events := readAll(t, conn)
	if len(events) != 3 || events[2].Type != proto.TypeResult {
		t.Fatalf("events = %+v", events)
	}
	var result map[string]any
	if err := json.Unmarshal(events[2].Result, &result); err != nil {
		t.Fatalf("result: %v", err)
	}
	if got := len(result["text"].(string)); got != len(content) {
		t.Fatalf("len = %d, want %d", got, len(content))
	}
}

func TestRegisterTwicePanics(t *testing.T) {
	srv := New(context.Background(), log.New(os.Stderr, "", 0))
	srv.Register("ping", func(ctx context.Context, req proto.Request, emit Emit) (any, error) { return nil, nil })
	defer func() {
		if recover() == nil {
			t.Fatal("want a panic on a duplicate registration")
		}
	}()
	srv.Register("ping", func(ctx context.Context, req proto.Request, emit Emit) (any, error) { return nil, nil })
}
