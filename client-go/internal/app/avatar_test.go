package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/avatar"
	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
)

func TestRailStatusFollowsTheBridge(t *testing.T) {
	i18n.Set(i18n.EN)
	a := New(Config{})
	a.conn = ConnOpen
	if got := a.railStatus(); got != i18n.T("idle") {
		t.Fatalf("open bridge says %q", got)
	}
	a.busy = true
	if got := a.railStatus(); got != i18n.T("busy") {
		t.Fatalf("busy chat says %q", got)
	}
	a.busy, a.conn, a.downReason = false, ConnDown, "error"
	if got := a.railStatus(); got != i18n.T("bridgeFailed") {
		t.Fatalf("failed bridge says %q", got)
	}
}

func TestAvatarMoodsFollowTheRun(t *testing.T) {
	a := New(Config{})
	if mood, tone := a.avatarLook(); mood != avatar.MoodIdle || tone != avatar.Tones.Idle {
		t.Fatalf("idle avatar = %v %v", mood, tone)
	}
	a.live = &protocol.LiveDoc{Run: []byte(`{"x":1}`)}
	if mood, _ := a.avatarLook(); mood != avatar.MoodThinking {
		t.Fatalf("running avatar = %v", mood)
	}
	a.live = nil
	if mood, tone := a.avatarLook(); mood != avatar.MoodHappy || tone != avatar.Tones.Done {
		t.Fatalf("finished avatar = %v %v", mood, tone)
	}
	a.moodUntil = time.Now().Add(-time.Second)
	if mood, _ := a.avatarLook(); mood != avatar.MoodIdle {
		t.Fatalf("avatar after the smile = %v", mood)
	}
	a.tasks = []protocol.Task{{ID: 1, State: "running"}}
	if _, tone := a.avatarLook(); tone != avatar.Tones.Delegated {
		t.Fatalf("delegated tone = %v", tone)
	}
}

func TestRailHeaderDrawsAvatarAndStatus(t *testing.T) {
	i18n.Set(i18n.EN)
	a := New(Config{})
	a.conn = ConnOpen
	a.tasks = []protocol.Task{{ID: 3, State: "running"}}
	tt := ui.NewTester(func(c *ui.Context) {
		a.railHeader(c, theme.Current(c))
	}, 260, 60)
	tt.Frame()
	if !tt.HasText(i18n.T("appName")) || !tt.HasText(i18n.T("idle")) {
		t.Fatalf("header texts = %q", tt.Texts())
	}
}
