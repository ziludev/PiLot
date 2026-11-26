package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/i18n"
)

// TestTipBesideTheLogoStaysOffTheTrafficLights: the folded rail's top control sits
// under the window's traffic lights, and a tip drawn above it covers them. The tip
// goes to the right of the control instead — past the rail's own column.
func TestTipBesideTheLogoStaysOffTheTrafficLights(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.toggleRailCollapsed()
	tt := ui.NewTester(a.View, 900, 620)
	tt.Frame()
	tt.Move(28, 54) // the folded logo, in the rail's top strip
	tt.Advance(800 * time.Millisecond)
	tt.Frame()
	rect, ok := tt.Find(i18n.T("expandRail"))
	if !ok {
		t.Fatalf("hovering the logo showed no tip; drew %v", tt.Texts())
	}
	if rect.X < railCollapsedWidth {
		t.Fatalf("the tip sits at x=%.0f, inside the rail and over the traffic lights", rect.X)
	}
	writePNG(t, "/tmp/clientv2-rail-tip.png", tt.Image())
}
