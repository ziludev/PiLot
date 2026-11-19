package app

import (
	"encoding/json"
	"testing"
	"time"

	"pitta/internal/protocol"
)

func TestInboxText(t *testing.T) {
	for _, c := range []struct {
		name string
		raw  string
		want string
	}{
		{"bare string", `"check the build too"`, "check the build too"},
		{"parts", `[{"type":"text","text":"check the"},{"type":"image","data":"..."},{"type":"text","text":"build"}]`, "check the build"},
		{"object", `{"parts":[{"type":"text","text":"hi"}]}`, "hi"},
		{"blank", `[]`, ""},
		{"missing", ``, ""},
	} {
		if got := inboxText(json.RawMessage(c.raw)); got != c.want {
			t.Errorf("%s: inboxText = %q, want %q", c.name, got, c.want)
		}
	}
}

// A run in flight is timed from the task that carries it, not from when this
// window happened to hear about it.
func TestRunSoFar(t *testing.T) {
	a := newApp(Config{})
	a.now = time.Now()
	a.active = 11
	a.thread(11).live.Run = json.RawMessage(`{"taskId":9001}`)
	a.tasks = []protocol.Task{
		{ID: 9000, State: "working", StartedAt: a.now.Add(-time.Hour).UnixMilli()},
		{ID: 9001, State: "working", StartedAt: a.now.Add(-92 * time.Second).UnixMilli()},
	}

	elapsed, ok := a.runSoFar()
	if !ok {
		t.Fatal("run in flight reported no elapsed time")
	}
	if got := shortDuration(elapsed); got != "1m32s" {
		t.Fatalf("runSoFar = %s, want 1m32s", got)
	}

	a.thread(11).live.Run = nil
	if _, ok := a.runSoFar(); ok {
		t.Fatal("idle chat reported a running turn")
	}
}
