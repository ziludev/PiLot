package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
)

// TestQuitAsksWhileATurnRuns checks ⌘Q with work in flight opens the question
// instead of ending the process, and that confirming quits.
func TestQuitAsksWhileATurnRuns(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	quit := 0
	SetQuit(func() { quit++ })
	t.Cleanup(func() { SetQuit(nil) })

	a.tasks = []protocol.Task{{ID: 1, State: "running"}}
	tt := ui.NewTester(a.View, 1100, 760)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyQ)
	tt.Frame()
	if !a.quitAsk {
		t.Fatal("⌘Q left without asking while a turn was running")
	}
	if quit != 0 {
		t.Fatalf("the process quit under the confirmation (%d)", quit)
	}
	if !tt.HasText(quitTitle) {
		t.Fatalf("the question is not on screen: %q", tt.Texts())
	}
	tt.Click(quitLabel)
	tt.Frame()
	if quit != 1 {
		t.Fatalf("confirming quit %d times, want 1", quit)
	}
	if a.quitAsk {
		t.Fatal("the question stayed open after quitting")
	}
}

// TestQuitLeavesWhenIdle checks the common case: nothing running, no question.
func TestQuitLeavesWhenIdle(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	a.tasks = nil
	a.live = nil
	quit := 0
	SetQuit(func() { quit++ })
	t.Cleanup(func() { SetQuit(nil) })
	a.askQuit()
	if quit != 1 || a.quitAsk {
		t.Fatalf("idle quit: fired %d, asked %v", quit, a.quitAsk)
	}
}
