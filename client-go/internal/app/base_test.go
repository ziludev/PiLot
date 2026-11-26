package app

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
)

// baseBridge answers git and changes, and counts whether anything was told to
// move the base. There is no reason left for the window to send that request.
type baseBridge struct {
	mu      sync.Mutex
	setHits int
	paths   []string
}

func newBaseBridge(t *testing.T) (*baseBridge, *httptest.Server) {
	t.Helper()
	b := &baseBridge{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		b.paths = append(b.paths, r.URL.Path)
		if r.URL.Path == "/api/git/base" {
			b.setHits++
		}
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
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
func baseFixture(t *testing.T) *App {
	t.Helper()
	bridge, server := newBaseBridge(t)
	a := changesFixture(t)
	a.active, a.changes.conv = 12, 12
	a.changes.base = "main"
	a.api = api.New(server.URL, "")
	_ = bridge
	return a
}

func (b *baseBridge) baseWrites() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.setHits
}

// TestBaseIsNamedButNotOffered: the footer says what the numbers are measured
// from. Choosing it is a git question — the drawer answers it, it does not ask.
func TestBaseIsNamedButNotOffered(t *testing.T) {
	a := baseFixture(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	label := i18n.TF("baseChip", map[string]any{"branch": "main"})
	if !tt.HasText(label) {
		t.Fatalf("the footer does not name the base; drew %v", tt.Texts())
	}
	// Nothing on the chip says it can be opened: no chevron, no branch list.
	for _, gone := range []string{i18n.T("baseTitle"), i18n.T("baseCurrent"), "▾"} {
		if tt.HasText(gone) {
			t.Errorf("the drawer still draws %q", gone)
		}
	}
	// And clicking it is a no-op, not a quiet re-read of everything.
	_ = tt.Click(label)
	tt.Frame()
	if a.changes.base != "main" {
		t.Fatalf("clicking the chip moved the base to %q", a.changes.base)
	}
	select {
	case ev := <-a.inbox:
		if ev.kind == evChanges || ev.kind == evGit {
			t.Fatalf("clicking the chip asked the bridge for %v", ev.kind)
		}
	case <-time.After(60 * time.Millisecond):
	}
}
