package app

import (
	"os"
	"testing"
)

// TestMain points HOME at a scratch directory: the app persists preferences
// under ~/.pitta/ui.json, and a test must not rewrite the developer's own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pitta-app-test-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("HOME", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
