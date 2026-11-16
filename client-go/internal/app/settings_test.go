package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
)

// TestCmdCommaOpensSettings checks ⌘, opens the panel, that it lists the keys,
// and that picking a theme reaches the preferences.
func TestCmdCommaOpensSettings(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	a.prefs.Theme = ""
	tt := ui.NewTester(a.View, 1100, 800)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyComma)
	tt.Frame()
	if !a.settingsOpen {
		t.Fatal("⌘, did not open the settings panel")
	}
	if !tt.HasText(settingsTitle) || !tt.HasText(settingsKeys) {
		t.Fatalf("the panel is empty: %q", tt.Texts())
	}
	if !tt.HasText(shortcuts()[0].Keys) {
		t.Fatalf("the key list is missing: %q", tt.Texts())
	}
	tt.Click(themeDarkLabel)
	tt.Frame()
	if a.prefs.Theme != "dark" {
		t.Fatalf("picking Dark set the theme to %q", a.prefs.Theme)
	}
	tt.Click(themeSystemLabel)
	tt.Frame()
	if a.prefs.Theme != "system" {
		t.Fatalf("picking the system theme set %q", a.prefs.Theme)
	}
	tt.Click(i18n.T("cancel"))
	tt.Frame()
	if a.settingsOpen {
		t.Fatal("Cancel left the panel open")
	}
}
