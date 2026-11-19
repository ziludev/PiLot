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

// baseBridge answers the base endpoint and remembers what it was told.
type baseBridge struct {
	mu      sync.Mutex
	path    string
	body    string
	answer  string
	gitHits int
}

func newBaseBridge(t *testing.T, answer string) (*baseBridge, *httptest.Server) {
	t.Helper()
	b := &baseBridge{answer: answer}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		b.path, b.body = r.URL.Path, string(raw)
		answer := b.answer
		if r.URL.Path == "/api/git" {
			b.gitHits++
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/git/base":
			_, _ = w.Write([]byte(answer))
		case "/api/changes":
			_, _ = w.Write([]byte(`{"files":[],"patch":"","branch":"app/right-panel"}`))
		case "/api/git":
			_, _ = w.Write([]byte(`{"branch":"app/right-panel","base":"main","commits":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return b, server
}

// baseFixture is the drawer open on a worktree measured from main.
func baseFixture(t *testing.T, answer string) (*App, *baseBridge) {
	t.Helper()
	bridge, server := newBaseBridge(t, answer)
	a := changesFixture(t)
	a.active, a.changes.conv = 12, 12
	a.changes.base = "main"
	a.changes.branches = []protocol.Branch{
		{Name: "main", Current: false, Track: "origin/main"},
		{Name: "app/right-panel", Current: true},
	}
	a.api = api.New(server.URL, "")
	return a, bridge
}

func (b *baseBridge) asked() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.path, b.body
}

// until runs the inbox until cond holds.
func until(t *testing.T, a *App, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		select {
		case ev := <-a.inbox:
			switch ev.kind {
			case evBase:
				a.applyBase(ev.convID, ev.path, ev.text)
			case evChanges:
				a.applyChanges(ev.convID, ev.changes, ev.err)
			case evGit:
				a.applyGit(ev.convID, ev.git, ev.err)
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("condition never held")
}

// TestBaseChipOpensTheBranches: the footer names the base, and the list it opens
// says which branch is in use instead of offering it again.
func TestBaseChipOpensTheBranches(t *testing.T) {
	a, _ := baseFixture(t, `{"ok":true}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	label := i18n.TF("baseChip", map[string]any{"branch": "main"})
	if !tt.HasText(label) {
		t.Fatalf("the footer does not name the base; drew %v", tt.Texts())
	}
	if err := tt.Click(label); err != nil {
		t.Fatalf("could not click the chip: %v", err)
	}
	tt.Frame()
	if !a.changes.baseOpen {
		t.Fatal("the chip did not open the picker")
	}
	for _, want := range []string{"main", "app/right-panel", i18n.T("baseCurrent")} {
		if !tt.HasText(want) {
			t.Errorf("the picker does not show %q; drew %v", want, tt.Texts())
		}
	}
}

// TestChoosingABranchMovesTheBase: what a person picked is what the bridge is
// told, and everything measured from the base is re-read rather than re-counted.
func TestChoosingABranchMovesTheBase(t *testing.T) {
	a, bridge := baseFixture(t, `{"ok":true,"base":"app/right-panel"}`)
	a.changes.baseOpen = true
	a.setBase("app/right-panel")
	until(t, a, func() bool { return !a.changes.baseBusy })

	path, body := bridge.asked()
	if path != "/api/git/base" {
		t.Fatalf("the switch went to %q", path)
	}
	var sent struct {
		ConversationID int    `json:"conversationId"`
		Base           string `json:"base"`
	}
	if err := json.Unmarshal([]byte(body), &sent); err != nil {
		t.Fatalf("body %q: %v", body, err)
	}
	if sent.ConversationID != 12 {
		t.Fatalf("the switch named chat %d", sent.ConversationID)
	}
	if sent.Base != "app/right-panel" {
		t.Fatalf("the switch asked for %q", sent.Base)
	}
	if a.changes.base != "app/right-panel" {
		t.Fatalf("the base is still %q", a.changes.base)
	}
	if a.changes.baseOpen {
		t.Fatal("the picker stayed open after the switch")
	}
}

// TestRefusedBaseKeepsTheOldOne: a branch the bridge will not take leaves the
// drawer measuring from where it was, and says why.
func TestRefusedBaseKeepsTheOldOne(t *testing.T) {
	a, _ := baseFixture(t, `{"ok":false,"error":"no such branch"}`)
	a.changes.baseOpen = true
	a.setBase("nope")
	until(t, a, func() bool { return !a.changes.baseBusy && a.changes.baseErr != "" })

	if a.changes.base != "main" {
		t.Fatalf("the base moved to %q on a refusal", a.changes.base)
	}
	if !a.changes.baseOpen {
		t.Fatal("the picker closed on a refusal, hiding the reason")
	}
	if !strings.Contains(a.changes.baseErr, "no such branch") {
		t.Fatalf("the refusal reads %q", a.changes.baseErr)
	}
}

// TestBaseOfTheBranchItIsAlreadyOnIsANoOp: the current base is not sent again.
func TestBaseOfTheBranchItIsAlreadyOnIsANoOp(t *testing.T) {
	a, bridge := baseFixture(t, `{"ok":true}`)
	a.setBase("main")
	time.Sleep(80 * time.Millisecond)
	if path, _ := bridge.asked(); path == "/api/git/base" {
		t.Fatal("the base was set to what it already was")
	}
}
