package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestHealthReadsTheBridgeReport: the footer trusts this call for the uptime,
// the queue depth and the version, so every field has to survive the trip.
func TestHealthReadsTheBridgeReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Errorf("path = %q, want /api/health", r.URL.Path)
		}
		io.WriteString(w, `{"pid":39217,"startedAt":"2026-11-18T02:05:11.412Z","uptimeMs":11520000,`+
			`"version":"0.4.2","queueDepth":2,"runningTurns":1,"conversations":7}`)
	}))
	defer server.Close()

	got, err := New(server.URL, "").Health(context.Background())
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	if got.PID != 39217 || got.UptimeMS != 11520000 || got.Version != "0.4.2" {
		t.Fatalf("health = %+v, want pid 39217 / 11520000ms / 0.4.2", got)
	}
	if got.QueueDepth != 2 || got.Running != 1 || got.Chats != 7 {
		t.Fatalf("health counts = %+v, want 2 / 1 / 7", got)
	}
	// The footer computes the uptime itself, from this stamp.
	started, err := time.Parse(time.RFC3339, got.StartedAt)
	if err != nil {
		t.Fatalf("startedAt %q does not parse: %v", got.StartedAt, err)
	}
	if want := time.Date(2026, 11, 18, 2, 5, 11, 412_000_000, time.UTC); !started.Equal(want) {
		t.Fatalf("startedAt = %s, want %s", started, want)
	}
}

// TestHealthOnAnOldBridge is the case the footer's red state exists for: a
// bridge that predates /api/health answers 404, and the client must report an
// error rather than a zero-valued report it would draw as a healthy agent.
func TestHealthOnAnOldBridge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	got, err := New(server.URL, "").Health(context.Background())
	if err == nil {
		t.Fatalf("404 was read as a report: %+v", got)
	}
	var status *StatusError
	if !errors.As(err, &status) {
		t.Fatalf("error = %v, want a StatusError the caller can inspect", err)
	}
	if status.Code != http.StatusNotFound || status.Path != "/api/health" {
		t.Fatalf("error = %v, want 404 on /api/health", status)
	}
}

// TestHealthWithABrokenBody: a 200 that is not the report (a proxy page, a
// half-written response) has to fail loudly; drawing zeros would claim a
// healthy bridge of pid 0.
func TestHealthWithABrokenBody(t *testing.T) {
	for _, body := range []string{"", "<html>not json</html>", `{"pid":}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, body)
		}))
		got, err := New(server.URL, "").Health(context.Background())
		server.Close()
		if err == nil {
			t.Errorf("body %q was read as a report: %+v", body, got)
		}
	}
}

// TestHealthWithoutABridge: the red line in the footer starts as a transport
// error, so that path may not panic or hand back a report either.
func TestHealthWithoutABridge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close() // nothing listens there any more

	if got, err := New(url, "").Health(context.Background()); err == nil {
		t.Fatalf("a dead bridge was read as a report: %+v", got)
	}
}
