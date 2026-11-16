package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestPaletteRunsCommands checks ">" turns the jump list into a command list
// and Enter runs the highlighted command, the way Raycast's one field works.
func TestPaletteRunsCommands(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	a.api = quietAPI(t)
	tt := ui.NewTester(a.View, 1180, 760)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if !a.switcher.open {
		t.Fatal("⌘K did not open the palette")
	}
	tt.Type(">sett")
	tt.Frame()
	if !paletteMode(a.switcher.query) {
		t.Fatalf("query %q is not read as a command", a.switcher.query)
	}
	if got := a.paletteMatches(a.switcher.query); len(got) == 0 {
		t.Fatal("no command matched sett")
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !a.settingsOpen {
		t.Fatal("Enter did not run the Settings command")
	}
	if a.switcher.open {
		t.Fatal("the palette stayed open behind the command")
	}
}

// TestPaletteStillJumpsToChats checks the default mode is unchanged: a plain
// query narrows chats and Enter opens one.
func TestPaletteStillJumpsToChats(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	a.api = quietAPI(t)
	tt := ui.NewTester(a.View, 1180, 760)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	matches := a.switcherMatches()
	if len(matches) == 0 {
		t.Fatal("the palette lists no chats")
	}
	target := matches[0]
	if target.ConversationID == a.currentValue() && len(matches) > 1 {
		target = matches[1]
	}
	tt.Type("#" + itoa(target.ConversationID))
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.currentValue() != target.ConversationID {
		t.Fatalf("Enter opened %d, want %d", a.currentValue(), target.ConversationID)
	}
}
