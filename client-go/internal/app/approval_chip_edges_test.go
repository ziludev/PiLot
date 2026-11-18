package app

import (
	"testing"
	"time"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// approvalsIn parks n waiting calls on one surface, which is all the chip reads.
func approvalsIn(surface string, n int) []protocol.PendingApproval {
	out := make([]protocol.PendingApproval, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, protocol.PendingApproval{
			CallID: surface + "-call-" + string(rune('a'+i)), SurfaceID: surface,
			GrantKey: "bash:rm", Tool: "bash", Summary: "rm -rf ./build",
			Reason: "destructive command", CreatedAt: time.Now().UnixMilli() - 1000,
		})
	}
	return out
}

// TestApprovalChipCountsEveryChat: the chip is the window's only standing sign
// that something waits, so it counts the calls of every chat, not just the one
// on screen, and opens the first chat in the list that has one.
func TestApprovalChipCountsEveryChat(t *testing.T) {
	a := snapshotData(t)
	i18n.Set(i18n.ZH)
	a.current = 11
	a.approvals = append(approvalsIn("client:13", 1), approvalsIn("feishu:12", 2)...)

	tt := railApprovalFrame(a)
	if want := "3 " + i18n.T("needsApproval"); !tt.HasText(want) {
		t.Fatalf("the rail does not show %q; texts=%q", want, tt.Texts())
	}
	if err := tt.Click(railApprovalLabel); err != nil {
		t.Fatalf("the chip could not be clicked: %v", err)
	}
	// 13 is listed first, so it is the one the chip lands on. Landing on 12 as
	// well would be fine as long as the bar is reachable; landing nowhere is not.
	if got := a.currentValue(); got != 13 {
		t.Fatalf("the chip opened chat %d, want the first chat with a call (13)", got)
	}
}

// TestApprovalChipTakesAWayBackHome: once the window sits on the chat that
// owns the calls, the chip is still there (the bar is visible already) and a
// click must not move the window somewhere else.
func TestApprovalChipKeepsTheWindowOnItsOwnChat(t *testing.T) {
	a := snapshotData(t)
	i18n.Set(i18n.ZH)
	a.current = 12
	a.approvals = approvalsIn("feishu:12", 2)

	tt := railApprovalFrame(a)
	if err := tt.Click(railApprovalLabel); err != nil {
		t.Fatalf("the chip could not be clicked: %v", err)
	}
	if got := a.currentValue(); got != 12 {
		t.Fatalf("clicking the chip moved the window to chat %d, want it to stay on 12", got)
	}
}

// TestApprovalChipWithoutAChatIsInert is the error path: a call whose surface
// the client has not seen yet has no chat to open. The chip still has to be
// drawn (the call is real and blocking) and a click must be a no-op rather
// than a jump to conversation 0.
func TestApprovalChipWithoutAChatIsInert(t *testing.T) {
	a := snapshotData(t)
	i18n.Set(i18n.ZH)
	a.current = 11
	a.approvals = approvalsIn("feishu:99", 1)

	tt := railApprovalFrame(a)
	if want := "1 " + i18n.T("needsApproval"); !tt.HasText(want) {
		t.Fatalf("the rail hides a call it cannot place: texts=%q", tt.Texts())
	}
	if err := tt.Click(railApprovalLabel); err != nil {
		t.Fatalf("the chip could not be clicked: %v", err)
	}
	if got := a.currentValue(); got != 11 {
		t.Fatalf("an unplaceable call moved the window to chat %d, want it left on 11", got)
	}
}

// TestApprovalChipCountsDownWithTheQueue: the number is not a snapshot taken
// when the chip appeared — deciding a call makes it drop.
func TestApprovalChipCountsDownWithTheQueue(t *testing.T) {
	a := snapshotData(t)
	i18n.Set(i18n.ZH)
	a.current = 11
	a.approvals = approvalsIn("feishu:12", 2)

	tt := railApprovalFrame(a)
	if !tt.HasText("2 " + i18n.T("needsApproval")) {
		t.Fatalf("chip does not start at 2: %q", tt.Texts())
	}

	a.approveLocally("feishu:12-call-a") // same surgery decideApproval does before reloading
	tt = railApprovalFrame(a)
	if !tt.HasText("1 " + i18n.T("needsApproval")) {
		t.Fatalf("chip did not drop to 1: %q", tt.Texts())
	}
}

// approveLocally drops one call the way a successful decide does, without the
// network round trip the chip itself never sees.
func (a *App) approveLocally(callID string) {
	a.apply(func() {
		kept := a.approvals[:0]
		for _, approval := range a.approvals {
			if approval.CallID != callID {
				kept = append(kept, approval)
			}
		}
		a.approvals = kept
		a.invalidate()
	})
}
