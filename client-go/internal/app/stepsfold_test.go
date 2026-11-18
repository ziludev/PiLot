package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// longTurnEntries is the turn the folded line exists for: a dozen calls over
// forty seconds, one of them failed, five files touched, and a verdict at the
// end. It is the shape a reader scrolls past and a line has to survive.
func longTurnEntries() []protocol.Entry {
	entries := []protocol.Entry{
		{ID: 1, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "把客户端的三处改动做完"})},
		{ID: 2, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "timestamp": float64(1_000),
			"content": []any{map[string]any{"type": "thinking", "thinking": "先看现有实现"}},
		})},
	}
	call := func(id int, at int64, name, callID string, args map[string]any) protocol.Entry {
		return protocol.Entry{ID: id, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "timestamp": float64(at),
			"content": []any{map[string]any{"type": "toolCall", "id": callID, "name": name, "arguments": args}},
		})}
	}
	result := func(id int, at int64, callID, name, text string, isError bool) protocol.Entry {
		message := map[string]any{
			"role": "toolResult", "toolCallId": callID, "toolName": name, "timestamp": float64(at),
			"content": []any{map[string]any{"type": "text", "text": text}},
		}
		if isError {
			message["isError"] = true
		}
		return protocol.Entry{ID: id, Kind: "pi.tool-result", Model: rawModel(message)}
	}
	patch := func(path string, added, removed int) string {
		var b strings.Builder
		b.WriteString("diff --git a/" + path + " b/" + path + "\n--- a/" + path + "\n+++ b/" + path + "\n@@ -1 +1 @@\n")
		for i := 0; i < added; i++ {
			b.WriteString("+line " + strconv.Itoa(i) + "\n")
		}
		for i := 0; i < removed; i++ {
			b.WriteString("-line " + strconv.Itoa(i) + "\n")
		}
		return b.String()
	}

	// Six files, +38 −12, forty-one seconds, one failure in the middle.
	steps := []struct {
		name, callID, target, text string
		failed                     bool
		at                         int64
	}{
		{"read", "c1", "internal/app/transcript.go", "package app", false, 2_000},
		{"read", "c2", "internal/app/rail.go", "package app", false, 4_000},
		{"bash", "c3", "", "ok", false, 6_000},
		{"edit", "c4", "internal/app/transcript_rows.go", patch("internal/app/transcript_rows.go", 18, 4), false, 9_000},
		{"bash", "c5", "", "FAIL: TestHalfWritten", true, 13_000},
		{"edit", "c6", "internal/app/app.go", patch("internal/app/app.go", 8, 3), false, 17_000},
		{"edit", "c7", "internal/app/rail.go", patch("internal/app/rail.go", 12, 5), false, 21_000},
		{"bash", "c8", "", "ok", false, 25_000},
		{"write", "c9", "internal/i18n/dict.go", "ok", false, 29_000},
		{"edit", "c10", "internal/api/client.go", "ok", false, 33_000},
		{"bash", "c11", "", "ok", false, 36_000},
		{"edit", "c12", "src/server.ts", "ok", false, 38_000},
	}
	entry := 3
	for _, step := range steps {
		args := map[string]any{"command": "go test ./..."}
		if step.target != "" {
			args = map[string]any{"path": step.target}
		}
		entries = append(entries,
			call(entry, step.at, step.name, step.callID, args),
			result(entry+1, step.at+300, step.callID, step.name, step.text, step.failed),
		)
		entry += 2
	}
	entries = append(entries,
		call(entry, 40_000, "report_done", "c13", map[string]any{"summary": "done"}),
		result(entry+1, 41_500, "c13", "report_done", "done", false),
		protocol.Entry{ID: entry + 2, Kind: "app.feature-report", Model: rawModel(map[string]any{
			"summary": "三处改动都做完了",
		})},
	)
	return entries
}

// stepsFoldLine is the line the two snapshot tests below look for: the count of
// steps, not of tool calls.
func foldedLine(count int) string {
	return strconv.Itoa(count) + " " + i18n.T("stepsCount")
}

// TestSnapshotTurnStepsFold draws a twelve-step turn folded into one line. The
// line has to say how much work sits under it, the failing step has to stay a
// red row of its own, and the answer has to be right under both.
func TestSnapshotTurnStepsFold(t *testing.T) {
	a := transcriptApp(longTurnEntries()...)
	// The app reads the language out of its preferences when it is built, so
	// the switch has to come after it.
	i18n.Set(i18n.ZH)
	if got := len(a.groupedTurns()); got < 1 {
		t.Fatal("the entries built no turn")
	}
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.transcript(c) }, 860, 1180)
	tt.Frame()

	line := foldedLine(14) // twelve calls, the think between them, the verdict
	if !tt.HasText(line) {
		t.Fatalf("the turn did not fold; texts=%q", strings.Join(tt.Texts(), " | "))
	}
	for _, want := range []string{"7 files", "+38", "−12", "41s", "FAIL: TestHalfWritten"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q; texts=%q", want, strings.Join(tt.Texts(), " | "))
		}
	}
	// The calls are under the line, and the summary under them would repeat it.
	for _, gone := range []string{"read · internal/app/transcript.go", "12 tools"} {
		if tt.HasText(gone) {
			t.Errorf("folded turn still drew %q", gone)
		}
	}
	// The folded picture on its own: the frame above is tall enough for the
	// steps to open inside it, and a turn that folds should not leave a page of
	// white under it.
	shot := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.transcript(c) }, 860, 470)
	shot.Frame()
	writePNG(t, "/tmp/pilot-steps-folded.png", shot.Image())

	// One click opens the steps themselves, with the summary and its file list
	// back where they were.
	if err := tt.Click(line); err != nil {
		t.Fatalf("the folded line could not be clicked: %v", err)
	}
	// The rows slide in; let them land before looking at the picture.
	for i := 0; i < 8; i++ {
		tt.Advance(120 * time.Millisecond)
		tt.Frame()
	}
	for _, want := range []string{"read · internal/app/transcript.go", "12 tools", "7 files"} {
		if !tt.HasText(want) {
			t.Errorf("opening the steps did not bring back %q; texts=%q", want, strings.Join(tt.Texts(), " | "))
		}
	}
	writePNG(t, "/tmp/pilot-steps-open.png", tt.Image())

	// And the same click closes them again.
	if err := tt.Click(line); err != nil {
		t.Fatalf("the open line could not be clicked: %v", err)
	}
	tt.Advance(600 * time.Millisecond)
	tt.Frame()
	if tt.HasText("read · internal/app/transcript.go") {
		t.Fatalf("the steps did not fold back; texts=%q", strings.Join(tt.Texts(), " | "))
	}
}

// TestShortTurnKeepsItsSteps is the fold's floor: three steps read as three
// steps, and a line about them would only be a wrapper.
func TestShortTurnKeepsItsSteps(t *testing.T) {
	entries := toolRunEntries() // three calls
	a := transcriptApp(entries...)
	i18n.Set(i18n.ZH)
	tt := ui.NewTester(func(c *ui.Context) { theme.Apply(c, theme.Light()); a.transcript(c) }, 820, 480)
	tt.Frame()

	if tt.HasText(foldedLine(3)) {
		t.Fatalf("a three-step turn folded; texts=%q", strings.Join(tt.Texts(), " | "))
	}
	for _, row := range a.buildChatRows(a.groupedTurns()) {
		if row.kind == chatFold {
			t.Fatal("a three-step turn built a fold row")
		}
	}
}

// TestFoldCountIsEveryStep is the number on the line: it is the steps the turn
// holds, not Stats.Tools, which skips the thinking message and the verdict.
func TestFoldCountIsEveryStep(t *testing.T) {
	entries := longTurnEntries()
	built := turns.GroupTurns(entries, nil)
	turn := built[0]
	if len(turn.Steps) <= turn.Stats.Tools {
		t.Fatalf("the turn has %d steps and %d tools: this test's case is gone",
			len(turn.Steps), turn.Stats.Tools)
	}

	a := transcriptApp(entries...)
	i18n.Set(i18n.ZH)
	rows := a.buildChatRows(a.groupedTurns())
	folded := 0
	for _, row := range rows {
		if row.kind == chatFold {
			folded = len(row.folded) + 1 // the failing step stays a row of its own
		}
	}
	if folded != len(turn.Steps) {
		t.Fatalf("the line stands for %d steps, the turn has %d", folded, len(turn.Steps))
	}
	if got := foldedLine(len(turn.Steps)); got != strconv.Itoa(len(turn.Steps))+" 步" {
		t.Fatalf("the line reads %q", got)
	}
}

// TestRunningStepStaysOutOfTheFold: a live turn's last step is where the work
// is happening, so it keeps its own row while the finished ones fold.
func TestRunningStepStaysOutOfTheFold(t *testing.T) {
	entries := longTurnEntries()
	// The last call never came back: the turn is still on it.
	entries = entries[:len(entries)-4] // the verdict, and the answer to the last call
	a := transcriptApp(entries...)
	i18n.Set(i18n.ZH)
	// A run in flight is what makes the last turn live; the row code only asks
	// which step the turn is on.
	a.live = &protocol.LiveDoc{Run: json.RawMessage(`{"status":"running"}`)}
	built := a.groupedTurns()
	if len(built) != 1 {
		t.Fatalf("the entries built %d turns, want 1", len(built))
	}
	last := built[0].Steps[len(built[0].Steps)-1]
	if last.Result != nil {
		t.Fatal("the case needs a step with no answer yet")
	}
	if built[0].Status(a.live) != turns.StatusRunning {
		t.Fatalf("the turn is %v, not running", built[0].Status(a.live))
	}

	rows := a.buildChatRows(built)
	kept := false
	for _, row := range rows {
		if row.kind == chatSteps && len(row.steps) == 1 && row.steps[0].Entry.ID == last.Entry.ID {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("the step still running was folded away; rows=%v", rowKinds(rows))
	}
}

func rowKinds(rows []chatRow) []string {
	kinds := make([]string, 0, len(rows))
	for _, row := range rows {
		kinds = append(kinds, strconv.Itoa(int(row.kind)))
	}
	return kinds
}
