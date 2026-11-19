package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
)

// TestApprovalButtonsStayInsideTheChat pins what a snapshot caught: the ask and
// its three buttons shared one row, and with the side panel open the chat
// column is too narrow for both — "Allow all in this chat" was clipped at the
// column edge and Deny fell outside it entirely, on top of the panel. The ask
// now has its own line and the buttons sit under it, so the buttons have to be
// inside the chat column whatever the panel is doing.
func TestApprovalButtonsStayInsideTheChat(t *testing.T) {
	i18n.Set(i18n.EN)
	a := snapshotData(t)
	a.sideOpen = true
	a.prefs.SideWidth = 280

	const frameWidth = 1180
	tt := ui.NewTester(func(c *ui.Context) { a.pane(c) }, frameWidth, 820)
	tt.Frame()
	tt.Frame()

	writePNG(t, "/tmp/pitta-approval-panel.png", tt.Image())
	if len(a.approvals) != 1 {
		t.Fatalf("fixture approvals = %d, want 1", len(a.approvals))
	}
	// The summary line under the ask; the buttons answer the pair, so they
	// belong below it rather than on its line.
	ask, ok := tt.Find("rm -rf ./build && go build ./...")
	if !ok {
		t.Fatal("the ask line is not painted")
	}
	panelLeft := a.mainPane // the split puts the panel to its right
	for _, label := range []string{i18n.T("allow"), i18n.T("allowSession"), i18n.T("deny")} {
		rect, ok := tt.Find(label)
		// A squeezed label is how the row overflowed before: the widget was
		// clamped to the panel edge and "Deny" painted as "De".
		if rect.W < 30 {
			t.Fatalf("%q is squeezed to %.0f wide — the row overflowed its column", label, rect.W)
		}
		if !ok {
			t.Fatalf("%q is not painted at all", label)
		}
		if rect.X+rect.W > panelLeft {
			t.Fatalf("%q runs under the panel: x=%.0f w=%.0f, panel starts at %.0f",
				label, rect.X, rect.W, panelLeft)
		}
		// Under the ask, not beside it: on one line the three buttons plus the
		// sentence do not fit a chat column narrowed by the panel.
		if rect.Y < ask.Y+ask.H {
			t.Fatalf("%q sits beside the ask (y=%.0f) instead of under it (ask ends at %.0f)",
				label, rect.Y, ask.Y+ask.H)
		}
	}
}
