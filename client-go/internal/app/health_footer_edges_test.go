package app

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/protocol"
	"pitta/internal/theme"
	"pitta/internal/turns"
)

// footerFrame draws only the rail's last line, which is where the bridge's
// state lives.
func chipFooterFrame(a *App) *ui.Tester {
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Light())
		ui.Column(c).Width(320).Children(func() { a.railFooter(c, theme.Light()) })
	}, 340, 60)
	tt.Frame()
	return tt
}

// healthAt builds a report for a bridge that started the given span ago.
func healthAt(start time.Duration) *protocol.Health {
	return &protocol.Health{PID: 39217, Version: "0.4.2", QueueDepth: 2,
		StartedAt: time.Now().Add(-start).UTC().Format(time.RFC3339)}
}

// TestUptimeUnderAnHourUsesTheClock: the number is computed per frame from the
// bridge's start time, so a window left open overnight keeps counting and a
// restarted bridge drops back to zero. The stamp only carries whole seconds, so
// the reading is the span plus up to a second.
func TestUptimeUnderAnHourUsesTheClock(t *testing.T) {
	for _, ago := range []time.Duration{30 * time.Second, 90 * time.Second, 59 * time.Minute} {
		got := uptime(healthAt(ago))
		if want := turns.Clock(ago.Milliseconds()); got != want && got != turns.Clock(ago.Milliseconds()+999) {
			t.Fatalf("uptime after %s = %q, want %q", ago, got, want)
		}
	}
}

// TestUptimeOverAnHourCountsHours: past an hour the clock format grows a unit
// that never rolls over to "60m".
func TestUptimeOverAnHourCountsHours(t *testing.T) {
	if got := uptime(healthAt(3*time.Hour + 12*time.Minute)); got != "3h12m" {
		t.Fatalf("uptime = %q, want 3h12m", got)
	}
}

// TestUptimeFallsBackToTheReportedSpan: a stamp the client cannot parse (an
// older bridge, a hand-rolled response) still has to yield a number.
func TestUptimeFallsBackToTheReportedSpan(t *testing.T) {
	health := &protocol.Health{PID: 1, UptimeMS: 5000, StartedAt: "not a time"}
	if got := uptime(health); got != turns.Clock(5000) {
		t.Fatalf("uptime = %q, want %q", got, turns.Clock(5000))
	}
}

// TestHealthChipIsQuietUntilSomethingIsKnown: before the first read there is
// nothing to say, and a dot about nothing is noise.
func TestHealthChipIsQuietUntilSomethingIsKnown(t *testing.T) {
	a := snapshotData(t)
	tt := chipFooterFrame(a)
	if tt.HasText("●") {
		t.Fatalf("footer draws a dot before any read: %q", tt.Texts())
	}
	if tt.HasText(i18n.T("agentUnreachable")) {
		t.Fatalf("footer claims the agent is down before any read: %q", tt.Texts())
	}
}

// TestHealthChipShowsUptimeAndQueue: what the footer promises — how long the
// bridge has been up and how much work is waiting, beside the workspace path.
func TestHealthChipShowsUptimeAndQueue(t *testing.T) {
	a := snapshotData(t)
	i18n.Set(i18n.ZH)
	a.health = healthAt(3*time.Hour + 12*time.Minute)
	a.health.QueueDepth = 2

	tt := chipFooterFrame(a)
	want := "3h12m · " + i18n.T("queueDepth") + " 2"
	if !tt.HasText(want) {
		t.Fatalf("footer does not show %q: %q", want, tt.Texts())
	}
	if !tt.HasText("●") {
		t.Fatalf("the healthy dot is missing: %q", tt.Texts())
	}
}

// TestHealthChipTurnsRedOnAFailedRead: a bridge that stopped answering is the
// one case the footer has to shout about, and it must say so in words, since a
// colour alone is not something everyone reads.
func TestHealthChipTurnsRedOnAFailedRead(t *testing.T) {
	a := snapshotData(t)
	i18n.Set(i18n.ZH)
	a.health = nil
	a.healthErr = errors.New("dial tcp 127.0.0.1:8787: connect: connection refused")

	tt := chipFooterFrame(a)
	if !tt.HasText(i18n.T("agentUnreachable")) {
		t.Fatalf("footer does not say the agent is unreachable: %q", tt.Texts())
	}
	if tt.HasText("· " + i18n.T("queueDepth")) {
		t.Fatalf("footer still shows queue numbers from a dead bridge: %q", tt.Texts())
	}
}

// TestHealthUnsupportedIsNotUnreachable: the bridge and the app ship separately, so
// a new window can outrun the agent it talks to. A 404 means "no report yet"; the red
// footer is reserved for a bridge that is actually not answering.
func TestHealthUnsupportedIsNotUnreachable(t *testing.T) {
	missing := &api.StatusError{Method: "GET", Path: "/api/health", Code: http.StatusNotFound, Status: "Not Found"}
	if !healthUnsupported(missing) {
		t.Fatal("404 from an older bridge should read as unsupported")
	}
	if healthUnsupported(nil) || healthUnsupported(errors.New("dial tcp: connection refused")) {
		t.Fatal("a real failure must stay unreachable")
	}
}
