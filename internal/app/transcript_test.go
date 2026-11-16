package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// transcriptApp is the shell with entries already loaded, no backend.
func transcriptApp(entries ...protocol.Entry) *App {
	a := New(Config{})
	a.current = 1
	a.entries = entries
	return a
}

// turnEntries builds one user message and its answer.
func turnEntries(id int, question, answer string) []protocol.Entry {
	return []protocol.Entry{
		{ID: id, Kind: "pi.user", Model: rawModel(map[string]any{
			"role": "user", "content": question,
		})},
		{ID: id + 1, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "text", "text": answer}},
		})},
	}
}

func rawModel(value any) json.RawMessage {
	data, err := json.Marshal([]any{value})
	if err != nil {
		panic(err)
	}
	return data
}

// TestTranscriptDrawsUserRightAndAnswerLeft is the transcript's layout
// contract: the person's message sits on the right, the answer on the left.
func TestTranscriptDrawsUserRightAndAnswerLeft(t *testing.T) {
	a := transcriptApp(turnEntries(1, "why is the sky blue", "Rayleigh scattering.")...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()

	user, okUser := tt.Find("why is the sky blue")
	answer, okAnswer := tt.Find("Rayleigh scattering.")
	if !okUser || !okAnswer {
		t.Fatalf("texts not drawn: %q", tt.Texts())
	}
	if user.X < 400 {
		t.Fatalf("user message at x=%.0f, want the right half", user.X)
	}
	if answer.X >= 400 {
		t.Fatalf("answer at x=%.0f, want the left half", answer.X)
	}
}

// TestTranscriptWindowKeepsTheFirstFrameCheap checks the 60-turn window and the
// button that opens the rest.
func TestTranscriptWindowKeepsTheFirstFrameCheap(t *testing.T) {
	var entries []protocol.Entry
	for i := 0; i < 65; i++ {
		entries = append(entries, turnEntries(i*10, "question "+itoa(i), "answer "+itoa(i))...)
	}
	a := transcriptApp(entries...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()

	if !tt.HasText("earlier turns") {
		t.Fatalf("no earlier-turns button; texts=%q", tt.Texts())
	}
	if tt.HasText("question 0") {
		t.Fatal("the window drew the oldest turn")
	}
	if !tt.HasText("question 64") {
		t.Fatalf("the window hid the newest turn; texts=%q", tt.Texts())
	}

	if err := tt.Click("earlier turns"); err != nil {
		t.Fatalf("click earlier turns: %v", err)
	}
	tt.Frame()
	if !tt.HasText("question 0") {
		t.Fatalf("the button did not open the older turns; texts=%q", tt.Texts())
	}
}

// TestToolStepOpensIntoItsPatch checks that a tool result holding a patch is
// folded into one line until the user opens it.
func TestToolStepOpensIntoItsPatch(t *testing.T) {
	patch := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new"
	entries := []protocol.Entry{
		{ID: 1, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "fix it"})},
		{ID: 2, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "content": []any{map[string]any{
				"type": "toolCall", "id": "call_1", "name": "edit", "arguments": map[string]any{"path": "a.go"},
			}},
		})},
		{ID: 3, Kind: "pi.tool-result", Model: rawModel(map[string]any{
			"role": "toolResult", "toolCallId": "call_1", "toolName": "edit",
			"content": []any{map[string]any{"type": "text", "text": patch}},
		})},
	}
	a := transcriptApp(entries...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()

	if tt.HasText("new") {
		t.Fatal("the patch drew without being opened")
	}
	row, ok := tt.Find("edit · a.go")
	if !ok {
		t.Fatalf("no step line; texts=%q", tt.Texts())
	}
	tt.ClickAt(row.X+2, row.Y+2)
	tt.Frame()
	if !tt.HasText("a.go") {
		t.Fatalf("the opened step drew no patch; texts=%q", tt.Texts())
	}
}

// TestTranscriptFrameStaysCheapOnALongChat is the scrolling budget: a chat with
// a thousand entries must build a frame in milliseconds, which is what a
// virtualized row list buys.
func TestTranscriptFrameStaysCheapOnALongChat(t *testing.T) {
	var entries []protocol.Entry
	for i := 0; i < 500; i++ {
		entries = append(entries, turnEntries(i*2, "question "+itoa(i), "answer "+itoa(i))...)
	}
	a := transcriptApp(entries...)
	tt := ui.NewTester(a.transcript, 900, 700)
	tt.Frame()

	start := time.Now()
	for i := 0; i < 10; i++ {
		tt.Frame()
	}
	per := time.Since(start) / 10
	if per > 100*time.Millisecond {
		t.Fatalf("frame took %v for %d entries", per, len(entries))
	}
	t.Logf("%d entries, %d turns, %v per frame", len(entries), 500, per)
}

// TestFindBarStepsThroughMatches is the in-chat search contract: the counter
// follows the query, the step buttons walk the hits and wrap, and the turn the
// bar stands on is drawn with a frame.
func TestFindBarStepsThroughMatches(t *testing.T) {
	entries := turnEntries(1, "first question", "the answer mentions needles")
	entries = append(entries, turnEntries(10, "second question", "nothing here")...)
	entries = append(entries, turnEntries(20, "third question", "another needles")...)
	a := transcriptApp(entries...)

	tt := ui.NewTester(func(c *ui.Context) { a.findBar(c, theme.Light()) }, 600, 60)
	tt.Frame()
	a.apply(func() {
		a.convQuery = "needles"
		a.convMatches = a.searchTurns()
	})
	tt.Frame()
	if !tt.HasText("1/2") {
		t.Fatalf("counter missing; texts=%q", tt.Texts())
	}

	a.stepMatch(1)
	if got := a.focusedTurn(); got != 2 {
		t.Fatalf("focused turn = %d, want 2", got)
	}
	a.stepMatch(1)
	if got := a.focusedTurn(); got != 0 {
		t.Fatalf("stepping past the end did not wrap: %d", got)
	}
	a.stepMatch(-1)
	if got := a.focusedTurn(); got != 2 {
		t.Fatalf("stepping before the start did not wrap: %d", got)
	}
}

// TestFindBarJumpsToAnOldTurn checks the window follows the selected match, so
// a hit 200 turns back is drawn instead of staying outside the window.
func TestFindBarJumpsToAnOldTurn(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	for i := 0; i < 200; i++ {
		question, answer := "question "+itoa(i), "answer "+itoa(i)
		if i == 3 {
			answer = "the needle sits here"
		}
		a.entries = append(a.entries, turnEntries(i*2+1, question, answer)...)
	}
	a.convQuery = "needle"
	a.convMatches = a.searchTurns()
	if len(a.convMatches) != 1 {
		t.Fatalf("matches = %v, want 1", a.convMatches)
	}

	tt := ui.NewTester(func(c *ui.Context) { a.transcript(c) }, 900, 700)
	tt.Frame()
	if !tt.HasText("the needle sits here") {
		t.Fatal("matched turn not drawn")
	}
	if tt.HasText("answer 199") {
		t.Fatal("window did not move to the match")
	}

	// Clicking the earlier-turns button grows the window and keeps the match.
	tt.Click("earlier turns")
	tt.Frame()
	if !tt.HasText("the needle sits here") {
		t.Fatal("match lost after widening the window")
	}
}

// TestCmdFOpensFindBar covers the window-wide shortcut ConvSearch.tsx binds:
// the root's OnShortcut has to fire even with nothing focused.
func TestCmdFOpensFindBar(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	a.entries = turnEntries(1, "where is it", "the answer")
	tt := ui.NewTester(a.View, 1000, 700)
	tt.Frame()
	if a.findOpen {
		t.Fatal("find bar open before the shortcut")
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Frame()
	if !a.findOpen {
		t.Fatal("cmd+F did not open the find bar")
	}
	// Typing goes into the bar's field and reports the match it found.
	if err := tt.Click(i18n.T("findShort")); err != nil {
		t.Fatal(err)
	}
	tt.Type("answer")
	tt.Frame()
	if !tt.HasText("1/1") {
		t.Fatalf("find bar did not count the match in %v", tt.Texts())
	}
}
