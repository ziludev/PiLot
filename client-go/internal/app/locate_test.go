package app

import (
	"testing"

	"pilot/internal/i18n"
)

// TestWorktreePathUsesWhatTheDrawerAlreadyRead: the drawer holds the path after
// a changes read, so the menu should not ask the bridge again — the fixture has
// no reachable bridge, so a second read would come back an error.
func TestWorktreePathUsesWhatTheDrawerAlreadyRead(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.changes.conv = a.active
	a.changes.workspace = "/tmp/conv-12"

	path, err := a.worktreePath()
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if path != "/tmp/conv-12" {
		t.Fatalf("path = %q", path)
	}
}

// TestWorktreePathRefusesWithNoChat: the verbs are menu items that stay enabled,
// so the guard has to be in the handler, not in the menu.
func TestWorktreePathRefusesWithNoChat(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.active = 0
	if _, err := a.worktreePath(); err == nil {
		t.Fatal("no chat, but a path came back")
	}
}

// TestEditorArgsHonoursPilotEditor: the probe order is a guess about what people
// install, so the one way to state the answer has to win over it.
func TestEditorArgsHonoursPilotEditor(t *testing.T) {
	t.Setenv("PILOT_EDITOR", "Nova")
	a := fixtureApp(t, false)
	got := a.editorArgs()
	if len(got) != 2 || got[0] != "-a" || got[1] != "Nova" {
		t.Fatalf("args = %#v", got)
	}
}

// TestEditorArgsNeverNamesAnAppThatIsNotInstalled: `open -a` on a missing bundle
// fails, and a failed menu item reads as a broken window. The last resort is the
// Finder, which is always there.
func TestEditorArgsNeverNamesAnAppThatIsNotInstalled(t *testing.T) {
	t.Setenv("PILOT_EDITOR", "")
	a := fixtureApp(t, false)
	got := a.editorArgs()
	if len(got) == 2 && !installed(got[1]) {
		t.Fatalf("picked an app that is not installed: %q", got[1])
	}
	if len(got) != 0 && len(got) != 2 {
		t.Fatalf("args = %#v", got)
	}
}
