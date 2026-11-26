package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pilot/internal/api"
	"pilot/internal/i18n"
	"pilot/internal/protocol"
)

// branchBridge answers the branch list and records the base it was told to move
// to, so a click can be checked at the wire rather than in the panel's own state.
type branchBridge struct {
	mu      sync.Mutex
	bases   []string
	names   []string
	renames []string
	// flows is every workflow write's body, so a test can read what the field
	// sent rather than what the panel decided.
	flows []map[string]any
}

func newBranchBridge(t *testing.T) (*branchBridge, *httptest.Server) {
	t.Helper()
	b := &branchBridge{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/workflow":
			if r.Method == http.MethodPost {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				b.mu.Lock()
				b.flows = append(b.flows, body)
				b.mu.Unlock()
				out, _ := json.Marshal(map[string]any{"projectId": body["projectId"], "workflow": map[string]any{
					"testBase": "release/test", "releaseTarget": "feature/server", "roundLimit": 3,
				}})
				_, _ = w.Write(out)
				return
			}
			_, _ = w.Write([]byte(`{"workflow":{"testBase":"release/test","releaseTarget":"feature/server","roundLimit":3}}`))
		case "/api/branches":
			_, _ = w.Write([]byte(`{"projectId":"dora","base":"main","current":"agent/conv-12","branches":[` +
				`{"name":"main","base":true,"ahead":0,"behind":0},` +
				`{"name":"agent/conv-12","current":true,"ahead":3,"behind":1},` +
				`{"name":"release/1.0","ahead":7,"behind":0,"owner":{"conversationId":1046,"dir":"/tmp/wt"}}` +
				`]}`))
		case "/api/git/base":
			var body struct {
				Base string `json:"base"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			b.mu.Lock()
			b.bases = append(b.bases, body.Base)
			b.mu.Unlock()
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/api/branch":
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			b.mu.Lock()
			b.names = append(b.names, body.Name)
			b.mu.Unlock()
			_, _ = w.Write([]byte(`{"status":"created","name":"` + body.Name + `"}`))
		case "/api/branch-name":
			var body struct {
				Branch string `json:"branch"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			b.mu.Lock()
			b.renames = append(b.renames, body.Branch)
			b.mu.Unlock()
			_, _ = w.Write([]byte(`{"status":"renamed","branch":"` + body.Branch + `"}`))
		case "/api/changes":
			_, _ = w.Write([]byte(`{"files":[],"patch":"","branch":"agent/conv-12"}`))
		case "/api/git":
			_, _ = w.Write([]byte(`{"branch":"agent/conv-12","base":"main","commits":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return b, server
}

func branchFixture(t *testing.T) (*App, *branchBridge) {
	t.Helper()
	bridge, server := newBranchBridge(t)
	a := changesFixture(t)
	a.active, a.changes.conv = 12, 12
	a.changes.workspace = "pi-durable-agent"
	a.api = api.New(server.URL, "")
	return a, bridge
}

func named(b *branchBridge) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.names...)
}

func renamesSent(b *branchBridge) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.renames...)
}

func basesSent(b *branchBridge) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.bases...)
}

// TestBranchNameNamesTheChat: the drawer can name the chat's own branch, which is
// how one requirement is followed across projects.
func TestBranchNameNamesTheChat(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, bridge := branchFixture(t)
	a.setChangesTab(tabBranches)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	a.drain()
	tt.Frame()
	if r := renamesSent(bridge); r != nil {
		t.Fatalf("opening the tab named a branch: %v", r)
	}
	if err := tt.Click("命名此会话的分支"); err != nil {
		t.Fatalf("no name button to click: %v", err)
	}
	tt.Frame()
	// The field starts at the branch the checkout is on, so a small edit is enough.
	if a.changes.branchNameDraft != "agent/conv-12" {
		t.Fatalf("the field starts at %q, wanted the current branch", a.changes.branchNameDraft)
	}
	a.changes.branchNameDraft = "feat/sky-blue"
	tt.Frame()
	if err := tt.Click("确定"); err != nil {
		t.Fatalf("no apply button: %v", err)
	}
	// The write is a goroutine against an HTTP stub, so the frame that first sees it
	// is a few frames later: spin rather than assume one drain is enough.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		a.drain()
		tt.Frame()
		if got := renamesSent(bridge); len(got) == 1 && got[0] == "feat/sky-blue" && tt.HasText("分支已改名") {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := renamesSent(bridge); len(got) != 1 || got[0] != "feat/sky-blue" {
		t.Fatalf("posted %v, wanted [feat/sky-blue]", got)
	}
	if !tt.HasText("分支已改名") {
		t.Fatalf("the footer does not say the branch was renamed; drew %v", tt.Texts())
	}
	if a.changes.branchRenaming {
		t.Fatal("the field is still open after applying")
	}
}

// TestBranchTabListsTheRefsAndMovesTheBase: the second tab reads the checkout and
// its "use as base" button is the one write it offers.
func TestBranchTabListsTheRefsAndMovesTheBase(t *testing.T) {
	a, bridge := branchFixture(t)
	a.setChangesTab(tabBranches)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	a.drain()
	tt.Frame()

	for _, want := range []string{"分支", "main", "agent/conv-12", "release/1.0", "当前", "设为基准"} {
		if !tt.HasText(want) {
			t.Fatalf("the branch tab does not show %q; drew %v", want, tt.Texts())
		}
	}
	// The base branch and the checked-out one both refuse to move: one is already
	// the answer, the other is where the work lives.
	if basesSent(bridge) != nil {
		t.Fatalf("opening the tab wrote a base: %v", basesSent(bridge))
	}

	if err := tt.Click("设为基准"); err != nil {
		t.Fatalf("no set-base button to click: %v", err)
	}
	waitFor(t, func() bool { return len(basesSent(bridge)) > 0 })
	a.drain()
	if got := basesSent(bridge); len(got) != 1 || got[0] != "release/1.0" {
		t.Fatalf("set-base sent %v, want release/1.0", got)
	}
}

// TestBranchesKeepTheirOwnChat: a list read for one chat must not land in the
// drawer of another.
func TestBranchesKeepTheirOwnChat(t *testing.T) {
	a, _ := branchFixture(t)
	a.setChangesTab(tabBranches)
	list := &protocol.Branches{Base: "main", Current: "agent/conv-12",
		Branches: []protocol.BranchRef{{Name: "main", Base: true}}}
	a.applyBranches(99, list, nil)
	if len(a.changes.branches) != 0 {
		t.Fatalf("a list for chat 99 landed in chat %d", a.active)
	}
	a.applyBranches(12, list, nil)
	if len(a.changes.branches) != 1 {
		t.Fatalf("the list for its own chat was dropped")
	}
}

// TestCreateBranchNamesItOnTheWire: the footer's field is the only path to a new
// ref, and it sends the branch the worktree is on as the start point.
func TestCreateBranchNamesItOnTheWire(t *testing.T) {
	a, bridge := branchFixture(t)
	a.changes.branchCur = "agent/conv-12"
	a.changes.branchDraft = "  spike/cache  "
	a.createBranch()
	waitFor(t, func() bool { return len(named(bridge)) > 0 })
	a.drain()
	if names := named(bridge); len(names) != 1 || names[0] != "spike/cache" {
		t.Fatalf("create sent %v, want [spike/cache]", names)
	}
	if a.changes.branchDraft != "" || a.changes.branchNaming {
		t.Fatalf("the field stayed open after a create")
	}
}

// TestSnapshotBranches writes the tab as a picture: the two moves and the numbers
// are easier to judge by eye than by assertion.
func TestSnapshotBranches(t *testing.T) {
	a, _ := branchFixture(t)
	a.setChangesTab(tabBranches)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	a.drain()
	tt.Frame()
	if !tt.HasText("release/1.0") {
		t.Fatalf("the snapshot has no branches; drew %v", tt.Texts())
	}
	writePNG(t, "/tmp/clientv2-branches.png", tt.Image())
}

// TestWorkflowTargetsShowAndSave: the tab says where 提测 hands to and where the
// release request goes, and editing one writes that project's workflow rather
// than this chat's base.
func TestWorkflowTargetsShowAndSave(t *testing.T) {
	i18n.Set(i18n.ZH)
	a, bridge := branchFixture(t)
	a.setChangesTab(tabBranches)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	// The tab reads the list and the workflow off the bridge; one drain applies
	// both answers.
	a.drain()
	tt.Frame()
	for _, want := range []string{"提测去向", "release/test", "上线去向", "feature/server"} {
		if !tt.HasText(want) {
			t.Fatalf("the tab does not say %q; drew %v", want, tt.Texts())
		}
	}
	a.changes.flowDraft = "release/2.0"
	a.saveWorkflow("testBase")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.drain()
		bridge.mu.Lock()
		n := len(bridge.flows)
		var got map[string]any
		if n > 0 {
			got = bridge.flows[n-1]
		}
		bridge.mu.Unlock()
		if n > 0 {
			if got["projectId"] != "dora" {
				t.Errorf("the write is not addressed to the project: %v", got)
			}
			if got["testBase"] != "release/2.0" {
				t.Errorf("the write does not carry the new branch: %v", got)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no workflow write reached the bridge")
}
