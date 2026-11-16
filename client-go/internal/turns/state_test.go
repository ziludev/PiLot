package turns

import (
	"encoding/json"
	"testing"

	"pitta/internal/protocol"
)

func TestTurnState(t *testing.T) {
	tests := []struct {
		name     string
		finals   int
		run      bool
		text     string
		slots    []string
		want     State
		wantLive bool
	}{
		{
			name:     "between two tool calls the turn is thinking",
			run:      true,
			want:     State{Kind: StateThinking},
			wantLive: true,
		},
		{
			name: "a running tool wins over partial text and keeps both",
			run:  true,
			text: "我在看日志",
			slots: []string{
				`{"name":"read","status":"done","output":"old file"}`,
				`{"name":"bash","status":"running","output":"line one\n\n  \nnpm test\n"}`,
			},
			want:     State{Kind: StateTool, Text: "我在看日志", Tool: "bash", ToolTail: "npm test"},
			wantLive: true,
		},
		{
			name:     "text without a running tool is streaming",
			run:      true,
			text:     "答案的前半段",
			want:     State{Kind: StateStreaming, Text: "答案的前半段"},
			wantLive: true,
		},
		{
			name: "a finished conversation without an answer is idle",
			want: State{Kind: StateIdle},
		},
		{
			name:   "a finished conversation with an answer is settled",
			finals: 1,
			want:   State{Kind: StateSettled},
		},
		{
			name:   "busy with an answer already delivered is settled",
			finals: 1,
			run:    true,
			want:   State{Kind: StateSettled},
		},
		{
			name: "a live doc left over after the run ended is a ghost",
			text: "leftover",
			want: State{Kind: StateIdle},
		},
		{
			name:     "a tool that already finished does not hide thinking",
			run:      true,
			slots:    []string{`{"name":"bash","status":"done","output":"done"}`},
			want:     State{Kind: StateThinking},
			wantLive: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			turn := Turn{Finals: make([]protocol.Entry, test.finals)}
			var live *protocol.LiveDoc
			if test.run || test.text != "" || len(test.slots) > 0 {
				live = &protocol.LiveDoc{}
				if test.run {
					live.Run = json.RawMessage(`{"id":1}`)
				}
				if test.text != "" {
					raw, err := json.Marshal(map[string]any{
						"message": map[string]any{
							"role":    "assistant",
							"content": []any{map[string]any{"type": "text", "text": test.text}},
						},
					})
					if err != nil {
						t.Fatalf("encode generation: %v", err)
					}
					live.Generation = raw
				}
				for _, slot := range test.slots {
					live.Tools = append(live.Tools, json.RawMessage(slot))
				}
			}

			state := turn.State(live)
			if state != test.want {
				t.Errorf("state = %+v, want %+v", state, test.want)
			}
			if state.Live() != test.wantLive {
				t.Errorf("live = %v, want %v", state.Live(), test.wantLive)
			}
		})
	}
}

func TestLiveTextAcceptsCommittedMessageArray(t *testing.T) {
	turn := Turn{}
	state := turn.State(&protocol.LiveDoc{
		Run: json.RawMessage(`{"id":1}`),
		Generation: json.RawMessage(`{"attempt":1,"message":[` +
			`{"role":"assistant","content":[` +
			`{"type":"text","text":"half"},{"type":"text","text":" answer"}]}]}`),
	})
	want := State{Kind: StateStreaming, Text: "half answer"}
	if state != want {
		t.Errorf("state = %+v, want %+v", state, want)
	}
}

func TestRunningTool(t *testing.T) {
	slot, ok := runningTool([]json.RawMessage{
		json.RawMessage(`{"name":"read","status":"done","output":"file"}`),
		json.RawMessage(`{"name":"bash","status":"running","output":"npm test"}`),
	})
	if !ok || slot.name != "bash" || slot.tail != "npm test" {
		t.Errorf("running tool = %+v ok=%v, want bash/npm test", slot, ok)
	}

	_, ok = runningTool([]json.RawMessage{json.RawMessage(`{"name":"read","status":"done"}`)})
	if ok {
		t.Error("running tool reported for a finished slot")
	}

	if _, ok := runningTool(nil); ok {
		t.Error("running tool reported for an empty tool list")
	}
}

func TestTurnStatus(t *testing.T) {
	tests := []struct {
		name    string
		entries []protocol.Entry
		live    *protocol.LiveDoc
		want    Status
	}{
		{
			name:    "a finished answer is done",
			entries: []protocol.Entry{userEntry(1, "hi", 1_000), stopEntry(2, "stop", 1_100)},
			want:    StatusDone,
		},
		{
			name:    "the last assistant message decides",
			entries: []protocol.Entry{userEntry(1, "hi", 1_000), stopEntry(2, "stop", 1_100), stopEntry(3, "error", 1_200)},
			want:    StatusError,
		},
		{
			name:    "an aborted run is aborted",
			entries: []protocol.Entry{userEntry(1, "hi", 1_000), stopEntry(2, "aborted", 1_100)},
			want:    StatusAborted,
		},
		{
			name:    "a retry that recovered is done",
			entries: []protocol.Entry{userEntry(1, "hi", 1_000), stopEntry(2, "error", 1_100), stopEntry(3, "stop", 1_200)},
			want:    StatusDone,
		},
		{
			name:    "a live run is running even if the last message failed",
			entries: []protocol.Entry{userEntry(1, "hi", 1_000), stopEntry(2, "error", 1_100)},
			live:    &protocol.LiveDoc{Run: json.RawMessage(`{"id":1}`)},
			want:    StatusRunning,
		},
		{
			name:    "a turn without assistant messages is done",
			entries: []protocol.Entry{userEntry(1, "hi", 1_000)},
			want:    StatusDone,
		},
		{
			name:    "a ghost live doc does not hide an error",
			entries: []protocol.Entry{userEntry(1, "hi", 1_000), stopEntry(2, "error", 1_100)},
			live:    &protocol.LiveDoc{Generation: json.RawMessage(`{"message":[]}`)},
			want:    StatusError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			turns := GroupTurns(test.entries, nil)
			if len(turns) != 1 {
				t.Fatalf("turns = %d, want 1", len(turns))
			}
			if got := turns[0].Status(test.live); got != test.want {
				t.Errorf("status = %v, want %v", got, test.want)
			}
		})
	}
}

func stopEntry(id int, reason string, ts int64) protocol.Entry {
	return entry(id, "pi.assistant", []any{map[string]any{
		"role":       "assistant",
		"content":    []any{map[string]any{"type": "text", "text": reason}},
		"stopReason": reason,
		"timestamp":  ts,
	}})
}
