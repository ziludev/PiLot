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
	Rail  int    // the rail's width in points; 0 for the default
	// Sections is which chat sections the user has opened, by key. Absent means
	// the section's default, so a section added later gets a sane first run.
	Sections map[string]bool `json:"sections"`
	// Recent is the models chosen last, most recent first, so the picker opens
	// on what this person actually uses rather than on the catalog's own order.
	Recent []string `json:"recentModels,omitempty"`
	// Kept is the chats taken back out of 已归档 by hand. The rail files a chat
	// away once it has sat a week; this list is where the user says "not this
	// one", and it has to survive a restart or the chat would go again.
	Kept []int `json:"keptChats,omitempty"`
	// Silent is the banners switched off. Written the other way round because
	// the file is read from disk where a missing field means false — and the
	// window should tell you about a finished turn until you ask it not to.
	Silent bool `json:"silent,omitempty"`
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
		Lang     string          `json:"lang"`
		Theme    string          `json:"theme"`
		Rail     int             `json:"rail"`
		Sections map[string]bool `json:"sections"`
		Recent   []string        `json:"recentModels"`
		Kept     []int           `json:"keptChats"`
		Silent   bool            `json:"silent"`
	}
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("prefs: parse %s: %v", path, err)
		return prefs{}
	}
	return prefs{Lang: p.Lang, Theme: p.Theme, Rail: p.Rail, Sections: p.Sections, Recent: p.Recent, Kept: p.Kept, Silent: p.Silent}
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

// railWidth is the width to draw the rail at, the saved one if it is sane.
func (p prefs) railWidth() float32 {
	if p.Rail < railMin || p.Rail > railMax {
		return railWidth
	}
	return float32(p.Rail)
}

// sectionClosed is whether a chat section is drawn folded. 进行中 is the set the
// user is working through, so it starts open and every other section starts
// folded; an explicit choice in the file wins over both.
func (p prefs) sectionClosed(key string) bool {
	if open, ok := p.Sections[key]; ok {
		return !open
	}
	return key != sectionActive
}

// keptChat is whether the user has taken this chat back out of 已归档.
func (p prefs) keptChat(convID int) bool {
	for _, id := range p.Kept {
		if id == convID {
			return true
		}
	}
	return false
}

// keepChat files a chat back into the sections for good. Appending once, since
// the list is read on every frame and would otherwise grow with every click.
func (p *prefs) keepChat(convID int) {
	if p.keptChat(convID) {
		return
	}
	p.Kept = append(p.Kept, convID)
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
