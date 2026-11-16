package app

import (
	"os"
	"testing"
	"time"

	"pitta/internal/api"
)

// TestSecondaryWindowKeepsTheOwnedState checks the split between windows: the
// second one opens on the chat it was given, never writes the preference file,
// and never sets the Dock badge.
func TestSecondaryWindowKeepsTheOwnedState(t *testing.T) {
	home := t.TempDir()
	client := api.New("http://127.0.0.1:1", "")
	a := NewSecondary(Config{Home: home}, client, 42)
	if !a.secondary {
		t.Fatal("a second window should be marked secondary")
	}
	if a.api != client {
		t.Fatal("the second window should share the client")
	}
	if got := a.currentValue(); got != 42 {
		t.Fatalf("current = %d, want the chat it was given", got)
	}
	// Neither of these may write: the first window owns the file and the badge.
	a.railWidth = 400
	a.prefs.RailWidth = 400
	path := prefsPath()
	os.Remove(path)
	a.savePrefsLater()
	a.syncBadge()
	time.Sleep(80 * time.Millisecond)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("secondary window wrote %s: %v", path, err)
	}
	if a.badge != 0 {
		t.Fatalf("secondary window set the badge to %d", a.badge)
	}
}

// TestRailOffersASecondWindow checks the rail's menu item reaches the factory
// main.go installs.
func TestRailOffersASecondWindow(t *testing.T) {
	a := snapshotData(t)
	var opened []int
	SetWindowOpener(func(cfg Config, client *api.Client, conversationID int) {
		opened = append(opened, conversationID)
	})
	t.Cleanup(func() { SetWindowOpener(nil) })
	// The factory is what the menu item needs, so a call proves the wiring.
	a.openInNewWindow(7)
	if len(opened) != 1 || opened[0] != 7 {
		t.Fatalf("opened %v, want [7]", opened)
	}
}
