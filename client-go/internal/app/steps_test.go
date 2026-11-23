package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
)

// openFolds unfolds every step row of a chat, the way a click on n 步 does.
func openFolds(a *App, conv int) {
	for _, row := range a.threadRows(conv) {
		if row.kind == rowSteps {
			a.vm.folds[row.id] = true
		}
	}
}

// TestStepsListToolsWhenThereAreMany: a run of six calls opens on what it used,
// not on the six calls. Only the last tool shows its calls, because that is the
// one that just answered.
func TestStepsListToolsWhenThereAreMany(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := time.Now().Add(-time.Minute)
	a.thread(11).upsert(entryJSON(t, 600, "pi.user", now,
		`[{"role":"user","content":"check the build","timestamp":__ts}]`))

	var calls []string
	for i := 0; i < 4; i++ {
		calls = append(calls, `{"type":"toolCall","name":"bash","arguments":{"command":"echo `+strconv.Itoa(i)+`"}}`)
	}
	for i := 0; i < 2; i++ {
		calls = append(calls, `{"type":"toolCall","name":"read","arguments":{"path":"/tmp/f`+strconv.Itoa(i)+`.go"}}`)
	}
	a.thread(11).upsert(entryJSON(t, 601, "pi.assistant", now,
		`[{"role":"assistant","content":[`+strings.Join(calls, ",")+`],"timestamp":__ts}]`))
	openFolds(a, 11)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{"bash ×4", "read ×2"} {
		if !tt.HasText(want) {
			t.Fatalf("missing %q; drew %v", want, tt.Texts())
		}
	}
	// read came last, so its two calls are the ones on show.
	if !tt.HasText("/tmp/f1.go") {
		t.Fatalf("the last tool's calls were folded away too; drew %v", tt.Texts())
	}
	if tt.HasText("echo 0") {
		t.Fatal("bash's four calls were drawn without asking")
	}

	if err := tt.Click("bash ×4"); err != nil {
		t.Fatalf("group head: %v", err)
	}
	tt.Frame()
	if !tt.HasText("echo 0") || !tt.HasText("echo 3") {
		t.Fatalf("opening bash did not show its calls; drew %v", tt.Texts())
	}
}

// TestFewStepsStayFlat: four calls read at a glance, and a table of contents
// over four lines is more chrome than content.
func TestFewStepsStayFlat(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := time.Now().Add(-time.Minute)
	a.thread(11).upsert(entryJSON(t, 610, "pi.user", now,
		`[{"role":"user","content":"two things","timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 611, "pi.assistant", now,
		`[{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"echo one"}},{"type":"toolCall","name":"read","arguments":{"path":"/tmp/a.go"}}],"timestamp":__ts}]`))
	openFolds(a, 11)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if tt.HasText("bash ×1") || tt.HasText("read ×1") {
		t.Fatalf("two calls were given a table of contents; drew %v", tt.Texts())
	}
	if !tt.HasText("echo one") || !tt.HasText("/tmp/a.go") {
		t.Fatalf("the calls were not drawn; drew %v", tt.Texts())
	}
}

// TestToolOutputCopiesWithoutTheAnswer: the stack the reader wants is one
// paragraph of a message, and copying the message takes the answer with it.
func TestToolOutputCopiesWithoutTheAnswer(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := time.Now().Add(-time.Minute)
	out := "FAIL src/queue.test.ts\n  Expected: 5\n  Received: 3"
	quoted, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	a.thread(11).upsert(entryJSON(t, 620, "pi.user", now,
		`[{"role":"user","content":"run the tests","timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 621, "pi.assistant", now,
		`[{"role":"assistant","content":[{"type":"toolCall","name":"verify","arguments":{"command":"npx vitest run"}}],"timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 622, "pi.tool-result", now,
		`[{"role":"toolResult","content":[{"type":"text","text":`+string(quoted)+`}],"timestamp":__ts}]`))
	openFolds(a, 11)
	a.vm.groups = map[string]bool{}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	// Faint until the pointer is on the block, but always there to find.
	rect, ok := tt.Find(i18n.T("copy") + " verify")
	if !ok {
		t.Fatalf("the output had no copy button; drew %v", tt.Texts())
	}
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if err := tt.Click(i18n.T("copy") + " verify"); err != nil {
		t.Fatalf("copy button: %v", err)
	}
	tt.Frame()
	if got := tt.Clipboard(); got != "FAIL src/queue.test.ts\n  Expected: 5\n  Received: 3" {
		t.Fatalf("clipboard holds %q", got)
	}
}

// TestFailedToolShowsWithTheFoldShut: a failed call is a state the line can
// state, not something the reader has to open the fold to discover.
func TestFailedToolShowsWithTheFoldShut(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := time.Now().Add(-time.Minute)
	a.thread(11).upsert(entryJSON(t, 630, "pi.user", now,
		`[{"role":"user","content":"build it","timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 631, "pi.assistant", now,
		`[{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"go build ./..."}}],"timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 632, "pi.tool-result", now,
		`[{"role":"toolResult","content":[{"type":"text","text":"build failed"}],"isError":true,"timestamp":__ts}]`))

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !hasAnyText(tt, "1 步 · 1 个失败") {
		t.Fatalf("the failed call did not say so with the fold shut; drew %v", tt.Texts())
	}
}

// TestStepsGroupByConsecutiveTool: five calls alternating bash and read are five
// stretches, not two tools with counts. The order is the story of the turn.
func TestStepsGroupByConsecutiveTool(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := time.Now().Add(-time.Minute)
	a.thread(11).upsert(entryJSON(t, 640, "pi.user", now,
		`[{"role":"user","content":"read then build","timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 641, "pi.assistant", now,
		`[{"role":"assistant","content":[`+
			`{"type":"toolCall","name":"bash","arguments":{"command":"echo 0"}},`+
			`{"type":"toolCall","name":"read","arguments":{"path":"/tmp/a.go"}},`+
			`{"type":"toolCall","name":"bash","arguments":{"command":"echo 1"}},`+
			`{"type":"toolCall","name":"read","arguments":{"path":"/tmp/b.go"}},`+
			`{"type":"toolCall","name":"bash","arguments":{"command":"echo 2"}}`+
			`],"timestamp":__ts}]`))
	openFolds(a, 11)
	a.vm.groups = map[string]bool{}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	// The head records its own Label and the text inside it, so a group shows up
	// twice; what matters is that no stretch was merged into a ×2.
	seen := map[string]bool{}
	for _, text := range tt.Texts() {
		seen[text] = true
		if strings.HasPrefix(text, "bash ×") && text != "bash ×1" {
			t.Fatalf("bash stretches merged into %q; drew %v", text, tt.Texts())
		}
	}
	if !seen["bash ×1"] || !seen["read ×1"] {
		t.Fatalf("the alternating stretches did not all draw; drew %v", tt.Texts())
	}
}

// TestStepArgsOpenAboveTheResult: the call's arguments are what the fold opens
// onto, and they sit above the answer the way the request does.
func TestStepArgsOpenAboveTheResult(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := time.Now().Add(-time.Minute)
	a.thread(11).upsert(entryJSON(t, 650, "pi.user", now,
		`[{"role":"user","content":"build it","timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 651, "pi.assistant", now,
		`[{"role":"assistant","content":[{"type":"toolCall","name":"bash","arguments":{"command":"go build ./...","timeout":30}}],"timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 652, "pi.tool-result", now,
		`[{"role":"toolResult","content":[{"type":"text","text":"build ok"}],"timestamp":__ts}]`))
	openFolds(a, 11)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText(i18n.T("toolArgs")) {
		t.Fatalf("the arguments block was not drawn; drew %v", tt.Texts())
	}
	if !hasAnyText(tt, `"timeout": 30`) {
		t.Fatalf("the arguments block lost a key the summary cannot show; drew %v", tt.Texts())
	}
}

// TestSingleKeyArgumentsStayOnTheHeader: one key is already the header's line,
// so repeating it as a block is only height.
func TestSingleKeyArgumentsStayOnTheHeader(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	now := time.Now().Add(-time.Minute)
	a.thread(11).upsert(entryJSON(t, 660, "pi.user", now,
		`[{"role":"user","content":"build it","timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 661, "pi.assistant", now,
		`[{"role":"assistant","content":[{"type":"toolCall","name":"read","arguments":{"path":"/tmp/a.go"}}],"timestamp":__ts}]`))
	a.thread(11).upsert(entryJSON(t, 662, "pi.tool-result", now,
		`[{"role":"toolResult","content":[{"type":"text","text":"package main"}],"timestamp":__ts}]`))
	openFolds(a, 11)

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if tt.HasText(i18n.T("toolArgs")) {
		t.Fatalf("a single-key call was given an arguments block; drew %v", tt.Texts())
	}
}
