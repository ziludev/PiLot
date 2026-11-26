package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// quiet is a logger whose output is discarded: the tests assert on state, not on log lines.
func quiet(t *testing.T) *logging {
	t.Helper()
	discard, err := os.CreateTemp(t.TempDir(), "gw.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = discard.Close() })
	return &logging{logger: log.New(discard, "", 0)}
}

func TestDeliverRetriesUntilTheBridgeTakesIt(t *testing.T) {
	retryBackoff = []time.Duration{0, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { retryBackoff = []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second} })

	var calls atomic.Int64
	bridge := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer bridge.Close()

	state := &stats{started: time.Now()}
	forwarder := &forwarder{base: bridge.URL, out: quiet(t), state: state}
	forwarder.deliver(map[string]any{"type": "message"})

	if got := state.delivered.Load(); got != 1 {
		t.Fatalf("delivered = %d, want 1", got)
	}
	if got := state.dropped.Load(); got != 0 {
		t.Fatalf("dropped = %d, want 0", got)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("attempts = %d, want 3", got)
	}
}

// A turn that cannot be delivered must be counted and given up on: retrying forever holds a
// goroutine per message and hides the failure in a silenced loop.
func TestDeliverGivesUpAndCountsTheLoss(t *testing.T) {
	retryBackoff = []time.Duration{0, time.Millisecond}
	t.Cleanup(func() { retryBackoff = []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second} })

	bridge := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer bridge.Close()

	state := &stats{started: time.Now()}
	forwarder := &forwarder{base: bridge.URL, out: quiet(t), state: state}
	forwarder.deliver(map[string]any{"type": "message"})

	if got := state.dropped.Load(); got != 1 {
		t.Fatalf("dropped = %d, want 1", got)
	}
	if got := state.delivered.Load(); got != 0 {
		t.Fatalf("delivered = %d, want 0", got)
	}
}

func TestCardActionPassesTheBridgesAnswerThrough(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("content-type") != "application/json" {
			t.Errorf("content-type = %q", request.Header.Get("content-type"))
		}
		_, _ = writer.Write([]byte(`{"toast":{"type":"info","content":"已切换"}}`))
	}))
	defer bridge.Close()

	state := &stats{started: time.Now()}
	answer := (&forwarder{base: bridge.URL, out: quiet(t), state: state}).
		card(context.Background(), map[string]any{"type": "card"})

	if answer.Toast == nil || answer.Toast.Content != "已切换" {
		t.Fatalf("toast = %+v", answer.Toast)
	}
}

// An empty answer is a valid one: the click was handled and the card will be patched instead.
func TestCardActionTreatsAnEmptyAnswerAsHandled(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte(`{}`))
	}))
	defer bridge.Close()

	state := &stats{started: time.Now()}
	answer := (&forwarder{base: bridge.URL, out: quiet(t), state: state}).
		card(context.Background(), map[string]any{"type": "card"})

	if answer.Toast != nil || answer.Card != nil {
		t.Fatalf("expected an empty answer, got %+v", answer)
	}
}

// Feishu shows its own error at ~3s, so an unresponsive bridge has to become a toast, not a hang.
func TestCardActionTimesOutIntoAToast(t *testing.T) {
	bridge := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		time.Sleep(1500 * time.Millisecond)
	}))
	defer bridge.Close()

	state := &stats{started: time.Now()}
	forwarder := &forwarder{base: bridge.URL, out: quiet(t), state: state}
	forwarder.cardHTTP = &http.Client{Timeout: 100 * time.Millisecond}
	answer := forwarder.card(context.Background(), map[string]any{"type": "card"})

	if answer.Toast == nil {
		t.Fatal("a timeout must still answer the click")
	}
}

func TestHealthDistinguishesConnectedFromReady(t *testing.T) {
	state := &stats{started: time.Now()}
	state.connected.Store(true)
	server := newServer(config{token: "t", bridgeURL: "http://127.0.0.1:8787"}, nil, state, quiet(t))

	// Connected but nothing ever arrived: Feishu is not routing events to us yet.
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["connected"] != true || body["ready"] != false {
		t.Fatalf("healthz = %v", body)
	}

	state.lastEvent.Store(time.Now().Unix())
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	_ = json.Unmarshal(recorder.Body.Bytes(), &body)
	if body["ready"] != true {
		t.Fatalf("after an event, healthz = %v", body)
	}
}

func TestGuardedRejectsAMissingOrWrongToken(t *testing.T) {
	state := &stats{started: time.Now()}
	server := newServer(config{token: "secret"}, nil, state, quiet(t))

	for _, header := range []string{"", "wrong"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/message", nil)
		if header != "" {
			request.Header.Set("x-pilot-token", header)
		}
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("token %q gave %d, want 401", header, recorder.Code)
		}
	}
}

func TestCreateRejectsAnIncompleteBody(t *testing.T) {
	state := &stats{started: time.Now()}
	server := newServer(config{token: "secret"}, nil, state, quiet(t))

	request := httptest.NewRequest(http.MethodPost, "/v1/message", strings.NewReader(`{"chatId":"oc_x"}`))
	request.Header.Set("x-pilot-token", "secret")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

// The reaction routes are the bridge's two status marks; without an emoji name there is nothing to
// put on the message, and Feishu would answer with its own, vaguer complaint.
func TestReactionNeedsAnEmoji(t *testing.T) {
	state := &stats{started: time.Now()}
	server := newServer(config{token: "secret"}, nil, state, quiet(t))

	request := httptest.NewRequest(http.MethodPost, "/v1/message/om_1/reaction", strings.NewReader(`{}`))
	request.Header.Set("x-pilot-token", "secret")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}
