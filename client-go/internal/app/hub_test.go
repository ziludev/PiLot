package app

import (
	"strings"
	"testing"
	"time"

	"pitta/internal/protocol"
)

// TestSurfacesReachTheOtherWindows checks a list loaded in one window lands in
// the other, which is how archiving a chat in one window empties it from the
// rail of both.
func TestSurfacesReachTheOtherWindows(t *testing.T) {
	one, two := snapshotData(t), snapshotData(t)
	one.previewImage, two.previewImage = -1, -1
	subscribeSurfaces(one)
	subscribeSurfaces(two)
	t.Cleanup(func() { unsubscribeSurfaces(one); unsubscribeSurfaces(two) })

	shared := []protocol.Surface{{ConversationID: 77, SurfaceID: "s77", Title: "archived elsewhere", Archived: true}}
	broadcastSurfaces(one, shared)
	if len(two.surfaces) != 1 || two.surfaces[0].ConversationID != 77 {
		t.Fatalf("the other window kept %d surfaces", len(two.surfaces))
	}
	if len(one.surfaces) == 1 && one.surfaces[0].ConversationID == 77 {
		t.Fatal("the window that loaded the list had it replaced")
	}
}

// TestTitleCarriesTheApprovalWait checks the title says how long the oldest
// approval has waited, and counts unread chats when nothing waits.
func TestTitleCarriesTheApprovalWait(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	a.approvals = nil
	if got := a.waitedSuffix(); got != "" {
		t.Fatalf("an empty queue reported %q", got)
	}
	a.approvals = []protocol.PendingApproval{{CallID: "c1", CreatedAt: time.Now().UnixMilli() - 42000}}
	if got := a.waitedSuffix(); got != "42s" {
		t.Fatalf("waited %q, want 42s", got)
	}
	a.approvals = nil
	a.unread = map[int]bool{1: true, 2: false, 3: true}
	if got := a.unreadCount(); got != 2 {
		t.Fatalf("unread count %d, want 2", got)
	}
}

// TestApprovalsReachTheOtherWindows checks a resolved approval disappears from
// both windows at once instead of lingering until the other window's own poll.
func TestApprovalsReachTheOtherWindows(t *testing.T) {
	one, two := snapshotData(t), snapshotData(t)
	one.previewImage, two.previewImage = -1, -1
	subscribeSurfaces(one)
	subscribeSurfaces(two)
	t.Cleanup(func() { unsubscribeSurfaces(one); unsubscribeSurfaces(two) })

	two.approvals = []protocol.PendingApproval{{CallID: "call_1", Tool: "bash"}}
	broadcastApprovals(one, nil)
	if len(two.approvals) != 0 {
		t.Fatalf("the other window still shows %d approvals", len(two.approvals))
	}
	left := []protocol.PendingApproval{{CallID: "call_2", Tool: "edit"}}
	broadcastApprovals(one, left)
	if len(two.approvals) != 1 || two.approvals[0].CallID != "call_2" {
		t.Fatalf("the other window shows %+v", two.approvals)
	}
}

// TestLongWaitBlinksInTheTitle checks a wait past five minutes alternates its
// marker instead of sitting still, and that a fresh wait does not.
func TestLongWaitBlinksInTheTitle(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	a.surfaces = []protocol.Surface{{ConversationID: 5, SurfaceID: "s5", Title: "chat"}}
	a.current = 5
	a.approvals = nil
	if got := a.titleText(); got != "Pitta · chat" {
		t.Fatalf("idle title %q", got)
	}
	a.approvals = []protocol.PendingApproval{{CallID: "c1", CreatedAt: time.Now().UnixMilli() - 12_000}}
	if got := a.titleText(); got != "⚠ 12s · Pitta · chat" {
		t.Fatalf("short wait title %q", got)
	}
	a.approvals = []protocol.PendingApproval{{CallID: "c1", CreatedAt: time.Now().UnixMilli() - 420_000}}
	a.titleTick = 0
	on := a.titleText()
	a.titleTick = 1
	off := a.titleText()
	if on == off {
		t.Fatalf("a seven-minute wait does not blink: %q", on)
	}
	if !strings.Contains(on, "420s") || !strings.Contains(off, "420s") {
		t.Fatalf("the age is missing: %q / %q", on, off)
	}
}

// TestClickingASharedChatRaisesItsWindow checks the rail does not load a chat a
// second time: a chat that has a window of its own comes forward instead.
func TestClickingASharedChatRaisesItsWindow(t *testing.T) {
	main, second := snapshotData(t), snapshotData(t)
	main.previewImage, second.previewImage = -1, -1
	second.secondary = true
	raised := 0
	noteOwnWindow(second, 13, func() { raised++ })
	t.Cleanup(func() { forgetOwnWindow(13) })

	if !focusOwnWindow(main, 13) || raised != 1 {
		t.Fatalf("the window of chat 13 was not raised (%d)", raised)
	}
	// The owner's own click must load the chat, not raise itself forever.
	if focusOwnWindow(second, 13) {
		t.Fatal("the window raised itself")
	}
	forgetOwnWindow(13)
	if focusOwnWindow(main, 13) {
		t.Fatal("a closed window is still remembered")
	}
}
