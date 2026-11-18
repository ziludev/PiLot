package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/bridge"
	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// footerFrame draws the rail's footer on its own, which is where the bridge's
// health ends up: one line under the chat list.
func footerFrame(a *App) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Light())
		ui.Column(c).Width(300).Children(func() { a.railFooter(c, theme.Light()) })
	}, 320, 42)
	tt.Frame()
	return tt
}

// TestLiveHealthFooter reads the running bridge's own numbers and draws them
// in the footer, then reads them again a second later to prove the clock in the
// picture belongs to the server rather than to a fixture.
//
// It only runs when asked (PITTA_LIVE=1), as it needs a running bridge.
func TestLiveHealthFooter(t *testing.T) {
	if os.Getenv("PITTA_LIVE") != "1" {
		t.Skip("set PITTA_LIVE=1 to render against the running agent")
	}
	cfg := liveConfig()
	client := api.New(cfg.BaseURL, cfg.Token)
	ctx := context.Background()
	health, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("GET %s/api/health: %v", cfg.BaseURL, err)
	}
	t.Logf("health: %+v", *health)
	if health.PID == os.Getpid() {
		t.Fatalf("pid %d is this test's, not the bridge's", health.PID)
	}
	if health.Version == "" || health.StartedAt == "" {
		t.Fatalf("the bridge did not say which bridge it is: %+v", *health)
	}
	if health.UptimeMS <= 0 {
		t.Fatalf("uptimeMs = %d", health.UptimeMS)
	}
	version := packageVersion(t)
	if health.Version != version {
		t.Fatalf("the bridge reports version %q, package.json says %q", health.Version, version)
	}
	time.Sleep(1100 * time.Millisecond)
	again, err := client.Health(ctx)
	if err != nil {
		t.Fatalf("the second read failed: %v", err)
	}
	if again.UptimeMS <= health.UptimeMS {
		t.Fatalf("uptime did not move: %d then %d", health.UptimeMS, again.UptimeMS)
	}

	a := New(cfg)
	// What init's first read would have put there, without opening a window.
	a.health = again
	i18n.Set(i18n.ZH)
	tt := footerFrame(a)
	want := uptime(again) + " · " + i18n.T("queueDepth") + " " + itoa(again.QueueDepth)
	if !tt.HasText(want) {
		t.Fatalf("the footer does not show %q; texts=%q", want, tt.Texts())
	}
	if !tt.HasText(shortPath(cfg.Home)) {
		t.Fatalf("the workspace path is gone; texts=%q", tt.Texts())
	}
	writePNG(t, "/tmp/pilot-health-live.png", tt.Image())
}

// TestHealthFooterWithoutABridge draws what the same footer says when nothing
// answers: the segment turns red instead of the client going quiet.
func TestHealthFooterWithoutABridge(t *testing.T) {
	cfg := Config{BaseURL: "http://127.0.0.1:8798", Home: "/Users/alex/project"}
	client := api.New(cfg.BaseURL, "")
	health, err := client.Health(context.Background())
	if err == nil {
		t.Fatalf("a bridge answered on %s: %+v", cfg.BaseURL, health)
	}

	a := New(cfg)
	a.health, a.healthErr = health, err
	i18n.Set(i18n.ZH)
	tt := footerFrame(a)
	if !tt.HasText(i18n.T("agentUnreachable")) {
		t.Fatalf("the footer does not say the agent is gone; texts=%q", tt.Texts())
	}
	writePNG(t, "/tmp/pilot-health-down.png", tt.Image())
}

// packageVersion is the version the bridge should be reporting: the one in the
// package.json it runs out of.
func packageVersion(t *testing.T) string {
	t.Helper()
	full, err := os.ReadFile(filepath.Join(bridge.Home(), "package.json"))
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(full, &pkg); err != nil {
		t.Fatalf("parse package.json: %v", err)
	}
	return pkg.Version
}
