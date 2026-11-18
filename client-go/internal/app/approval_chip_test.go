package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// railApprovalFrame draws the rail on its own. The chip sits in the rail's top
// area, which is what the app's layout makes of it at 280pt wide.
func railApprovalFrame(a *App) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Light())
		ui.Row(c).Width(280).FillHeight().Children(func() { a.rail(c) })
	}, 300, 640)
	tt.Frame()
	return tt
}

// TestSnapshotApprovalChipInTheRail checks the standing way into a waiting call.
// The approval bar lives in the composer, so a window looking at another chat
// used to hide it entirely; the chip says how many wait and takes the window to
// the chat that owns them.
func TestSnapshotApprovalChipInTheRail(t *testing.T) {
	a := snapshotData(t)
	a.current = 11 // the window is on another chat than the waiting call's
	now := time.Now().UnixMilli()
	a.approvals = []protocol.PendingApproval{
		{CallID: "call-1", SurfaceID: "feishu:12", GrantKey: "bash:npm", Tool: "bash",
			Summary: "npm run migrate", Reason: "writes the database", CreatedAt: now - 90_000},
		{CallID: "call-2", SurfaceID: "feishu:12", GrantKey: "bash:rm", Tool: "bash",
			Summary: "rm -rf ./build", Reason: "destructive command", CreatedAt: now - 30_000},
	}
	i18n.Set(i18n.ZH)
	tt := railApprovalFrame(a)

	chip := "2 " + i18n.T("needsApproval")
	if !tt.HasText(chip) {
		t.Fatalf("the rail does not show %q; texts=%q", chip, tt.Texts())
	}
	writePNG(t, "/tmp/pilot-approval-chip.png", tt.Image())

	// One click on the chip opens the chat the calls belong to, where the
	// approval bar is drawn.
	if err := tt.Click(railApprovalLabel); err != nil {
		t.Fatalf("the chip could not be clicked: %v", err)
	}
	if got := a.currentValue(); got != 12 {
		t.Fatalf("the chip opened chat %d, want 12", got)
	}
}

// TestApprovalChipIsNotThereWithoutApprovals: a rail with nothing waiting has no
// chip and no space for one, so the chat list starts where it always did.
func TestApprovalChipIsNotThereWithoutApprovals(t *testing.T) {
	a := snapshotData(t)
	a.approvals = nil
	i18n.Set(i18n.ZH)
	empty := railApprovalFrame(a)
	first, ok := empty.Access("why is the sky blue")
	if !ok {
		t.Fatalf("the chat list is missing; texts=%q", empty.Texts())
	}
	writePNG(t, "/tmp/pilot-approval-none.png", empty.Image())

	a.approvals = []protocol.PendingApproval{{
		CallID: "call-1", SurfaceID: "feishu:12", Tool: "bash",
		Summary: "npm run migrate", CreatedAt: time.Now().UnixMilli(),
	}}
	withChip := railApprovalFrame(a)
	after, ok := withChip.Access("why is the sky blue")
	if !ok {
		t.Fatalf("the chat list is missing; texts=%q", withChip.Texts())
	}
	if after.Bounds.Y == first.Bounds.Y {
		t.Fatalf("the chip takes no room: the list starts at y=%.1f with and without it", after.Bounds.Y)
	}
	if got := after.Bounds.Y - first.Bounds.Y; got < 20 || got > 40 {
		t.Fatalf("the chip moved the list %.1fpx, want one row of about 28", got)
	}
	if !withChip.HasText("1 " + i18n.T("needsApproval")) {
		t.Fatalf("the chip does not say how many wait; texts=%q", withChip.Texts())
	}
}
