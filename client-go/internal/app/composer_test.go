package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// recorder collects what a fake bridge was sent, safe for the server's
// goroutine and the test's.
type recorder struct {
	mu       sync.Mutex
	requests []api.SubmitRequest
}

func (r *recorder) add(request api.SubmitRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests = append(r.requests, request)
}

func (r *recorder) snapshot() []api.SubmitRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]api.SubmitRequest, len(r.requests))
	copy(out, r.requests)
	return out
}

// appWithBridge gives a test app a bridge that records submissions.
func appWithBridge(t *testing.T, r *recorder) *App {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/api/submit" {
			raw, _ := io.ReadAll(req.Body)
			var request api.SubmitRequest
			_ = json.Unmarshal(raw, &request)
			r.add(request)
		}
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)
	a := newApp(Config{BaseURL: server.URL})
	a.api = api.New(server.URL, "")
	a.active = 5
	a.setSurfaces([]protocol.Surface{{ConversationID: 5, Kind: "client", Title: "sky"}})
	a.now = time.Now()
	return a
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition never held")
}

func TestEnterSendsAndShiftEnterDoesNot(t *testing.T) {
	i18n.Set(i18n.EN)
	var log recorder
	a := appWithBridge(t, &log)

	a.draft = "why is the sky blue"
	if !a.composerKey(ui.TextKey{Key: ui.KeyEnter}) {
		t.Fatal("Enter was not handled")
	}
	if a.draft != "" {
		t.Fatalf("draft survived the send: %q", a.draft)
	}
	waitFor(t, func() bool { return len(log.snapshot()) == 1 })
	sent := log.snapshot()
	if sent[0].ConversationID != 5 || sent[0].Text != "why is the sky blue" {
		t.Fatalf("request %+v", sent[0])
	}
	if len(a.history) != 1 || a.history[0] != "why is the sky blue" {
		t.Fatalf("history %v", a.history)
	}

	a.draft = "a second line"
	if a.composerKey(ui.TextKey{Key: ui.KeyEnter, Mods: ui.Shift}) {
		t.Fatal("Shift+Enter is the text area's newline, not a send")
	}
	if a.draft != "a second line" {
		t.Fatalf("Shift+Enter ate the draft: %q", a.draft)
	}
	if got := len(log.snapshot()); got != 1 {
		t.Fatalf("Shift+Enter sent %d requests", got)
	}
}

func TestEmptyEnterSendsNothing(t *testing.T) {
	var log recorder
	a := appWithBridge(t, &log)
	a.draft = "   "
	a.composerKey(ui.TextKey{Key: ui.KeyEnter})
	time.Sleep(50 * time.Millisecond)
	if sent := log.snapshot(); len(sent) != 0 {
		t.Fatalf("blank draft sent %+v", sent)
	}
}

func TestHistoryWalk(t *testing.T) {
	var log recorder
	a := appWithBridge(t, &log)
	a.history = []string{"first", "second"}
	a.histIndex = len(a.history)

	if !a.composerKey(ui.TextKey{Key: ui.KeyUp}) || a.draft != "second" {
		t.Fatalf("first Up gave %q", a.draft)
	}
	if !a.composerKey(ui.TextKey{Key: ui.KeyUp}) || a.draft != "first" {
		t.Fatalf("second Up gave %q", a.draft)
	}
	if !a.composerKey(ui.TextKey{Key: ui.KeyDown}) || a.draft != "second" {
		t.Fatalf("Down gave %q", a.draft)
	}
	if a.composerKey(ui.TextKey{Key: ui.KeyDown, Mods: ui.Shift}) {
		t.Fatal("Shift+Down belongs to the text area")
	}
}

// TestApprovalCardReplacesComposer: a pending approval takes the composer's
// whole slot, and answering it goes out with the call id alone.
func TestApprovalCardReplacesComposer(t *testing.T) {
	i18n.Set(i18n.EN)
	var decision api.ApprovalDecision
	var decided bool
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/approvals" && r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			_ = json.Unmarshal(raw, &decision)
			decided = true
			mu.Unlock()
		}
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)

	a := newApp(Config{BaseURL: server.URL})
	a.api = api.New(server.URL, "")
	a.setSurfaces([]protocol.Surface{{ConversationID: 5, Kind: "client", Title: "sky"}})
	a.active = 5
	a.pending = []protocol.PendingApproval{{
		CallID: "call-1", Tool: "bash", Summary: "rm -rf ./build", Reason: "destructive command",
	}}
	a.draft = "half-written message"

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{
		"Approval needed: bash", "rm -rf ./build", "Allow", "Deny",
	} {
		if !tt.HasText(want) {
			t.Fatalf("approval card is missing %q; drew %v", want, tt.Texts())
		}
	}
	if tt.HasText("Send") {
		t.Fatal("the composer stayed on screen under an approval")
	}

	a.decide("call-1", true)
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return decided })
	mu.Lock()
	defer mu.Unlock()
	if decision.CallID != "call-1" || !decision.Approved {
		t.Fatalf("decision %+v", decision)
	}
}

// TestComposerPlaceholderVersusHint: the Enter rule is written once. An empty
// box carries it in the placeholder; once there is a draft the placeholder is
// gone and the hint under the box takes over.
func TestComposerPlaceholderVersusHint(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	a.draft = ""
	a.draftImage = nil
	if cue := a.composerCue(); cue != "" {
		t.Fatalf("an empty composer draws a cue on top of its placeholder: %q", cue)
	}
	a.draft = "a draft"
	if cue := a.composerCue(); !strings.Contains(cue, "Enter to send") {
		t.Fatalf("a draft has no Enter hint: %q", cue)
	}
}

func TestRunningFromLiveDoc(t *testing.T) {
	a := newApp(Config{})
	a.active = 3
	th := a.thread(3)
	if a.running() {
		t.Fatal("a chat with no live doc is running?")
	}
	th.live = protocol.LiveDoc{Run: []byte(`null`)}
	if a.running() {
		t.Fatal("a null run is not a running turn")
	}
	th.live = protocol.LiveDoc{Run: []byte(`{"startedAt":1}`)}
	if !a.running() {
		t.Fatal("a run document means a turn is in flight")
	}
}

// TestEventReachesTheNextFrame: background work changes the window through the
// inbox, and posting an event must make a frame happen that applies it.
func TestEventReachesTheNextFrame(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	a.post(event{kind: evNotice, notice: "bridge said no"})
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("bridge said no") {
		t.Fatalf("the event never reached a frame; drew %v", tt.Texts())
	}
}

// TestStepsExpand: a folded run opens on click, and its steps keep their
// summary line above them.
func TestStepsExpand(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	rows := a.threadRows(11)
	var steps threadRow
	for _, row := range rows {
		if row.kind == rowSteps {
			steps = row
			break
		}
	}
	if steps.id == "" {
		t.Fatal("fixture has no step row")
	}
	if !strings.HasPrefix(steps.id, foldKey(11, 41)) {
		t.Fatalf("fold key %q is not (conversation, turn)", steps.id)
	}
	a.vm.folds[steps.id] = true
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("bash") {
		t.Fatalf("expanded steps drew no tool line; drew %v", tt.Texts())
	}
	if !hasAnyText(tt, "1 step") {
		t.Fatalf("expanded steps lost their summary; drew %v", tt.Texts())
	}
}

// TestApprovalCardOffersSessionGrant: when the bridge names a risk class, the
// card offers "allow in this chat", and that answer goes out with session set.
// A call with no grant key has no class to remember, so the extra button is
// absent rather than a no-op.
func TestApprovalCardOffersSessionGrant(t *testing.T) {
	i18n.Set(i18n.EN)
	var decision api.ApprovalDecision
	var decided bool
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/approvals" && r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			mu.Lock()
			_ = json.Unmarshal(raw, &decision)
			decided = true
			mu.Unlock()
		}
		io.WriteString(w, `{}`)
	}))
	t.Cleanup(server.Close)

	a := newApp(Config{BaseURL: server.URL})
	a.api = api.New(server.URL, "")
	a.setSurfaces([]protocol.Surface{{ConversationID: 5, Kind: "client", Title: "sky"}})
	a.active = 5
	a.pending = []protocol.PendingApproval{{
		CallID: "call-1", Tool: "bash", Summary: "rm -rf ./build", GrantKey: "bash:rm",
	}}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("Allow in this chat") {
		t.Fatalf("a grantable approval drew no session answer; drew %v", tt.Texts())
	}
	a.decideSession("call-1")
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return decided })
	mu.Lock()
	defer mu.Unlock()
	if decision.CallID != "call-1" || !decision.Approved || !decision.Session {
		t.Fatalf("decision %+v", decision)
	}

	a.pending = []protocol.PendingApproval{{CallID: "call-2", Tool: "bash", Summary: "ls"}}
	tt2 := ui.NewTester(a.View, 1180, 820)
	tt2.Frame()
	if tt2.HasText("Allow in this chat") {
		t.Fatal("an approval without a grant key offered a session grant")
	}
}

// TestComposerShowsChatSpend: the chat's running total sits beside the model
// that is spending it, and a chat that has not answered yet shows nothing
// rather than a row of zeroes.
func TestComposerShowsChatSpend(t *testing.T) {
	i18n.Set(i18n.EN)
	a := fixtureApp(t, false)
	a.active = 5
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if tt.HasText("tok") {
		t.Fatal("a chat with no turns drew a spend")
	}
	a.thread(5).usage = &protocol.Usage{Cost: 0.0423, Tokens: 12400, Turns: 3}
	tt2 := ui.NewTester(a.View, 1180, 820)
	tt2.Frame()
	if !tt2.HasText("$0.042 · 12.4K tok") {
		t.Fatalf("spend missing; drew %v", tt2.Texts())
	}
}
