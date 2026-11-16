package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// noRawKeys fails when a view draws a dictionary key instead of its text. The
// chat menu once asked for "renameChat" and "archiveChat", keys the React
// dictionary never had, so the buttons read "renameChat" and "archiveChat".
func noRawKeys(t *testing.T, tt *ui.Tester) {
	t.Helper()
	for _, s := range tt.Texts() {
		if i18n.IsKey(s) {
			t.Errorf("text %q is a dictionary key, not a label", s)
		}
	}
}

// TestChatMenuDrawsLabels covers the menu the auditor caught: rename, archive
// and worktree cleanup, with the archive entry flipping to unarchive.
func TestChatMenuDrawsLabels(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	a.current = 7
	a.surfaces = []protocol.Surface{{ConversationID: 7, SurfaceID: "s7", Title: "想摸鱼"}}
	a.menu.open = true
	a.menu.title = "想摸鱼"

	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Dark())
		a.chatMenuDialog(c, theme.Dark())
	}, 480, 260)
	tt.Frame()

	for _, key := range []string{"rename", "save", "archive", "cleanupWorktree", "cancel"} {
		if want := i18n.T(key); !tt.HasText(want) {
			t.Errorf("chat menu does not show %q (%s)", want, key)
		}
	}
	noRawKeys(t, tt)
}

// TestChatMenuOffersUnarchive is the archived case: the entry has to offer the
// opposite action, as the React menu does.
func TestChatMenuOffersUnarchive(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	a.current = 7
	a.surfaces = []protocol.Surface{{ConversationID: 7, SurfaceID: "s7", Archived: true}}
	a.menu.open = true

	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Dark())
		a.chatMenuDialog(c, theme.Dark())
	}, 480, 260)
	tt.Frame()

	if !tt.HasText(i18n.T("unarchive")) {
		t.Fatalf("archived chat offers %q, want %q", i18n.T("archive"), i18n.T("unarchive"))
	}
	if tt.HasText(i18n.T("archive")) {
		t.Fatal("archived chat still offers archive")
	}
}

// TestImageDialogDrawsLabels covers the other dialog the app builds by hand.
func TestImageDialogDrawsLabels(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	a.imageOpen = true

	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Light())
		a.imageDialog(c, theme.Light())
	}, 480, 220)
	tt.Frame()

	if !tt.HasText(i18n.T("attach")) {
		t.Fatalf("image dialog does not show %q", i18n.T("attach"))
	}
	noRawKeys(t, tt)
}
