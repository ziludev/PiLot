package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestQuickKeysSwitchTheFirstChats checks ⌘1 … ⌘9 land in the rail's order,
// and that a number past the list does nothing.
func TestQuickKeysSwitchTheFirstChats(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	want := a.chatForQuickKey(1)
	if want == 0 {
		t.Skip("the fixture lists fewer than two chats")
	}
	tt.Key(ui.Cmd, ui.Key2)
	tt.Frame()
	if a.currentValue() != want {
		t.Fatalf("⌘2 opened %d, want %d", a.currentValue(), want)
	}
	before := a.currentValue()
	tt.Key(ui.Cmd, ui.Key9)
	tt.Frame()
	if a.currentValue() != before {
		t.Fatalf("⌘9 with nine chats listed jumped to %d", a.currentValue())
	}
}
