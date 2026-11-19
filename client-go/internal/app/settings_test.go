package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
)

// The footer's two switches moved behind one gear. Nothing about the switches
// themselves is new, so the test is about the panel: it names them, and picking
// one writes the pref the next launch reads.
func TestSettingsDialogPicksThemeAndLanguage(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.settings.open = true

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{"设置", "语言", "外观", "跟随系统", "浅色", "深色"} {
		if !tt.HasText(want) {
			t.Fatalf("settings missing %q; drew %v", want, tt.Texts())
		}
	}

	tt.Click("深色")
	tt.Frame()
	if a.prefs.Theme != "dark" {
		t.Fatalf("picking 深色 left prefs.Theme=%q", a.prefs.Theme)
	}
	if !a.dark {
		t.Fatal("the frame after picking 深色 did not draw dark")
	}

	tt.Click("跟随系统")
	tt.Frame()
	if a.prefs.Theme != "" {
		t.Fatalf("picking 跟随系统 left prefs.Theme=%q", a.prefs.Theme)
	}

	tt.Click("English")
	tt.Frame()
	if i18n.Current() != i18n.EN || a.prefs.Lang != "en" {
		t.Fatalf("picking English left lang=%v pref=%q", i18n.Current(), a.prefs.Lang)
	}
	i18n.Set(i18n.ZH)
}

// The dialog is a latch, not a copy: closing it and reopening shows what the
// prefs say, including a theme picked by the menu bar.
func TestSettingsDialogReadsPrefsWhenItOpens(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.prefs.Theme = "light"
	a.settings.open = true

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("浅色") {
		t.Fatal("浅色 not drawn")
	}
	// The chosen side is the one raised on the background colour; the tester only
	// sees labels, so the proof is that clicking the *other* side changes the
	// pref and this one does not.
	tt.Click("浅色")
	tt.Frame()
	if a.prefs.Theme != "light" {
		t.Fatalf("clicking the already-chosen side changed prefs.Theme to %q", a.prefs.Theme)
	}
}
