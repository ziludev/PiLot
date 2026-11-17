package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestSettingsHeadsEverySection checks each group of preferences carries the
// same small heading. The language chips used to sit straight under the theme
// chips with nothing between them, so "中文 English" read as a second set of
// appearance options.
func TestSettingsHeadsEverySection(t *testing.T) {
	a := snapshotData(t)
	a.apply(func() { a.settingsOpen = true; a.invalidate() })
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	type section struct{ heading, first string }
	sections := []section{
		{settingsTheme, themeSystemLabel},
		{settingsLanguage, langZH},
		{settingsWindow, settingsOpacity},
		{settingsKeys, shortcuts()[0].Action},
	}
	var previous float32
	for _, s := range sections {
		heading, ok := tt.Find(s.heading)
		if !ok || heading.W == 0 {
			t.Fatalf("the %q heading is not drawn", s.heading)
		}
		first, ok := tt.Find(s.first)
		if !ok || first.W == 0 {
			t.Fatalf("%q's first row (%q) is not drawn", s.heading, s.first)
		}
		if heading.Y >= first.Y {
			t.Fatalf("the %q heading is at y=%.1f, at or below its row at %.1f", s.heading, heading.Y, first.Y)
		}
		if heading.Y < previous {
			t.Fatalf("the %q heading is above the section before it", s.heading)
		}
		previous = heading.Y
	}
}

// TestSettingsLanguageHeadingFollowsTheChips checks the heading sits directly
// above the two language chips, where a reader takes it for their label.
func TestSettingsLanguageHeadingFollowsTheChips(t *testing.T) {
	a := snapshotData(t)
	a.apply(func() { a.settingsOpen = true; a.invalidate() })
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	heading, ok := tt.Find(settingsLanguage)
	if !ok || heading.W == 0 {
		t.Fatalf("the language heading is not drawn")
	}
	chip, ok := tt.Find(langZH)
	if !ok || chip.W == 0 {
		t.Fatalf("the language chip is not drawn")
	}
	if gap := chip.Y - heading.Y; gap < heading.H || gap > 30 {
		t.Fatalf("the heading is %.1f px above the chips, want it right on top of them", gap)
	}
	if chip.X != heading.X {
		t.Fatalf("the heading starts at x=%.1f, the chips at %.1f", heading.X, chip.X)
	}
}
