package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/brand"
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

func TestMarkMoodsFollowTheRun(t *testing.T) {
	a := New(Config{})
	if mood, delegated := a.markLook(); mood != brand.MoodIdle || delegated {
		t.Fatalf("idle mark = %v delegated=%t", mood, delegated)
	}
	a.live = &protocol.LiveDoc{Run: []byte(`{"x":1}`)}
	if mood, _ := a.markLook(); mood != brand.MoodThinking {
		t.Fatalf("running mark = %v", mood)
	}
	a.live = nil
	if mood, _ := a.markLook(); mood != brand.MoodHappy {
		t.Fatalf("finished mark = %v", mood)
	}
	a.moodUntil = time.Now().Add(-time.Second)
	if mood, _ := a.markLook(); mood != brand.MoodIdle {
		t.Fatalf("mark after the beat = %v", mood)
	}
	a.tasks = []protocol.Task{{ID: 1, State: "running"}}
	if _, delegated := a.markLook(); !delegated {
		t.Fatal("a background task should tint the mark")
	}
}

func TestRailHeaderDrawsTheMarkAndStatus(t *testing.T) {
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
