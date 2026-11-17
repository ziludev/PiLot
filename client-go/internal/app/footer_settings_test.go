package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
)

// TestRailFooterOpensSettings checks the panel a reader has no shortcut for is
// reachable from the rail: a button in the footer, named, that opens it.
func TestRailFooterOpensSettings(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	button, ok := tt.Find(settingsTitle)
	if !ok || button.W == 0 {
		t.Fatalf("the settings button is not drawn in the footer: %q", tt.Texts())
	}
	if a.settingsOpen {
		t.Fatalf("the panel is open before the button is pressed")
	}
	if err := tt.Click(settingsTitle); err != nil {
		t.Fatalf("pressing the settings button: %v", err)
	}
	tt.Frame()
	if !a.settingsOpen {
		t.Fatalf("pressing the settings button left the panel shut")
	}
	if _, ok := tt.Find(settingsOpacity); !ok {
		t.Fatalf("the panel is open but its sections are not drawn")
	}
}

// TestRailFooterKeepsTheSettingsButtonInTheFooter checks the button sits in the
// rail's last line rather than floating over the chat, which is where a control
// acting on the whole window belongs.
func TestRailFooterKeepsTheSettingsButtonInTheFooter(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	button, ok := tt.Find(settingsTitle)
	if !ok || button.W == 0 {
		t.Fatalf("the settings button is not drawn")
	}
	logs, ok := tt.Find(i18n.T("logs"))
	if !ok || logs.W == 0 {
		t.Fatalf("the log button is not drawn")
	}
	if button.Y != logs.Y {
		t.Fatalf("the settings button is at y=%.1f, the log button at %.1f", button.Y, logs.Y)
	}
	if button.X > logs.X {
		t.Fatalf("the settings button is at x=%.1f, right of the log's %.1f", button.X, logs.X)
	}
	// The footer is the rail's last line: nothing may be drawn below it.
	if _, ok := tt.Find(settingsTitle); !ok {
		t.Fatalf("the settings button went missing")
	}
	if button.Y+button.H > float32(820)-1 {
		t.Fatalf("the settings button runs past the window's bottom edge")
	}
}
