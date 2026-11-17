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
	// The window's transparency and the daemon's controls are here now, not in
	// the rail's footer.
	if !tt.HasText(settingsWindow) || !tt.HasText(settingsOpacity) {
		t.Fatalf("the window section is missing: %q", tt.Texts())
	}
	if !tt.HasText(i18n.T("restart")) || !tt.HasText(i18n.T("backup")) {
		t.Fatalf("the agent controls are missing: %q", tt.Texts())
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

// TestSettingsChoosesTheLanguage checks the switches the rail's header used to
// carry: the panel names them in full, and picking one changes the dictionary
// the whole window renders with.
func TestSettingsChoosesTheLanguage(t *testing.T) {
	// The dictionary is process-wide, so put back what the run started with.
	was := i18n.Current()
	t.Cleanup(func() { i18n.Set(was) })

	a := snapshotData(t)
	a.prefs.Theme = "light"
	a.settingsOpen = true
	tt := ui.NewTester(a.View, 1100, 800)
	tt.Frame()
	// Nothing in the header toggles these now.
	if _, ok := tt.Find(i18n.T("theme")); ok {
		t.Fatalf("the theme toggle still sits in the rail header")
	}
	tt.Click(langZH)
	tt.Frame()
	if a.prefs.Lang != string(i18n.ZH) || i18n.Current() != i18n.ZH {
		t.Fatalf("picking 中文 left lang=%q dictionary=%q", a.prefs.Lang, i18n.Current())
	}
	tt.Click(langEN)
	tt.Frame()
	if a.prefs.Lang != string(i18n.EN) || i18n.Current() != i18n.EN {
		t.Fatalf("picking English left lang=%q dictionary=%q", a.prefs.Lang, i18n.Current())
	}
}
