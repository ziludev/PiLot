package turns

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pitta/internal/protocol"
)

func TestGroupTurns(t *testing.T) {
	diff := "diff --git a/src/main.rs b/src/main.rs\n" +
		"--- a/src/main.rs\n" +
		"+++ b/src/main.rs\n" +
		"@@ -1,2 +1,2 @@\n" +
		"-old\n" +
		"+new\n" +
		" context\n"

	tests := []struct {
		name    string
		entries []protocol.Entry
		waits   []protocol.ApprovalWait
		check   func(t *testing.T, turns []Turn)
	}{
		{
			name: "machinery collapses into one turn",
			entries: []protocol.Entry{
				userEntry(1, "帮我改一下", 1_000),
				callEntry(2, "c1", "read", map[string]any{"path": "src/main.rs"}, 1_500),
				resultEntry(3, "c1", diff, false, 2_000),
				assistantText(4, "改好了", 2_500),
				userEntry(5, "再来一个", 3_000),
				assistantText(6, "第二个答案", 3_500),
			},
			check: func(t *testing.T, turns []Turn) {
				t.Helper()
				if len(turns) != 2 {
					t.Fatalf("turns = %d, want 2", len(turns))
				}
				first := turns[0]
				// The earlier web client makes a step of every assistant message, so
				// the answer is a think step too (the Rust port dropped it).
				checkInt(t, "first turn steps", len(first.Steps), 2)
				checkInt(t, "first step id", first.Steps[0].ID, 2)
				if first.Steps[0].Kind != StepTool {
					t.Errorf("first step kind = %v, want tool", first.Steps[0].Kind)
				}
				if !strings.HasPrefix(first.Steps[0].Label, "read · src/main.rs") {
					t.Errorf("first step label = %q, want read prefix", first.Steps[0].Label)
				}
				checkInt64(t, "first step start", first.Steps[0].Start, 1_500)
				checkInt64(t, "first step end", first.Steps[0].End, 2_000)
				if first.Steps[0].Result == nil || first.Steps[0].Result.ID != 3 {
					t.Errorf("first step result = %+v, want entry 3", first.Steps[0].Result)
				}
				if first.Steps[1].Kind != StepThink || first.Steps[1].Label != "思考" {
					t.Errorf("answer step = kind %v label %q, want think/思考", first.Steps[1].Kind, first.Steps[1].Label)
				}
				checkInt64(t, "answer step start", first.Steps[1].Start, 2_500)
				checkInt64(t, "answer step end", first.Steps[1].End, 2_500)
				checkInt(t, "first turn tools", first.Stats.Tools, 1)
				checkInt(t, "first turn files", first.Stats.Files, 1)
				checkInt(t, "first turn added", first.Stats.Added, 1)
				checkInt(t, "first turn removed", first.Stats.Removed, 1)
				checkInt(t, "first turn finals", len(first.Finals), 1)
				checkInt(t, "first turn internals", len(first.Internals), 2)
				checkInt64Ptr(t, "first turn start", first.Stats.Start, 1_000)
				checkInt64Ptr(t, "first turn end", first.Stats.End, 2_500)
				checkInt64Ptr(t, "first turn ms", first.Stats.MS, 1_500)

				second := turns[1]
				checkInt(t, "second turn steps", len(second.Steps), 1)
				checkInt(t, "second turn finals", len(second.Finals), 1)
				checkInt64Ptr(t, "second turn ms", second.Stats.MS, 500)
			},
		},
		{
			name: "thinking-only assistant becomes a step",
			entries: []protocol.Entry{
				userEntry(1, "why", 1_000),
				thinkingEntry(2, "hmm", 1_100),
				assistantText(3, "because", 1_200),
			},
			check: func(t *testing.T, turns []Turn) {
				t.Helper()
				checkInt(t, "internals", len(turns[0].Internals), 1)
				checkInt(t, "finals", len(turns[0].Finals), 1)
				checkInt(t, "steps", len(turns[0].Steps), 2)
				if turns[0].Steps[0].Kind != StepThink || turns[0].Steps[1].Kind != StepThink {
					t.Errorf("step kinds = %v/%v, want think/think", turns[0].Steps[0].Kind, turns[0].Steps[1].Kind)
				}
				// A non-empty thinking text always carries the k suffix, even
				// when it rounds to 0.0.
				if turns[0].Steps[0].Label != "思考 · 0.0k" {
					t.Errorf("thinking label = %q, want 思考 · 0.0k", turns[0].Steps[0].Label)
				}
				checkInt64(t, "thinking step start", turns[0].Steps[0].Start, 1_100)
				checkInt64(t, "thinking step end", turns[0].Steps[0].End, 1_200)
				checkInt64(t, "phases think", turns[0].Stats.Phases.Think, 100)
				checkInt64Ptr(t, "turn ms", turns[0].Stats.MS, 200)
			},
		},
		{
			name: "step label flattens whitespace",
			entries: []protocol.Entry{
				userEntry(1, "run it", 1_000),
				callEntry(2, "c1", "bash", map[string]any{"command": "pnpm   test\n--watch"}, 1_100),
			},
			check: func(t *testing.T, turns []Turn) {
				t.Helper()
				if got, want := turns[0].Steps[0].Label, "bash · pnpm test --watch"; got != want {
					t.Errorf("label = %q, want %q", got, want)
				}
			},
		},
		{
			name: "thinking label rounds halves up",
			entries: []protocol.Entry{
				userEntry(1, "why", 1_000),
				thinkingEntry(2, strings.Repeat("a", 1_250), 1_100),
			},
			check: func(t *testing.T, turns []Turn) {
				t.Helper()
				if got, want := turns[0].Steps[0].Label, "思考 · 1.3k"; got != want {
					t.Errorf("label = %q, want %q", got, want)
				}
			},
		},
		{
			name: "notes and unknown kinds stay in their turns",
			entries: []protocol.Entry{
				noteEntry(1),
				{ID: 2, Kind: "pi.system"},
				userEntry(3, "hello", 1_000),
				noteEntry(4),
				{ID: 5, Kind: "app.mystery", Model: json.RawMessage(`[]`)},
				{ID: 6, Kind: "pi.compaction"},
				reportEntry(7),
				userEntry(8, "again", 2_000),
			},
			check: func(t *testing.T, turns []Turn) {
				t.Helper()
				if len(turns) != 4 {
					t.Fatalf("turns = %d, want 4", len(turns))
				}
				if turns[0].User != nil || len(turns[0].Notes) != 1 || turns[0].Notes[0].ID != 1 {
					t.Errorf("note turn = %+v, want a note-only turn", turns[0])
				}
				if turns[1].User == nil || turns[1].User.ID != 3 {
					t.Errorf("second turn user = %+v, want entry 3", turns[1].User)
				}
				if len(turns[1].Notes) != 1 || turns[1].Notes[0].ID != 4 {
					t.Errorf("second turn notes = %+v, want entry 4", turns[1].Notes)
				}
				if len(turns[1].Finals) != 1 || turns[1].Finals[0].ID != 5 {
					t.Errorf("second turn finals = %+v, want entry 5", turns[1].Finals)
				}
				if turns[2].User != nil || len(turns[2].Reports) != 1 || turns[2].Reports[0].ID != 7 {
					t.Errorf("report turn = %+v, want a report turn", turns[2])
				}
				if turns[3].User == nil || turns[3].User.ID != 8 {
					t.Errorf("fourth turn user = %+v, want entry 8", turns[3].User)
				}

				kept := map[int]bool{}
				for _, turn := range turns {
					kept[turn.Anchor.ID] = true
					if turn.User != nil {
						kept[turn.User.ID] = true
					}
					for _, entries := range [][]protocol.Entry{turn.Finals, turn.Reports, turn.Notes, turn.Internals} {
						for _, entry := range entries {
							kept[entry.ID] = true
						}
					}
				}
				for _, id := range []int{1, 3, 4, 5, 7, 8} {
					if !kept[id] {
						t.Errorf("entry %d was dropped", id)
					}
				}
				if kept[2] || kept[6] {
					t.Errorf("bookkeeping entries were kept: %v", kept)
				}
			},
		},
		{
			name: "approval waits count only their overlap",
			entries: []protocol.Entry{
				userEntry(1, "wait", 1_000),
				assistantText(2, "done", 5_000),
			},
			waits: []protocol.ApprovalWait{
				{Start: 2_000, End: 3_000, Tool: "bash"},
				{Start: 4_000, End: 6_000, Tool: "bash"},
				{Start: 0, End: 500, Tool: "bash"},
			},
			check: func(t *testing.T, turns []Turn) {
				t.Helper()
				checkInt64(t, "phases wait", turns[0].Stats.Phases.Wait, 2_000)
			},
		},
		{
			name: "a turn without timestamps has no start, end or ms",
			entries: []protocol.Entry{
				noteEntry(1),
			},
			waits: []protocol.ApprovalWait{{Start: 0, End: 10_000}},
			check: func(t *testing.T, turns []Turn) {
				t.Helper()
				if turns[0].Stats.Start != nil || turns[0].Stats.End != nil || turns[0].Stats.MS != nil {
					t.Errorf("stats timing = %+v, want nil start/end/ms", turns[0].Stats)
				}
				checkInt64(t, "phases wait", turns[0].Stats.Phases.Wait, 0)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.check(t, GroupTurns(test.entries, test.waits))
		})
	}
}

func TestClock(t *testing.T) {
	tests := []struct {
		ms   int64
		want string
	}{
		{42, "42ms"},
		{999, "999ms"},
		{1_000, "1.0s"},
		{1_250, "1.3s"},
		{3_700, "3.7s"},
		{9_999, "10.0s"},
		{10_000, "10s"},
		{42_000, "42s"},
		{59_999, "60s"},
		{60_000, "1m"},
		{90_000, "1m30s"},
		{120_000, "2m"},
	}
	for _, test := range tests {
		if got := Clock(test.ms); got != test.want {
			t.Errorf("Clock(%d) = %q, want %q", test.ms, got, test.want)
		}
	}
}

type wantStep struct {
	id        int
	kind      StepKind
	start     int64
	end       int64
	hasResult bool
}

type wantStats struct {
	tools   int
	files   int
	added   int
	removed int
	tokens  int
	cost    float64
	think   int64
	tool    int64
	errors  int64
	wait    int64
	start   int64
	end     int64
	ms      int64
}

func TestGroupTurnsRealTranscripts(t *testing.T) {
	tests := []struct {
		file      string
		turnID    int
		finals    int
		internals int
		steps     []wantStep
		stats     wantStats
		status    Status
	}{
		{
			file:      "turn.json",
			turnID:    123,
			finals:    1,
			internals: 4,
			steps: []wantStep{
				{127, StepError, 1_794_195_672_168, 1_794_195_673_627, true},
				{132, StepTool, 1_794_195_673_635, 1_794_195_674_992, true},
				{137, StepThink, 1_794_195_674_996, 1_794_195_674_996, false},
			},
			stats: wantStats{
				tools: 2, tokens: 12_214, cost: 0.0019354019999999997,
				tool: 1_357, errors: 1_459,
				start: 1_794_195_672_122, end: 1_794_195_674_996, ms: 2_874,
			},
			status: StatusDone,
		},
		{
			file:      "patch-turn.json",
			turnID:    648,
			finals:    1,
			internals: 12,
			steps: []wantStep{
				{652, StepTool, 1_794_223_244_585, 1_794_223_246_880, true},
				{657, StepTool, 1_794_223_246_889, 1_794_223_249_428, true},
				{662, StepTool, 1_794_223_249_433, 1_794_223_252_022, true},
				{667, StepTool, 1_794_223_252_026, 1_794_223_254_780, true},
				{672, StepTool, 1_794_223_254_785, 1_794_223_256_752, true},
				{677, StepTool, 1_794_223_256_756, 1_794_223_259_285, true},
				{682, StepThink, 1_794_223_259_290, 1_794_223_259_290, false},
			},
			stats: wantStats{
				tools: 6, added: 1, removed: 4, tokens: 191_597, cost: 0.029376887999999997,
				tool:  14_673,
				start: 1_794_223_244_566, end: 1_794_223_259_290, ms: 14_724,
			},
			status: StatusDone,
		},
		{
			file:      "path-turn.json",
			turnID:    19,
			finals:    1,
			internals: 3,
			steps: []wantStep{
				{23, StepTool, 1_794_194_334_200, 1_794_194_335_592, true},
				{31, StepThink, 1_794_194_335_595, 1_794_194_335_595, false},
			},
			stats: wantStats{
				tools: 2, files: 1, tokens: 3_232, cost: 0.00056265,
				tool:  1_392,
				start: 1_794_194_334_134, end: 1_794_194_335_595, ms: 1_461,
			},
			status: StatusDone,
		},
		{
			file:      "failed-turn.json",
			turnID:    23_975,
			finals:    0,
			internals: 4,
			steps: []wantStep{
				{23_979, StepThink, 1_794_580_544_710, 1_794_580_657_413, false},
				{23_981, StepThink, 1_794_580_657_413, 1_794_580_664_720, false},
				{23_983, StepThink, 1_794_580_664_720, 1_794_580_675_997, false},
				{23_985, StepThink, 1_794_580_675_997, 1_794_580_675_997, false},
			},
			stats: wantStats{
				think: 131_287,
				start: 1_794_580_544_515, end: 1_794_580_675_997, ms: 131_482,
			},
			status: StatusError,
		},
		{
			file:      "aborted-turn.json",
			turnID:    14_699,
			finals:    0,
			internals: 11,
			steps: []wantStep{
				{14_703, StepTool, 1_794_484_925_004, 1_794_484_927_794, true},
				{14_708, StepTool, 1_794_484_927_797, 1_794_484_930_420, true},
				{14_713, StepTool, 1_794_484_930_426, 1_794_484_932_610, true},
				{14_718, StepTool, 1_794_484_932_615, 1_794_484_934_387, true},
				{14_723, StepTool, 1_794_484_934_394, 1_794_484_936_781, true},
				{14_728, StepTool, 1_794_484_936_787, 1_794_484_936_787, false},
			},
			stats: wantStats{
				tools: 6, tokens: 141_621, cost: 0.02136591,
				tool:  11_756,
				start: 1_794_484_924_935, end: 1_794_484_936_787, ms: 11_852,
			},
			status: StatusAborted,
		},
	}

	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			turns := GroupTurns(loadEntries(t, test.file), nil)
			if len(turns) != 1 {
				t.Fatalf("turns = %d, want 1", len(turns))
			}
			turn := turns[0]
			checkInt(t, "turn id", turn.ID, test.turnID)
			if turn.User == nil || turn.User.ID != test.turnID {
				t.Errorf("user = %+v, want entry %d", turn.User, test.turnID)
			}
			checkInt(t, "finals", len(turn.Finals), test.finals)
			checkInt(t, "internals", len(turn.Internals), test.internals)
			checkInt(t, "steps", len(turn.Steps), len(test.steps))
			for i, want := range test.steps {
				if i >= len(turn.Steps) {
					break
				}
				step := turn.Steps[i]
				checkInt(t, "step id", step.ID, want.id)
				if step.Kind != want.kind {
					t.Errorf("step %d kind = %v, want %v", step.ID, step.Kind, want.kind)
				}
				checkInt64(t, "step start", step.Start, want.start)
				checkInt64(t, "step end", step.End, want.end)
				checkInt(t, "step entry id", step.Entry.ID, want.id)
				if (step.Result != nil) != want.hasResult {
					t.Errorf("step %d result = %+v, want present=%v", step.ID, step.Result, want.hasResult)
				}
			}

			checkInt(t, "tools", turn.Stats.Tools, test.stats.tools)
			checkInt(t, "files", turn.Stats.Files, test.stats.files)
			checkInt(t, "added", turn.Stats.Added, test.stats.added)
			checkInt(t, "removed", turn.Stats.Removed, test.stats.removed)
			checkInt(t, "tokens", turn.Stats.Tokens, test.stats.tokens)
			if math.Abs(turn.Stats.Cost-test.stats.cost) > 1e-12 {
				t.Errorf("cost = %v, want %v", turn.Stats.Cost, test.stats.cost)
			}
			checkInt64(t, "phases think", turn.Stats.Phases.Think, test.stats.think)
			checkInt64(t, "phases tool", turn.Stats.Phases.Tool, test.stats.tool)
			checkInt64(t, "phases error", turn.Stats.Phases.Error, test.stats.errors)
			checkInt64(t, "phases wait", turn.Stats.Phases.Wait, test.stats.wait)
			checkInt64Ptr(t, "start", turn.Stats.Start, test.stats.start)
			checkInt64Ptr(t, "end", turn.Stats.End, test.stats.end)
			checkInt64Ptr(t, "ms", turn.Stats.MS, test.stats.ms)
			if got := turn.Status(nil); got != test.status {
				t.Errorf("status = %v, want %v", got, test.status)
			}
		})
	}
}

func TestStepLabelTruncatesTo46Units(t *testing.T) {
	entries := []protocol.Entry{
		userEntry(1, "write it", 1_000),
		callEntry(2, "c1", "write", map[string]any{"path": "/Users/alexwang/learn/pi-durable-agent/workspace/note.md"}, 1_100),
	}
	turns := GroupTurns(entries, nil)
	const want = "write · /Users/alexwang/learn/pi-durable-agent/workspa"
	if got := turns[0].Steps[0].Label; got != want {
		t.Errorf("label = %q, want %q", got, want)
	}
}

func loadEntries(t *testing.T, name string) []protocol.Entry {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var entries []protocol.Entry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return entries
}

func entry(id int, kind string, model any) protocol.Entry {
	raw, err := json.Marshal(model)
	if err != nil {
		panic(err)
	}
	return protocol.Entry{ID: id, Kind: kind, Model: raw}
}

func userEntry(id int, text string, ts int64) protocol.Entry {
	return entry(id, "pi.user", []any{map[string]any{
		"role":      "user",
		"content":   []any{map[string]any{"type": "text", "text": text}},
		"timestamp": ts,
	}})
}

func assistantText(id int, text string, ts int64) protocol.Entry {
	return entry(id, "pi.assistant", []any{map[string]any{
		"role":      "assistant",
		"content":   []any{map[string]any{"type": "text", "text": text}},
		"timestamp": ts,
	}})
}

func thinkingEntry(id int, text string, ts int64) protocol.Entry {
	return entry(id, "pi.assistant", []any{map[string]any{
		"role":      "assistant",
		"content":   []any{map[string]any{"type": "thinking", "thinking": text}},
		"timestamp": ts,
	}})
}

func callEntry(id int, callID, name string, args map[string]any, ts int64) protocol.Entry {
	return entry(id, "pi.assistant", []any{map[string]any{
		"role": "assistant",
		"content": []any{map[string]any{
			"type":      "toolCall",
			"id":        callID,
			"name":      name,
			"arguments": args,
		}},
		"timestamp": ts,
	}})
}

func resultEntry(id int, callID, text string, isError bool, ts int64) protocol.Entry {
	return entry(id, "pi.tool-result", []any{map[string]any{
		"role":       "toolResult",
		"toolCallId": callID,
		"toolName":   "bash",
		"content":    []any{map[string]any{"type": "text", "text": text}},
		"isError":    isError,
		"timestamp":  ts,
	}})
}

func noteEntry(id int) protocol.Entry {
	return protocol.Entry{ID: id, Kind: "app.note", Data: json.RawMessage(`{"text":"restarted"}`)}
}

func reportEntry(id int) protocol.Entry {
	return protocol.Entry{ID: id, Kind: "app.feature-report", Data: json.RawMessage(`{"text":"report"}`)}
}

func checkInt(t *testing.T, what string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", what, got, want)
	}
}

func checkInt64(t *testing.T, what string, got, want int64) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", what, got, want)
	}
}

func checkInt64Ptr(t *testing.T, what string, got *int64, want int64) {
	t.Helper()
	if got == nil {
		t.Errorf("%s = nil, want %d", what, want)
		return
	}
	if *got != want {
		t.Errorf("%s = %d, want %d", what, *got, want)
	}
}
