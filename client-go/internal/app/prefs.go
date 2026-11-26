package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"pilot/internal/i18n"
)

// prefs is the little of the earlier client's preference file this window uses:
// the language and the theme have no dialog here, so they need somewhere to live
// between launches.
type prefs struct {
	Lang  string `json:"lang,omitempty"`
	Theme string `json:"theme,omitempty"` // "dark", "light", or "" to follow macOS
	Rail  int    `json:"rail,omitempty"`  // the rail's width in points; 0 for the default
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
	// Pinned is the chats the user put at the top of the rail. A pin is a place,
	// not a property of the conversation, so it lives here and never reaches the
	// bridge — and a pinned chat is never filed away for being old.
	Pinned []int `json:"pinnedChats,omitempty"`
	// Group is how the rail files chats that are not pinned: by what they are
	// ("" — the default) or by the project they belong to ("project").
	Group string `json:"railGroup,omitempty"`
	// Quiet is when the window keeps its news to itself: "off", or a window of
	// local hours like "22-8". Absent means 22-8, because a personal agent that
	// wakes you for a finished turn is the failure mode, not the feature.
	Quiet string `json:"quietHours,omitempty"`
	// DailyCap is how many banners a day may be spent on finished turns: "off",
	// or a number. Absent means five. Past it the rail's dot still holds the
	// news — it just stops interrupting.
	DailyCap string `json:"dailyCap,omitempty"`
	// Silent is the banners switched off. Written the other way round because
	// the file is read from disk where a missing field means false — and the
	// window should tell you about a finished turn until you ask it not to.
	Silent bool `json:"silent,omitempty"`
	// Seen is the newest entry the user had on screen in each chat, keyed by
	// conversation: what makes the line in the transcript survive a restart.
	Seen map[string]int `json:"seen,omitempty"`
	// Drafts is what the composer held in each chat when the user last left
	// it, keyed by conversation. Writing half a message and going to answer
	// something else is the normal way this window is used, so leaving must
	// not be the same as discarding.
	Drafts map[string]string `json:"drafts,omitempty"`
}

// prefsPath is this window's own file. The earlier Rust client's ui.json is left
// alone: it holds forty keys this window never reads, and adopting it would mean
// inheriting its choices.
var prefsPath = func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pilot", "window.json")
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
	// The file is the struct: a hand-written copy of the field list is how
	// quietHours and dailyCap were saved but never read back.
	var p prefs
	if err := json.Unmarshal(data, &p); err != nil {
		log.Printf("prefs: parse %s: %v", path, err)
		return prefs{}
	}
	return p
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

// sectionClosed is whether a chat section is drawn folded. 进行中 and 置顶 are
// the sets the user is working through, so they start open; a project section
// starts open too, since picking that grouping is a request to see the projects.
// Everything else starts folded, and an explicit choice in the file wins over all.
func (p prefs) sectionClosed(key string) bool {
	if open, ok := p.Sections[key]; ok {
		return !open
	}
	switch {
	case key == sectionActive, key == sectionPinned:
		return false
	case strings.HasPrefix(key, projectSectionPrefix):
		return false
	}
	return true
}

// draftAt is what the composer held in a chat, empty when it held nothing.
func (p prefs) draftAt(convID int) string {
	return p.Drafts[itoa(convID)]
}

// setDraft records a chat's composer text; an empty draft is a deletion, so the
// file does not fill with blank entries for every chat ever visited.
func (p *prefs) setDraft(convID int, text string) {
	key := itoa(convID)
	if text == "" {
		delete(p.Drafts, key)
		return
	}
	if p.Drafts == nil {
		p.Drafts = map[string]string{}
	}
	p.Drafts[key] = text
}

// pinnedChat is whether this chat sits at the top of the rail.
func (p prefs) pinnedChat(convID int) bool {
	for _, id := range p.Pinned {
		if id == convID {
			return true
		}
	}
	return false
}

// pinChat puts a chat at the top, or takes it back down. The list is read on
// every frame, so a second pin of the same chat must not append twice.
func (p *prefs) pinChat(convID int, pinned bool) {
	if pinned {
		if p.pinnedChat(convID) {
			return
		}
		p.Pinned = append([]int{convID}, p.Pinned...)
		return
	}
	for i, id := range p.Pinned {
		if id == convID {
			p.Pinned = append(p.Pinned[:i], p.Pinned[i+1:]...)
			return
		}
	}
}

// groupByProject is whether the rail files chats under their project.
func (p prefs) groupByProject() bool { return p.Group == groupProject }

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
// quietWindow is the hours the window does not speak, from-to in local time and
// wrapping midnight; equal hours mean it is never quiet. A malformed window falls
// back to the default rather than to silence: the file is hand-editable, and a
// typo should not mean a week of missed news.
func (p prefs) quietWindow() (int, int) {
	if p.Quiet == "off" {
		return 0, 0
	}
	if p.Quiet == "" {
		return 22, 8
	}
	from, to, ok := strings.Cut(p.Quiet, "-")
	if !ok {
		return 22, 8
	}
	start, errFrom := strconv.Atoi(strings.TrimSpace(from))
	end, errTo := strconv.Atoi(strings.TrimSpace(to))
	if errFrom != nil || errTo != nil || start < 0 || start > 23 || end < 0 || end > 23 {
		return 22, 8
	}
	return start, end
}

// bannerBudget is how many finished turns a day may be announced; 0 is no
// ceiling. Same grace for a typo as the quiet window.
func (p prefs) bannerBudget() int {
	switch p.DailyCap {
	case "off":
		return 0
	case "":
		return 5
	}
	n, err := strconv.Atoi(strings.TrimSpace(p.DailyCap))
	if err != nil || n <= 0 {
		return 5
	}
	return n
}

func (p prefs) dark(systemDark bool) bool {
	switch p.Theme {
	case "dark":
		return true
	case "light":
		return false
	}
	return systemDark
}

// prefsMu serialises writes. Two saves can overlap when one lands while
// another is still renaming the temp file, and the loser's write is gone.
var prefsMu sync.Mutex

// savePrefs writes them back, whole-file: the file is this window's own. It is
// called on the frame thread, not in a goroutine: a save that is still queued
// when the window closes is a setting the next launch never sees.
func savePrefs(p prefs) {
	prefsMu.Lock()
	defer prefsMu.Unlock()
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
