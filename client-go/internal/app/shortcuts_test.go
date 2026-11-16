package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestCmdSlashListsTheKeys checks the cheat sheet opens from the keyboard and
// names the shortcuts this build actually answers to.
func TestCmdSlashListsTheKeys(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if a.shortcutsOpen {
		t.Fatal("the sheet starts closed")
	}
	tt.Key(ui.Cmd, ui.KeySlash)
	tt.Frame()
	tt.Frame()
	if !a.shortcutsOpen {
		t.Fatal("⌘/ did not open the sheet")
	}
	for _, want := range []string{"⌘K", "⌘⇧N", "⌘⇧Space", "Keyboard shortcuts"} {
		if !tt.HasText(want) {
			t.Fatalf("the sheet misses %q; texts: %v", want, tt.Texts())
		}
	}
}
