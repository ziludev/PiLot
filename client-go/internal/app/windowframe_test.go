package app

import "testing"

// TestPrefsCarryTheWindowFrame checks the frame survives the preference file,
// so a restart lands where the window was.
func TestPrefsCarryTheWindowFrame(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	prefs := prefs{WindowX: 120, WindowY: 80, WindowW: 1180, WindowH: 820, LastChat: 42}
	savePrefsFile(prefs)
	back := loadPrefs()
	if back.WindowX != 120 || back.WindowY != 80 || back.WindowW != 1180 || back.WindowH != 820 {
		t.Fatalf("frame came back as %+v", back)
	}
	if back.LastChat != 42 {
		t.Fatalf("last chat came back as %d", back.LastChat)
	}
}
