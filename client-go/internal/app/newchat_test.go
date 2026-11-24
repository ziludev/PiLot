package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
	"pitta/internal/i18n"
)

// newChatBridge records what the dialog asked the bridge to create, because the
// branch name is the whole point: it must reach the wire, not just the panel.
type newChatBridge struct {
	mu      sync.Mutex
	project string
	branch  string
	asked   int
}

func newChatFixture(t *testing.T) (*App, *newChatBridge) {
	t.Helper()
	b := &newChatBridge{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/conversations":
			var body struct {
				ProjectID string `json:"projectId"`
				Branch    string `json:"branch"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			b.mu.Lock()
			b.project, b.branch = body.ProjectID, body.Branch
			b.asked++
			b.mu.Unlock()
			_, _ = w.Write([]byte(`{"conversationId":77,"surfaceId":"client:new"}`))
		case "/api/projects":
			_, _ = w.Write([]byte(`{"projects":[{"id":"workspace","name":"workspace","path":"/tmp/ws"},` +
				`{"id":"dora","name":"dora-service","path":"/tmp/dora"}]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	a := fixtureApp(t, false)
	a.api = api.New(server.URL, "")
	return a, b
}

func createdBy(b *newChatBridge) (string, string, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.project, b.branch, b.asked
}

// TestNewChatDialogCarriesTheBranch: the dialog is the only place a person names the
// branch at creation, and an empty name must stay empty (the bridge derives it).
func TestNewChatDialogCarriesTheBranch(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, bridge := newChatFixture(t)
	a.openNewChat()
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	a.drain()
	tt.Frame()

	for _, want := range []string{"新会话", "项目", "内置沙箱", "dora-service", "分支名（可空）", "创建"} {
		if !tt.HasText(want) {
			t.Fatalf("the dialog does not show %q; drew %v", want, tt.Texts())
		}
	}
	if err := tt.Click("dora-service"); err != nil {
		t.Fatalf("no project row to click: %v", err)
	}
	tt.Frame()
	a.newChatPanel.branch = "feat/sky-blue"
	tt.Frame()
	if err := tt.Click("创建"); err != nil {
		t.Fatalf("no create button: %v", err)
	}
	tt.Frame()
	a.drain()
	tt.Frame()

	project, branch, asked := createdBy(bridge)
	if asked != 1 {
		t.Fatalf("created %d chats, wanted one", asked)
	}
	if project != "dora" || branch != "feat/sky-blue" {
		t.Fatalf("asked for %q on %q, wanted dora on feat/sky-blue", project, branch)
	}
	if a.newChatPanel.open {
		t.Fatal("the dialog stayed open after creating")
	}
}

// TestNewChatNamesTheBranchOnAPrebuiltChat: an empty name is still sent as empty, so
// the bridge keeps deriving agent/conv-N and nothing silently changes for old chats.
func TestNewChatEmptyBranchIsStillSent(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, bridge := newChatFixture(t)
	a.openNewChat()
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	a.drain()
	tt.Frame()
	if err := tt.Click("创建"); err != nil {
		t.Fatalf("no create button: %v", err)
	}
	tt.Frame()
	a.drain()
	tt.Frame()
	project, branch, asked := createdBy(bridge)
	if asked != 1 || project != "" || branch != "" {
		t.Fatalf("asked %d times for project %q branch %q, wanted one ask with neither", asked, project, branch)
	}
}

func TestSnapshotNewChat(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, _ := newChatFixture(t)
	a.openNewChat()
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	a.drain()
	tt.Frame()
	a.newChatPanel.branch = "feat/device-context"
	tt.Frame()
	if !tt.HasText("创建") {
		t.Fatalf("the dialog did not draw; drew %v", tt.Texts())
	}
	writePNG(t, "/tmp/clientv2-newchat.png", tt.Image())
}
