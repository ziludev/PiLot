package app

import (
	"os"
	"path/filepath"
	"testing"

	"pitta/internal/bridge"
)

// TestMain points HOME at a scratch directory: the app persists preferences
// under ~/.pitta/ui.json, and a test must not rewrite the developer's own.
func TestMain(m *testing.M) {
	realHome = os.Getenv("HOME")
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

// realHome is HOME before TestMain swapped it, so live tests can still find the
// agent checkout's .env.
var realHome string

// liveConfig is the running agent's bridge settings. HOME points at a scratch
// dir inside tests, so the checkout has to be named through PI_DURABLE_HOME.
func liveConfig() Config {
	if realHome != "" {
		os.Setenv("PI_DURABLE_HOME", filepath.Join(realHome, "learn/pi-durable-agent"))
	}
	return Config{BaseURL: bridge.BaseURL(), Token: bridge.Token(), Home: bridge.Home()}
}
