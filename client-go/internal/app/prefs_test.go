package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// withPrefsFile points the window's preference file at a temp dir for one test.
func withPrefsFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := prefsPath
	prefsPath = func() string { return filepath.Join(dir, "window.json") }
	t.Cleanup(func() { prefsPath = old })
	return filepath.Join(dir, "window.json")
}

// TestPrefsRoundTripKeepsEverySetting: the file is the struct now, so a field
// added to prefs is read back without a second list to keep in step. Quiet and
// dailyCap were exactly the fields that drifted and came back empty.
func TestPrefsRoundTripKeepsEverySetting(t *testing.T) {
	path := withPrefsFile(t)
	want := prefs{
		Lang:     "en",
		Theme:    "dark",
		Rail:     320,
		Sections: map[string]bool{"other": true},
		Recent:   []string{"anthropic/claude-sonnet-4.5"},
		Kept:     []int{12},
		Pinned:   []int{7},
		Group:    groupProject,
		Quiet:    "off",
		DailyCap: "off",
		Silent:   true,
		Seen:     map[string]int{"7": 120},
	}
	savePrefs(want)

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("nothing written: %v", err)
	}
	got := readPrefs()
	if got.Lang != want.Lang || got.Theme != want.Theme || got.Rail != want.Rail {
		t.Fatalf("window settings came back as %+v", got)
	}
	if got.Quiet != want.Quiet || got.DailyCap != want.DailyCap || !got.Silent {
		t.Fatalf("delivery settings came back as %+v", got)
	}
	if got.Group != want.Group || len(got.Pinned) != 1 || got.Pinned[0] != 7 || len(got.Kept) != 1 {
		t.Fatalf("rail settings came back as %+v", got)
	}
	if got.Seen["7"] != 120 || !got.Sections["other"] {
		t.Fatalf("read marks came back as %+v", got)
	}
}

// TestPrefsReadsTheOldCapitalisedFile: the first writes used Go's field names.
// Those keys still parse (encoding/json matches them case-insensitively), so a
// window that upgrades keeps its choice instead of starting over.
func TestPrefsReadsTheOldCapitalisedFile(t *testing.T) {
	path := withPrefsFile(t)
	old := `{"Lang":"en","Theme":"dark","Rail":373,"quietHours":"off","seen":{"7":9}}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readPrefs()
	if got.Lang != "en" || got.Theme != "dark" || got.Rail != 373 {
		t.Fatalf("old file lost the window settings: %+v", got)
	}
	if got.Quiet != "off" {
		t.Fatalf("old file lost the quiet hours: %+v", got)
	}
}

// TestPrefsWrittenKeysAreTheReadKeys: every key savePrefs writes must parse
// back. A field with no json tag is written as "Quiet", which is still read,
// but the check keeps the file readable by anything else that expects the
// documented names.
func TestPrefsWrittenKeysAreTheReadKeys(t *testing.T) {
	withPrefsFile(t)
	data, err := json.Marshal(prefs{Lang: "en", Quiet: "off", DailyCap: "off"})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"lang", "quietHours", "dailyCap"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("saved file has no %q; keys are %v", key, raw)
		}
	}
}

// TestDraftSurvivesSwitchingChats: half a message written in one chat is still
// there on the way back, and the chat left with an empty composer keeps none.
func TestDraftSurvivesSwitchingChats(t *testing.T) {
	i18n.Set(i18n.EN)
	withPrefsFile(t)
	a := fixtureApp(t, false)
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 1, Kind: "client", Title: "one"},
		{ConversationID: 2, Kind: "client", Title: "two"},
	})
	a.active = 1
	a.draft = "half a question"
	a.selectConversation(2)
	if a.draft != "" {
		t.Fatalf("chat two opened with %q", a.draft)
	}
	a.selectConversation(1)
	if a.draft != "half a question" {
		t.Fatalf("chat one came back with %q", a.draft)
	}
	if got := readPrefs().Drafts["1"]; got != "half a question" {
		t.Fatalf("draft was not written to the file: %q", got)
	}
	// Answering empties the box and the store: reopening must not restore it.
	a.draft = ""
	a.prefs.setDraft(1, "")
	savePrefs(a.prefs)
	if _, ok := readPrefs().Drafts["1"]; ok {
		t.Fatal("a sent message stayed in the draft store")
	}
}
