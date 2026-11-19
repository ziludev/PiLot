package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"pitta/internal/i18n"
)

// prefs is the little of the earlier client's preference file this window uses:
// the language and the theme have no dialog here, so they need somewhere to live
// between launches.
type prefs struct {
	Lang  string
	Theme string // "dark", "light", or "" to follow macOS
}

// prefsPath is this window's own file. The earlier client's ~/.pitta/ui.json is
// left alone: it holds forty keys this window never reads, and adopting it would
// mean inheriting its choices.
var prefsPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pitta", "window.json")
}

func readPrefs() prefs {
	path := prefsPath()
	if path == "" {
		return prefs{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("prefs: read %s: %v", path, err)
		}
		return prefs{}
	}
	var p struct {
		Lang  string `json:"lang"`
		Theme string `json:"theme"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("prefs: parse %s: %v", path, err)
		return prefs{}
	}
	return prefs{Lang: p.Lang, Theme: p.Theme}
}

// loadPrefs reads the two settings this window keeps.
func loadPrefs() prefs {
	return readPrefs()
}

// applyLang makes the saved language the window's, if there is one.
func (p prefs) applyLang() {
	switch p.Lang {
	case string(i18n.ZH):
		i18n.Set(i18n.ZH)
	case string(i18n.EN):
		i18n.Set(i18n.EN)
	}
}

// dark is whether to draw dark: the saved choice, else the system's.
func (p prefs) dark(systemDark bool) bool {
	switch p.Theme {
	case "dark":
		return true
	case "light":
		return false
	}
	return systemDark
}

// savePrefs writes them back, whole-file: the file is this window's own.
func savePrefs(p prefs) {
	path := prefsPath()
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		log.Printf("prefs: encode: %v", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Printf("prefs: mkdir: %v", err)
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
