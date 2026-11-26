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

	"pilot/internal/api"
	"pilot/internal/i18n"
	"pilot/internal/protocol"
)

// projectBridge answers the projects endpoints and remembers what it was asked.
type projectBridge struct {
	mu   sync.Mutex
	path string
	body string
	// calls is every request, because starting a chat is followed by a state
	// reload that would otherwise be the last thing a test sees.
	calls   []string
	list    string
	addCode int
	add     string
	remove  string
}

func (b *projectBridge) asked() (string, string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.path, b.body
}

func newProjectBridge(t *testing.T, list, add string, addCode int, remove string) (*projectBridge, *httptest.Server) {
	t.Helper()
	b := &projectBridge{list: list, add: add, addCode: addCode, remove: remove}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		b.mu.Lock()
		b.path, b.body = r.URL.Path, string(raw)
		b.calls = append(b.calls, r.Method+" "+r.URL.Path+" "+string(raw))
		code, add, remove, list := b.addCode, b.add, b.remove, b.list
		b.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/projects":
			if r.Method == http.MethodPost {
				w.WriteHeader(code)
				_, _ = w.Write([]byte(add))
				return
			}
			_, _ = w.Write([]byte(list))
		case "/api/projects/remove":
			_, _ = w.Write([]byte(remove))
		case "/api/conversations":
			_, _ = w.Write([]byte(`{"conversationId":77,"surfaceId":"s77"}`))
		case "/api/state":
			_, _ = w.Write([]byte(`{"conversations":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return b, server
}

// projectsFixture is the drawer open on a bridge with two repositories and the
// built-in sandbox.
func projectsFixture(t *testing.T) (*App, *projectBridge) {
	t.Helper()
	bridge, server := newProjectBridge(t, `{"projects":[
		{"id":"builtin","name":"workspace","path":"/Users/alex/workspace","builtin":true},
		{"id":"p1","name":"dora-service","path":"/Users/alex/code/dora-service"}]}`,
		`{"status":"added"}`, http.StatusOK, `{"status":"removed","movedChats":0}`)
	a := changesFixture(t)
	a.api = api.New(server.URL, "")
	a.projects.open = true
	a.loadProjects()
	settle(t, a, func() bool { return a.projects.loaded })
	return a, bridge
}

// settle runs the inbox until cond holds, applying the events a frame loop
// would have applied.
func settle(t *testing.T, a *App, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		select {
		case ev := <-a.inbox:
			switch ev.kind {
			case evProjects:
				a.applyProjects(ev.seq, ev.projects, ev.err)
			case evProjectAdd:
				a.applyProjectAdd(ev.path, ev.text, ev.writeBad)
			case evProjectRemove:
				a.applyProjectRemove(ev.text, ev.writeBad)
			case evWrite:
				a.applyWrite(ev.convID, ev.writeKind, ev.text, ev.writeBad)
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("condition never held")
}

// TestProjectsDrawerListsTheRepositories: the drawer shows what the bridge
// knows, and the built-in sandbox says that it is built in.
func TestProjectsDrawerListsTheRepositories(t *testing.T) {
	a, _ := projectsFixture(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{"workspace", "dora-service", i18n.T("projectBuiltin")} {
		if !tt.HasText(want) {
			t.Errorf("the drawer does not show %q; drew %v", want, tt.Texts())
		}
	}
	if !tt.HasText("/Users/alex/code/dora-service") {
		t.Errorf("the path is not under the name; drew %v", tt.Texts())
	}
}

// TestAddingAPathKeepsTheRefusal: the bridge answers a folder that is not a
// repo with a 400 and the reason in the body; the drawer has to show the reason,
// not the status code.
func TestAddingAPathKeepsTheRefusal(t *testing.T) {
	a, bridge := projectsFixture(t)
	bridge.mu.Lock()
	bridge.addCode = http.StatusBadRequest
	bridge.add = `{"status":"not-a-repo","path":"/Users/alex/nope"}`
	bridge.mu.Unlock()
	a.projects.path = "/Users/alex/nope"
	a.addProject()
	settle(t, a, func() bool { return !a.projects.adding && a.projects.result != "" })

	if !a.projects.bad {
		t.Fatalf("a refusal came back as success: %q", a.projects.result)
	}
	if !strings.Contains(a.projects.result, "/Users/alex/nope") {
		t.Fatalf("the refusal does not say which path: %q", a.projects.result)
	}
	if a.projects.path != "/Users/alex/nope" {
		t.Fatal("the refused path was cleared from the field; it has to stay so it can be fixed")
	}
	if _, body := bridge.asked(); !strings.Contains(body, "/Users/alex/nope") {
		t.Fatalf("the path went as %q", body)
	}
}

// TestAddedProjectClearsTheFieldAndReloads: a path that worked leaves the field
// empty and the list re-read, not patched from the reply.
func TestAddedProjectClearsTheFieldAndReloads(t *testing.T) {
	a, bridge := projectsFixture(t)
	bridge.mu.Lock()
	bridge.add = `{"status":"added","project":{"id":"p2","name":"flicker","path":"/Users/alex/code/flicker"}}`
	bridge.list = `{"projects":[{"id":"p2","name":"flicker","path":"/Users/alex/code/flicker"}]}`
	bridge.mu.Unlock()
	a.projects.path = "/Users/alex/code/flicker"
	a.addProject()
	settle(t, a, func() bool { return !a.projects.adding && len(a.projects.items) == 1 })

	if a.projects.bad {
		t.Fatalf("adding was reported as a failure: %q", a.projects.result)
	}
	if a.projects.path != "" {
		t.Fatalf("the field still holds %q", a.projects.path)
	}
	if a.projects.items[0].Name != "flicker" {
		t.Fatalf("the list was not re-read: %+v", a.projects.items)
	}
}

// TestForgettingSaysWhereTheChatsWent: removing a project moves its chats to the
// built-in sandbox, and that is the part a person needs to hear.
func TestForgettingSaysWhereTheChatsWent(t *testing.T) {
	a, bridge := projectsFixture(t)
	bridge.mu.Lock()
	bridge.remove = `{"status":"removed","movedChats":2}`
	bridge.mu.Unlock()
	a.removeProject(protocol.Project{ID: "p1", Name: "dora-service"})
	settle(t, a, func() bool { return a.projects.result != "" })

	if a.projects.bad {
		t.Fatalf("removing was reported as a failure: %q", a.projects.result)
	}
	for _, want := range []string{"dora-service", "2"} {
		if !strings.Contains(a.projects.result, want) {
			t.Errorf("%q does not mention %q", a.projects.result, want)
		}
	}
}

// TestBuiltinCannotBeForgotten: the built-in sandbox has no × and asking anyway
// does not reach the bridge.
func TestBuiltinCannotBeForgotten(t *testing.T) {
	a, bridge := projectsFixture(t)
	a.removeProject(protocol.Project{ID: "builtin", Name: "workspace", Builtin: true})
	time.Sleep(60 * time.Millisecond)
	if path, _ := bridge.asked(); path == "/api/projects/remove" {
		t.Fatal("the built-in sandbox was asked to be removed")
	}
}

// TestClickingAProjectOpensAChatThere: the drawer's reason to exist — a chat in
// that repository's worktree, not in the built-in one.
func TestClickingAProjectOpensAChatThere(t *testing.T) {
	a, bridge := projectsFixture(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("dora-service"); err != nil {
		t.Fatalf("could not click the project: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		bridge.mu.Lock()
		calls := append([]string(nil), bridge.calls...)
		bridge.mu.Unlock()
		for _, call := range calls {
			if !strings.HasPrefix(call, "POST /api/conversations ") {
				continue
			}
			var sent struct {
				ProjectID string `json:"projectId"`
			}
			if err := json.Unmarshal([]byte(strings.TrimPrefix(call, "POST /api/conversations ")), &sent); err != nil {
				t.Fatalf("call %q: %v", call, err)
			}
			if sent.ProjectID != "p1" {
				t.Fatalf("the chat was started in project %q", sent.ProjectID)
			}
			if a.projects.open {
				t.Fatal("the drawer stayed open over the new chat")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no chat was ever started")
}
