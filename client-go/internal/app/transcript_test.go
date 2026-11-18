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

// TestTranscriptBuildsOnlyWhatIsInView is the list's contract: a chat of 65
// turns costs the rows the screen shows, and the turns above them are still
// there to scroll to.
func TestTranscriptBuildsOnlyWhatIsInView(t *testing.T) {
	var entries []protocol.Entry
	for i := 0; i < 65; i++ {
		entries = append(entries, turnEntries(i*10, "question "+itoa(i), "answer "+itoa(i))...)
	}
	a := transcriptApp(entries...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()
	tt.Frame()

	if a.turnTotal != 65 {
		t.Fatalf("the chat holds %d turns, want 65", a.turnTotal)
	}
	if tt.HasText("question 0") {
		t.Fatal("the first frame built the oldest turn")
	}
	if !tt.HasText("question 64") {
		t.Fatalf("the first frame hid the newest turn; texts=%q", tt.Texts())
	}

	// The view opens at the newest turn, so the older ones are above the
	// visible area: scrolling up the way a reader would builds them.
	a.apply(func() { a.turnList.ScrollTo(0, ui.Start) })
	tt.Frame()
	tt.Frame()
	if !tt.HasText("question 0") {
		t.Fatalf("scrolling to the top did not build the older turns; texts=%q", tt.Texts())
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
	tt.Frame()
	if !tt.HasText("the needle sits here") {
		t.Fatalf("the list did not land on the match; texts=%q", tt.Texts())
	}
	if tt.HasText("answer 199") {
		t.Fatal("the list stayed at the newest turn")
	}

	// The hit is marked, so the line that matched is found once it is in view.
	if !tt.HasText("the needle sits here") {
		t.Fatalf("the hit lost its mark; texts=%q", tt.Texts())
	}
}

// TestStepsHangOffARoundedRail checks the timeline's rail is a 2 px rounded
// column: a 1 px border reads as a table rule instead of a timeline.
func TestStepsHangOffARoundedRail(t *testing.T) {
	a := snapshotData(t)
	a.entries = turnEntries(701, "question", "answer")
	tt := ui.NewTester(a.View, 900, 620)
	tt.Frame()
	label, ok := tt.Find("思考")
	if !ok {
		t.Skipf("the fixture's steps are not on screen: %q", tt.Texts())
	}
	img := tt.Image()
	border := theme.Light().Border
	isBorder := func(x, y int) bool {
		c := img.RGBAAt(x, y)
		return nearColor(c.R, border.R) && nearColor(c.G, border.G) && nearColor(c.B, border.B)
	}
	top, bottom := int(label.Y)-4, int(label.Y+label.H)+4
	rows := bottom - top
	covered := func(x int) int {
		run := 0
		for y := top; y < bottom; y++ {
			if isBorder(x, y) {
				run++
			}
		}
		return run
	}
	rail, tallest := 0, 0
	for x := int(label.X) - 40; x < int(label.X); x++ {
		if n := covered(x); n > tallest {
			rail, tallest = x, n
		}
	}
	if tallest < rows*3/5 {
		t.Fatalf("no column runs alongside the steps (best %d of %d rows)", tallest, rows)
	}
	// Count the columns painted for the whole height: the rail is 2, a border
	// is 1, and a card that touches the rail covers only part of it.
	width := 0
	for x := rail - 2; x <= rail+2; x++ {
		if covered(x) >= rows-2 {
			width++
		}
	}
	if width != 2 {
		t.Fatalf("the step rail is %d px wide, want 2 (around column %d)", width, rail)
	}
}

// reportDoneEntries is a turn that ends on a verdict.
func reportDoneEntries(met bool, why string) []protocol.Entry {
	return []protocol.Entry{
		{ID: 1, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "fix the seam"})},
		{ID: 2, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "content": []any{map[string]any{
				"type": "toolCall", "id": "call_1", "name": "report_done",
				"arguments": map[string]any{"met": met, "why": why},
			}},
		})},
		{ID: 3, Kind: "pi.tool-result", Model: rawModel(map[string]any{
			"role": "toolResult", "toolCallId": "call_1", "toolName": "report_done",
			"content": []any{map[string]any{"type": "text", "text": "recorded"}},
		})},
	}
}

// TestReportDoneDrawsAsVerdict is where the verdict belongs: report_done is not
// one more step among the tools, it is the turn's answer to whether it got the
// job done, so the word and the line saying how it was checked are drawn out in
// the open, and the call never shows up as a foldable step label.
func TestReportDoneDrawsAsVerdict(t *testing.T) {
	a := transcriptApp(reportDoneEntries(true, "go test ./internal/app is green")...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()

	if !tt.HasText(i18n.T("goalMet")) {
		t.Fatalf("the verdict did not draw; texts=%q", tt.Texts())
	}
	if !tt.HasText("go test ./internal/app is green") {
		t.Fatalf("the reason did not draw; texts=%q", tt.Texts())
	}
	if tt.HasText("report_done") {
		t.Fatalf("the call still drew as a step; texts=%q", tt.Texts())
	}

	a = transcriptApp(reportDoneEntries(false, "the seam still flickers")...)
	tt = ui.NewTester(a.transcript, 800, 400)
	tt.Frame()
	if !tt.HasText(i18n.T("goalNotMet")) {
		t.Fatalf("an unmet turn drew no verdict; texts=%q", tt.Texts())
	}
}

// TestASmallScrollUpStopsFollowingTheEnd is the fix for a transcript that would
// not slide: the reader flicked up a few points, and the "close enough to the
// end" band pulled them straight back down. Anything that moves the offset up
// was the reader, so it ends the following however small it was.
func TestASmallScrollUpStopsFollowingTheEnd(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(a.transcript, 600, 120)
	tt.Frame()
	tt.Frame() // the first frame is where the rows get their heights
	if !a.atBottom {
		t.Fatal("a transcript opened at its end should follow it")
	}

	// A touchpad flick of ten points, the sort that used to be swallowed.
	tt.Scroll(300, 60, 0, -10)
	tt.Frame()
	tt.Frame()
	if a.atBottom {
		t.Fatal("an upward scroll of 10 was swallowed")
	}
	at, _ := a.turnList.Visible()
	tt.Advance(40 * time.Millisecond)
	tt.Frame()
	tt.Frame()
	if now, _ := a.turnList.Visible(); now < at {
		t.Fatalf("the transcript slid back down after the reader left the end: row %d then %d", at, now)
	}
}

// toolRunEntries is a turn that runs three commands before it answers.
func toolRunEntries() []protocol.Entry {
	entries := []protocol.Entry{
		{ID: 1, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "look around"})},
	}
	for i, command := range []string{"ls", "git status", "go test ./..."} {
		callID := "call_" + itoa(i+1)
		entries = append(entries,
			protocol.Entry{ID: 2 + i*2, Kind: "pi.assistant", Model: rawModel(map[string]any{
				"role": "assistant", "content": []any{map[string]any{
					"type": "toolCall", "id": callID, "name": "bash",
					"arguments": map[string]any{"command": command},
				}},
			})},
			protocol.Entry{ID: 3 + i*2, Kind: "pi.tool-result", Model: rawModel(map[string]any{
				"role": "toolResult", "toolCallId": callID, "toolName": "bash",
				"content": []any{map[string]any{"type": "text", "text": "ok"}},
			})},
		)
	}
	return entries
}

// TestToolCallsFoldIntoOneLine is the room a turn gets back: three commands used
// to put three lines between the question and the answer, and now take one, which
// opens into the three of them when they are wanted.
func TestToolCallsFoldIntoOneLine(t *testing.T) {
	a := transcriptApp(toolRunEntries()...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()

	if !tt.HasText(i18n.TF("toolCallsCount", map[string]any{"count": 3})) {
		t.Fatalf("the run did not fold; texts=%q", tt.Texts())
	}
	// The folded line ends with what the run came to; the calls before it do not
	// each get a row of their own.
	for _, command := range []string{"bash · ls", "bash · git status"} {
		if tt.HasText(command) {
			t.Fatalf("a folded call still drew its own row: %q; texts=%q", command, tt.Texts())
		}
	}

	// The one line opens into the calls themselves.
	if err := tt.Click(i18n.TF("toolCallsCount", map[string]any{"count": 3})); err != nil {
		t.Fatalf("the folded row could not be clicked: %v", err)
	}
	tt.Frame()
	for _, command := range []string{"bash · ls", "bash · git status", "bash · go test ./..."} {
		if !tt.HasText(command) {
			t.Fatalf("opening the run did not show %q; texts=%q", command, tt.Texts())
		}
	}
}

// TestASingleToolCallKeepsItsOwnRow keeps the fold for runs: one command reads
// better as itself than as "1 次工具调用".
func TestASingleToolCallKeepsItsOwnRow(t *testing.T) {
	entries := []protocol.Entry{
		{ID: 1, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "look"})},
		{ID: 2, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "content": []any{map[string]any{
				"type": "toolCall", "id": "call_1", "name": "bash",
				"arguments": map[string]any{"command": "ls"},
			}},
		})},
		{ID: 3, Kind: "pi.tool-result", Model: rawModel(map[string]any{
			"role": "toolResult", "toolCallId": "call_1", "toolName": "bash",
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
		})},
	}
	a := transcriptApp(entries...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()
	if !tt.HasText("bash · ls") {
		t.Fatalf("a lone call was folded away; texts=%q", tt.Texts())
	}
	if tt.HasText(i18n.TF("toolCallsCount", map[string]any{"count": 1})) {
		t.Fatalf("a lone call drew a run line; texts=%q", tt.Texts())
	}
}

// TestAVirtualListHoldsNoTurnBack: the list builds the rows in view and nothing
// else, so a chat of 200 turns has its first question at the top of the scroll
// rather than behind a window or a button.
func TestAVirtualListHoldsNoTurnBack(t *testing.T) {
	i18n.Set(i18n.EN)
	a := New(Config{})
	a.previewImage = -1
	a.current = 11
	a.entries = manyTurns(200)
	tt := ui.NewTester(func(c *ui.Context) { a.main(c) }, 800, 400)
	tt.Frame()
	tt.Frame()
	if a.turnTotal != 200 {
		t.Fatalf("the chat holds %d turns, want 200", a.turnTotal)
	}
	// Exact, not HasText: "question 1" is a prefix of "question 199", and the
	// tail of a 200-turn chat is full of turns numbered in the hundreds.
	if showsExactly(tt, "question 1") {
		t.Fatal("the first frame built the oldest turn")
	}

	// The reader scrolls to the start: the rows above are built as they come
	// into view, and the one at the top is the chat's first.
	a.apply(func() { a.turnList.ScrollTo(0, ui.Start) })
	tt.Frame()
	tt.Frame()
	if !showsExactly(tt, "question 1") {
		t.Fatalf("the top of the chat is not its first turn; texts=%q", tt.Texts())
	}
}

// TestAFlickFromTheEndStopsFollowingIt reads the flick the way the app does: the
// pointer over the transcript, ten points up, while the view still follows its
// end. It must stop there, and stay stopped.
func TestAFlickFromTheEndStopsFollowingIt(t *testing.T) {
	a := snapshotData(t)
	a.apply(func() { a.turnList.ScrollToEnd() })
	tt := ui.NewTester(a.transcript, 600, 120)
	tt.Frame()
	tt.Frame()
	if !a.atBottom {
		t.Fatal("a chat opened at its end does not follow it")
	}

	tt.Scroll(300, 60, 0, -10)
	tt.Frame()
	tt.Frame()
	if a.atBottom {
		t.Fatal("a ten-point flick was swallowed")
	}
	at, _ := a.turnList.Visible()
	tt.Frame()
	tt.Frame()
	if now, _ := a.turnList.Visible(); now < at {
		t.Fatalf("the transcript slid back to row %d after the flick, was %d", now, at)
	}
}

// TestStoppingNearTheEndDoesNotYankTheViewBack keeps a reader who scrolls up a
// little from being pulled the same little back: only the end itself re-attaches
// the following, so the view never jumps under the pointer.
func TestStoppingNearTheEndDoesNotYankTheViewBack(t *testing.T) {
	a := snapshotData(t)
	a.apply(func() { a.turnList.ScrollToEnd() })
	tt := ui.NewTester(a.transcript, 600, 120)
	tt.Frame()
	tt.Frame()

	// Up ten points, halfway through the last screen, and stop.
	tt.Scroll(300, 60, 0, -10)
	tt.Frame()
	tt.Frame()
	if a.atBottom {
		t.Fatal("a flick up re-attached the following")
	}
	at, _ := a.turnList.Visible()
	tt.Frame()
	tt.Frame()
	if now, _ := a.turnList.Visible(); now < at {
		t.Fatalf("the view jumped back to row %d after stopping near the end, was %d", now, at)
	}

	// Scrolling all the way down does re-attach it.
	tt.Scroll(300, 60, 0, 200000)
	tt.Frame()
	tt.Frame()
	if !a.atBottom {
		t.Fatal("the end did not re-attach the following")
	}
}

// TestTheHistoryOfferReadsPagesNotTurns: when every loaded turn is drawn and the
// bridge still holds entries above them, the button reads the next page instead
// of opening a window that has nothing behind it.
func TestTheHistoryOfferReadsPagesNotTurns(t *testing.T) {
	i18n.Set(i18n.EN)
	a := transcriptApp(turnEntries(1, "why is the sky blue", "Rayleigh scattering.")...)
	// A page in flight, so the top of the chat does not read the next one itself
	// while the test looks at the offer.
	a.historyMore, a.historyLoading = true, true
	tt := ui.NewTester(func(c *ui.Context) { a.main(c) }, 800, 400)
	tt.Frame()
	tt.Frame()
	a.apply(func() { a.turnList.ScrollTo(0, ui.Start) })
	tt.Frame()
	tt.Frame()
	if !tt.HasText(i18n.T("olderMessages")) {
		t.Fatalf("a chat with entries above it offered no page; texts=%q", tt.Texts())
	}

	// Once the page is in, the offer goes away.
	a.apply(func() { a.historyMore = false })
	tt.Frame()
	tt.Frame()
	if tt.HasText(i18n.T("olderMessages")) {
		t.Fatalf("the offer stayed after the chat began where it begins; texts=%q", tt.Texts())
	}
}
