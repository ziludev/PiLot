package app

import (
	"encoding/json"
	"strings"
	"testing"

	"pilot/internal/i18n"

	"github.com/egoist/mygo/ui"
)

// TestLiveHintReadsWhatTheCallIsDoing: a turn runs `bash` twenty times, so the
// running row has to say what this one is doing — otherwise every turn looks
// like the same bash.
func TestLiveHintReadsWhatTheCallIsDoing(t *testing.T) {
	cases := []struct {
		name    string
		output  string
		details string
		want    string
	}{
		{name: "command from details", details: `{"command":"npm test"}`, want: "npm test"},
		{name: "path from details", details: `{"path":"src/steps.ts"}`, want: "src/steps.ts"},
		{name: "details beat output", details: `{"command":"go test ./..."}`, output: "ok", want: "go test ./..."},
		{name: "tail of the output", output: "line one\n# pass 321\n", want: "# pass 321"},
		{name: "trailing blank lines skipped", output: "building\n\n  \n", want: "building"},
		{name: "nothing to say", want: ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var details json.RawMessage
			if c.details != "" {
				details = json.RawMessage(c.details)
			}
			if got := liveStepHint(c.output, details); got != c.want {
				t.Fatalf("hint = %q, want %q", got, c.want)
			}
		})
	}
}

// TestLiveHintIsOneShortLine: the hint shares a row with 「正在跑」 and the
// tool's name, so it is cut rather than wrapped.
func TestLiveHintIsOneShortLine(t *testing.T) {
	hint := liveStepHint(strings.Repeat("a", 200)+"\n", nil)
	if len([]rune(hint)) != liveHintRunes {
		t.Fatalf("hint has %d runes, want %d", len([]rune(hint)), liveHintRunes)
	}
	if !strings.HasSuffix(hint, "…") {
		t.Fatalf("hint = %q, want it to end in a cut mark", hint)
	}
	if got := liveStepHint("go test  ./...\n\n", nil); got != "go test ./..." {
		t.Fatalf("whitespace squeezed wrong: %q", got)
	}
}

// TestRunningRowNamesTheToolAndWhatItDoes: the row is drawn from pi.live, where
// the slot carries the streamed output and no arguments.
func TestRunningRowNamesTheToolAndWhatItDoes(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	th := a.thread(11)
	th.live.Run = json.RawMessage(`{"taskId":9001}`)
	th.live.Tools = []json.RawMessage{
		json.RawMessage(`{"callId":"c1","name":"bash","status":"done","output":"ls\n"}`),
		json.RawMessage(`{"callId":"c2","name":"bash","status":"running","output":"# pass 321\n# fail 0\n"}`),
	}

	row, ok := a.liveRow()
	if !ok {
		t.Fatal("live row missing while a call is running")
	}
	if row.liveStep != "bash" || row.liveHint != "# fail 0" {
		t.Fatalf("step = %q hint = %q", row.liveStep, row.liveHint)
	}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("正在跑 bash · # fail 0") {
		t.Fatalf("running row does not say what bash is doing; drew %v", tt.Texts())
	}
}
