package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps every test off the owner's real window.json. Opening a chat
// section or marking a chat seen saves prefs, so a test that built an App
// without pointing prefsPath elsewhere wrote a zero-valued file over the
// running window's settings — the language picked last time among them.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pitta-test-prefs")
	if err != nil {
		panic(err)
	}
	prefsPath = func() string { return filepath.Join(dir, "window.json") }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
