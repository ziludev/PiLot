package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

// prefs is what survives a restart. The React client kept these in
// localStorage (agent.surfaceOrder, agent.pinned, agent.lang,
// agent.surfaceFilter); a desktop app has a file instead.
type prefs struct {
	Lang          string   `json:"lang,omitempty"`
	Theme         string   `json:"theme,omitempty"` // "system", "light" or "dark"
	Pinned        []int    `json:"pinned,omitempty"`
	Order         []int    `json:"order,omitempty"`
	SurfaceFilter string   `json:"surfaceFilter,omitempty"`
	RailWidth     float32  `json:"railWidth,omitempty"`
	SideWidth     float32  `json:"sideWidth,omitempty"`
	Favorites     []string `json:"favorites,omitempty"` // model keys, provider/id
	Recents       []string `json:"recents,omitempty"`
	// Unread holds the chats whose answers arrived while nobody was looking;
	// the rail dots them until they are opened.
	Unread []int `json:"unread,omitempty"`
	// LastChat is the chat that was open when the app last quit, so a restart
	// lands where the reader left off instead of at the newest chat.
	LastChat int `json:"lastChat,omitempty"`
}

// prefsPath is ~/.pitta/ui.json, next to the app's own state.
func prefsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pitta", "ui.json")
}

// loadPrefs reads the saved preferences; a missing or broken file is not an
// error the user needs to see.
func loadPrefs() prefs {
	var p prefs
	path := prefsPath()
	if path == "" {
		return p
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("prefs: read %s: %v", path, err)
		}
		return p
	}
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("prefs: parse %s: %v", path, err)
		return prefs{}
	}
	return p
}

// savePrefs writes the preferences atomically: a crash mid-write must not
// leave the window unable to start.
func savePrefs(p prefs) {
	path := prefsPath()
	if path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("prefs: mkdir: %v", err)
		return
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		log.Printf("prefs: encode: %v", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		log.Printf("prefs: write %s: %v", tmp, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		log.Printf("prefs: rename %s: %v", path, err)
	}
}

// savePrefsLater persists the current preferences off the UI thread.
func (a *App) savePrefsLater() {
	if a.secondary {
		// One window owns the file; a second writing it would race.
		return
	}
	p := a.prefs
	p.RailWidth = a.railWidth
	p.SideWidth = a.sideWidth
	p.Favorites = a.modelsFav
	p.Recents = a.modelsRecent
	go savePrefs(p)
}
