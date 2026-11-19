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
	savePrefs(prefs{Lang: "en", Theme: "dark"})
	if got := loadPrefs(); got.Lang != "en" || got.Theme != "dark" {
		t.Fatalf("read back %+v", got)
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
