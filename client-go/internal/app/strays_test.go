package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
)

// strayBridge answers the projects drawer and the leftover-checkout half of it.
type strayBridge struct {
	worktrees string
	adopt     string
	mu        sync.Mutex
	adopted   string
}

func (b *strayBridge) adoptedDir() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.adopted
}

func newStrayBridge(t *testing.T, worktrees, adopt string) (*strayBridge, *httptest.Server) {
	t.Helper()
	b := &strayBridge{worktrees: worktrees, adopt: adopt}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects":
			_, _ = w.Write([]byte(`{"projects":[{"id":"p1","name":"dora-service","path":"/Users/alex/code/dora-service"}]}`))
		case "/api/worktrees":
			_, _ = w.Write([]byte(b.worktrees))
		case "/api/worktrees/adopt":
			b.mu.Lock()
			b.adopted = string(raw)
			b.mu.Unlock()
			_, _ = w.Write([]byte(b.adopt))
		case "/api/changes":
			_, _ = w.Write([]byte(`{"files":[]}`))
		case "/api/git":
			_, _ = w.Write([]byte(`{"branch":"main","files":[]}`))
		case "/api/state":
			_, _ = w.Write([]byte(`{"conversations":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return b, server
}

const strayList = `{"worktrees":[
	{"dir":"/Users/alex/code/dora-service/.worktrees/conv-41","conversationId":41,"branch":"agent/conv-41","ahead":3,"behind":1,"dirty":false,"merged":false,"occupied":true},
	{"dir":"/Users/alex/code/dora-service/.worktrees/conv-7","branch":"agent/conv-7","ahead":0,"behind":0,"dirty":true,"merged":false,"occupied":false},
	{"dir":"/Users/alex/code/dora-service/.worktrees/conv-9","conversationId":9,"branch":"agent/conv-9","ahead":0,"behind":2,"dirty":false,"merged":true,"occupied":false}]}`

// straysFixture is the projects drawer open on a project with three leftovers.
func straysFixture(t *testing.T, adopt string) (*App, *strayBridge) {
	t.Helper()
	bridge, server := newStrayBridge(t, strayList, adopt)
	a := changesFixture(t)
	a.api = api.New(server.URL, "")
	a.projects.open = true
	a.loadProjects()
	a.loadStrays()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.projects.loaded && !a.projects.straysBusy {
			break
		}
		select {
		case ev := <-a.inbox:
			switch ev.kind {
			case evProjects:
				a.applyProjects(ev.seq, ev.projects, ev.err)
			case evStrays:
				a.applyStrays(ev.seq, ev.text, ev.worktrees, ev.err)
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	return a, bridge
}

// TestStraysSectionShowsWhatEachCheckoutHolds: the drawer names the branch, says
// whose chat it was, and marks merged and dirty work.
func TestStraysSectionShowsWhatEachCheckoutHolds(t *testing.T) {
	a, _ := straysFixture(t, "")
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{
		"遗留工作区",
		"agent/conv-41", "agent/conv-7", "agent/conv-9",
		"会话 41", "会话已不在", "已合入", "有未提交改动",
		"+3", "−1",
	} {
		if !tt.HasText(want) {
			t.Errorf("the section does not show %q; drew %v", want, tt.Texts())
		}
	}
}

// TestAdoptSendsTheDirectory: opening a leftover asks for that directory, and the
// new chat is then selected like any other.
func TestAdoptSendsTheDirectory(t *testing.T) {
	a, bridge := straysFixture(t, `{"conversationId":188,"surfaceId":"s188","dir":"/Users/alex/code/dora-service/.worktrees/conv-41","branch":"agent/conv-41"}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("打开"); err != nil {
		t.Fatalf("the adopt button is not clickable: %v", err)
	}
	for i := 0; i < 200 && bridge.adoptedDir() == ""; i++ {
		select {
		case ev := <-a.inbox:
			if ev.kind == evAdopt {
				a.applyAdopt(ev.path, ev.convID, ev.err)
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	body := bridge.adoptedDir()
	if !strings.Contains(body, "/.worktrees/conv-41") {
		t.Fatalf("the adopt button sent %q; asks %v", body, tt.Texts())
	}
}

// TestSnapshotProjects draws the drawer with its leftovers under the projects.
func TestSnapshotProjects(t *testing.T) {
	a, _ := straysFixture(t, "")
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("遗留工作区") || !tt.HasText("agent/conv-41") {
		t.Fatalf("the leftovers are missing; drew %v", tt.Texts())
	}
	writePNG(t, "/tmp/clientv2-projects.png", tt.Image())
}
