package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
	"pilot/internal/protocol"
)

// mentionApp is a window with one checkout and one other chat, so both kinds
// of candidate have something to match.
func mentionApp(t *testing.T) *App {
	t.Helper()
	i18n.Set(i18n.EN)
	a := newApp(Config{})
	a.setSurfaces([]protocol.Surface{
		{ConversationID: 1, Kind: "client", Title: "prefs bug"},
		{ConversationID: 2, Kind: "client", Title: "deploy checklist"},
	})
	a.active = 1
	a.repoFiles.all = []string{
		"internal/app/prefs.go",
		"internal/app/prefs_test.go",
		"internal/i18n/dict.go",
	}
	return a
}

// TestMentionOffersFilesAndChats: an unfinished @word opens the popover, the
// files whose name starts with it come before the ones that merely contain it,
// and a chat title is a candidate too.
func TestMentionOffersFilesAndChats(t *testing.T) {
	a := mentionApp(t)
	a.draft = "why is @prefs"
	a.syncMentions()
	if !a.mention.open {
		t.Fatal("an unfinished @word opened nothing")
	}
	first := a.mention.hits[0]
	if first.insert != "@internal/app/prefs.go" || first.label != "prefs.go" || first.dir != "internal/app" {
		t.Fatalf("first candidate %+v", first)
	}
	var sawChat bool
	for _, hit := range a.mention.hits {
		if hit.chat && hit.insert == "@prefs bug" {
			sawChat = true
		}
	}
	if !sawChat {
		t.Fatalf("chat title missing from %+v", a.mention.hits)
	}
}

// TestMentionInsertsThePath: Enter takes the row and the sentence carries on
// with the path where the @word was; the popover closes with it.
func TestMentionInsertsThePath(t *testing.T) {
	a := mentionApp(t)
	a.draft = "the bug in @prefs.go please"
	// A space already followed the word, so there is nothing to complete.
	a.syncMentions()
	if a.mention.open {
		t.Fatal("a finished word kept the popover open")
	}

	a.draft = "the bug in @prefs"
	a.syncMentions()
	if !a.composerKey(ui.TextKey{Key: ui.KeyEnter}) {
		t.Fatal("Enter was not taken by the popover")
	}
	if want := "the bug in @internal/app/prefs.go "; a.draft != want {
		t.Fatalf("draft %q, want %q", a.draft, want)
	}
	if a.mention.open {
		t.Fatal("the popover stayed open after picking")
	}
}

// TestMentionEscLeavesTheText: Escape puts the list away and changes nothing;
// the next Enter sends, as it would have before the @ was typed.
func TestMentionEscLeavesTheText(t *testing.T) {
	a := mentionApp(t)
	a.draft = "look at @dict"
	a.syncMentions()
	if !a.mention.open {
		t.Fatal("no candidates for @dict")
	}
	if !a.composerKey(ui.TextKey{Key: ui.KeyEscape}) {
		t.Fatal("Escape was not taken by the popover")
	}
	if a.draft != "look at @dict" {
		t.Fatalf("Escape changed the text to %q", a.draft)
	}
	if a.mention.open {
		t.Fatal("Escape left the popover open")
	}
}

// TestMentionWalksWithArrows: Down moves the choice and wraps at the end.
func TestMentionWalksWithArrows(t *testing.T) {
	a := mentionApp(t)
	a.draft = "@prefs"
	a.syncMentions()
	if len(a.mention.hits) < 2 {
		t.Fatalf("need two candidates, got %+v", a.mention.hits)
	}
	a.composerKey(ui.TextKey{Key: ui.KeyDown})
	if a.mention.index != 1 {
		t.Fatalf("Down chose %d", a.mention.index)
	}
	a.composerKey(ui.TextKey{Key: ui.KeyUp})
	if a.mention.index != 0 {
		t.Fatalf("Up chose %d", a.mention.index)
	}
}
