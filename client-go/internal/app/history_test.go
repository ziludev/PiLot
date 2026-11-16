package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestComposerRecallsSentMessages checks Up walks back through what was sent,
// Down comes back to the draft, and the draft is not lost on the way.
func TestComposerRecallsSentMessages(t *testing.T) {
	a := snapshotData(t)
	// Without the approval bar the composer starts at the top of the tester,
	// so the click below lands in the text area.
	a.approvals = nil
	tt := ui.NewTester(func(c *ui.Context) { a.composer(c) }, 480, 200)
	tt.Frame()
	// Three messages, sent the way the composer sends them.
	for _, text := range []string{"first", "second", "third"} {
		a.apply(func() { a.recall(text) })
	}
	a.apply(func() { a.draft = "half typed" })
	tt.Frame()
	tt.ClickAt(40, 40)
	tt.Frame()

	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if a.draft != "third" {
		t.Fatalf("first Up gave %q, want third", a.draft)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if a.draft != "second" {
		t.Fatalf("second Up gave %q, want second", a.draft)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.draft != "third" {
		t.Fatalf("Down gave %q, want third", a.draft)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.draft != "half typed" {
		t.Fatalf("Down at the end gave %q, want the draft back", a.draft)
	}
}

// TestRecallLeavesArrowsAloneInAMultilineDraft checks the arrows keep moving the
// caret once the draft has line breaks: recall must not eat the editor's keys.
func TestRecallLeavesArrowsAloneInAMultilineDraft(t *testing.T) {
	a := snapshotData(t)
	a.approvals = nil
	a.apply(func() { a.recall("earlier message") })
	a.apply(func() { a.draft = "line one\nline two" })
	tt := ui.NewTester(func(c *ui.Context) { a.composer(c) }, 480, 200)
	tt.Frame()
	tt.ClickAt(40, 40)
	tt.Frame()
	if tt.Key(0, ui.KeyUp); a.draft != "line one\nline two" {
		t.Fatalf("Up replaced the multiline draft with %q", a.draft)
	}
}
