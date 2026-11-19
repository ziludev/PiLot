package app

import (
	"path/filepath"
	"testing"
)

func TestPrefsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	saved := prefsPath
	prefsPath = func() string { return filepath.Join(dir, "window.json") }
	defer func() { prefsPath = saved }()

	if got := loadPrefs(); got != (prefs{}) {
		t.Fatalf("missing file read as %+v, want empty", got)
	}
	savePrefs(prefs{Lang: "en", Theme: "dark", Rail: 300})
	if got := loadPrefs(); got.Lang != "en" || got.Theme != "dark" || got.Rail != 300 {
		t.Fatalf("read back %+v", got)
	}
}

// A width saved by an older window, or a hand-edited file, must not leave the
// rail at zero or off the window.
func TestPrefsRailWidth(t *testing.T) {
	if got := (prefs{}).railWidth(); got != railWidth {
		t.Fatalf("no saved width read as %v, want the default %v", got, railWidth)
	}
	if got := (prefs{Rail: 12}).railWidth(); got != railWidth {
		t.Fatalf("nonsense width read as %v", got)
	}
	if got := (prefs{Rail: 300}).railWidth(); got != 300 {
		t.Fatalf("saved width read as %v, want 300", got)
	}
}

func TestPrefsDark(t *testing.T) {
	if !(prefs{Theme: "dark"}).dark(false) {
		t.Fatal("a saved dark theme should beat a light system")
	}
	if (prefs{Theme: "light"}).dark(true) {
		t.Fatal("a saved light theme should beat a dark system")
	}
	if !(prefs{}).dark(true) {
		t.Fatal("no choice should follow the system")
	}
}
