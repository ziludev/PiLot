package tools

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pitta/toolbox/internal/proto"
	"pitta/toolbox/internal/server"
)

// callResult is one call's outcome as the agent sees it.
type callResult struct {
	text     string
	events   []proto.Event
	result   map[string]any
	failure  *proto.Event
	spill    string
	duration time.Duration
}

// harness serves a toolbox with bash registered and returns a caller plus the
// temp dir commands run in.
func harness(t *testing.T) (func(args map[string]any) callResult, string) {
	t.Helper()
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
	srv := server.New(ctx, nil)
	RegisterBash(srv)
	done := make(chan struct{})
	go func() {
		_ = srv.Serve(listener)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		_ = listener.Close()
		srv.Close()
		<-done
	})

	work := filepath.Join(dir, "work")
	if err := os.Mkdir(work, 0o755); err != nil {
		t.Fatalf("workdir: %v", err)
	}

	return func(args map[string]any) callResult {
		conn, err := net.Dial("unix", socketPath)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		defer conn.Close()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("args: %v", err)
		}
		if err := json.NewEncoder(conn).Encode(proto.Request{Tool: "bash", Args: raw, Cwd: work, CallID: "c"}); err != nil {
			t.Fatalf("encode: %v", err)
		}
		started := time.Now()
		var events []proto.Event
		var text strings.Builder
		decoder := json.NewDecoder(conn)
		for {
			var event proto.Event
			if err := decoder.Decode(&event); err != nil {
				t.Fatalf("decode: %v", err)
			}
			events = append(events, event)
			switch event.Type {
			case proto.TypeChunk:
				text.WriteString(event.Data)
			case proto.TypeResult:
				var result map[string]any
				if err := json.Unmarshal(event.Result, &result); err != nil {
					t.Fatalf("result: %v", err)
				}
				return callResult{text: text.String(), events: events, result: result, spill: spillOf(result), duration: time.Since(started)}
			case proto.TypeError:
				return callResult{text: text.String(), events: events, failure: &event, spill: event.SpillPath, duration: time.Since(started)}
			}
		}
	}, work
}

func spillOf(result map[string]any) string {
	if path, ok := result["fullOutputPath"].(string); ok {
		return path
	}
	return ""
}

func TestBashRunsAndCapturesBothStreams(t *testing.T) {
	call, _ := harness(t)
	got := call(map[string]any{"command": `echo out; echo err >&2`})
	if got.failure != nil {
		t.Fatalf("failure = %+v", got.failure)
	}
	if !strings.Contains(got.text, "out\n") || !strings.Contains(got.text, "err\n") {
		t.Fatalf("text = %q, want both streams", got.text)
	}
	if exit := got.result["exitCode"]; exit != float64(0) {
		t.Fatalf("exitCode = %v, want 0", exit)
	}
	if got.spill != "" {
		t.Fatalf("spill = %q, want none for a small output", got.spill)
	}
}

func TestBashReportsExitCode(t *testing.T) {
	call, _ := harness(t)
	got := call(map[string]any{"command": `exit 3`})
	if got.failure != nil {
		t.Fatalf("failure = %+v", got.failure)
	}
	if exit := got.result["exitCode"]; exit != float64(3) {
		t.Fatalf("exitCode = %v, want 3", exit)
	}
}

func TestBashSpillsOnlyPastTheThreshold(t *testing.T) {
	call, _ := harness(t)

	under := call(map[string]any{"command": `printf 'x%.0s' $(seq 1 40000)`})
	if under.spill != "" {
		t.Fatalf("spill = %q, want none under 50KB", under.spill)
	}

	over := call(map[string]any{"command": `printf 'line %d\n' $(seq 1 3000)`})
	if over.spill == "" {
		t.Fatalf("no spill over 2000 lines")
	}
	defer os.Remove(over.spill)
	if !strings.HasPrefix(filepath.Base(over.spill), "pi-output-") || !strings.HasSuffix(over.spill, ".log") {
		t.Fatalf("spill = %q, want a pi-output-*.log temp file", over.spill)
	}
	saved, err := os.ReadFile(over.spill)
	if err != nil {
		t.Fatalf("read spill: %v", err)
	}
	if !strings.Contains(string(saved), "line 1\n") || !strings.Contains(string(saved), "line 3000\n") {
		t.Fatalf("spill is not complete: %d bytes", len(saved))
	}
	// The streamed text is the whole output; the agent keeps the tail itself.
	if !strings.Contains(over.text, "line 3000") {
		t.Fatalf("streamed text is missing the end")
	}
}

func TestBashKillsOnTimeout(t *testing.T) {
	call, _ := harness(t)
	got := call(map[string]any{"command": `echo started; sleep 30`, "timeout": 0.2})
	if got.failure == nil {
		t.Fatalf("call succeeded, want a timeout")
	}
	if got.failure.Code != "timeout" {
		t.Fatalf("code = %q, want timeout", got.failure.Code)
	}
	if got.duration > 5*time.Second {
		t.Fatalf("took %s: the process group was not killed", got.duration)
	}
	if !strings.Contains(got.text, "started") {
		t.Fatalf("text = %q, want output seen before the timeout", got.text)
	}
}

func TestBashKillsTheWholeProcessGroup(t *testing.T) {
	call, work := harness(t)
	marker := filepath.Join(work, "survived")
	// A background child inherits the group; killing only the shell would leave it
	// running long enough to write the marker.
	got := call(map[string]any{"command": `sh -c 'sleep 1; touch ` + marker + `' & sleep 30`, "timeout": 0.2})
	if got.failure == nil || got.failure.Code != "timeout" {
		t.Fatalf("failure = %+v, want a timeout", got.failure)
	}
	time.Sleep(1500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatalf("background child survived the kill")
	}
}

func TestBashMissingWorkdir(t *testing.T) {
	call, work := harness(t)
	missing := filepath.Join(work, "nope")
	got := call(map[string]any{"command": "true"})
	if got.failure != nil {
		t.Fatalf("baseline failed: %+v", got.failure)
	}
	// The caller swaps the cwd; run once more against a path that does not exist.
	conn, err := net.Dial("unix", filepath.Join(filepath.Dir(work), "s"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(proto.Request{Tool: "bash", Args: json.RawMessage(`{"command":"true"}`), Cwd: missing}); err != nil {
		t.Fatalf("encode: %v", err)
	}
	var event proto.Event
	if err := json.NewDecoder(conn).Decode(&event); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := "Working directory does not exist: " + missing + "\nCannot execute bash commands."
	if event.Type != proto.TypeError || event.Message != want {
		t.Fatalf("event = %+v, want %q", event, want)
	}
}

func TestBashDecodesSplitUTF8(t *testing.T) {
	call, _ := harness(t)
	// One three-byte rune written in two calls, with a pause so the reads split it.
	got := call(map[string]any{"command": `printf '\344'; sleep 0.05; printf '\270\255'`})
	if got.failure != nil {
		t.Fatalf("failure = %+v", got.failure)
	}
	if got.text != "中" {
		t.Fatalf("text = %q, want 中", got.text)
	}
}

func TestBashReplacesInvalidUTF8PerByte(t *testing.T) {
	call, _ := harness(t)
	got := call(map[string]any{"command": `printf '\377\376'`})
	if got.failure != nil {
		t.Fatalf("failure = %+v", got.failure)
	}
	if got.text != "\uFFFD\uFFFD" {
		t.Fatalf("text = %q, want two replacement characters", got.text)
	}
}

func TestBashHonoursEnvironment(t *testing.T) {
	call, _ := harness(t)
	got := call(map[string]any{"command": `printf %s "$GREETING"`, "env": map[string]string{"GREETING": "hi"}})
	if got.failure != nil {
		t.Fatalf("failure = %+v", got.failure)
	}
	if got.text != "hi" {
		t.Fatalf("text = %q, want hi", got.text)
	}
}

func TestBashRejectsBadTimeout(t *testing.T) {
	call, _ := harness(t)
	for _, timeout := range []float64{0, -1} {
		got := call(map[string]any{"command": "true", "timeout": timeout})
		if got.failure == nil {
			t.Fatalf("timeout %v accepted", timeout)
		}
	}
}
