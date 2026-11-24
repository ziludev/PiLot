package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
)

// mergeBridge answers the merge tab and records what was asked of it: the title
// the drawer sent, and the id it merged.
type mergeBridge struct {
	read   string
	write  string
	mu     sync.Mutex
	titles []string
	merged []int
}

func newMergeBridge(t *testing.T, read, write string) (*mergeBridge, *httptest.Server) {
	t.Helper()
	b := &mergeBridge{read: read, write: write}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/mr":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(b.read))
				return
			}
			var body struct {
				Action string `json:"action"`
				Title  string `json:"title"`
				IID    int    `json:"iid"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			b.mu.Lock()
			if body.Action == "merge" {
				b.merged = append(b.merged, body.IID)
			} else {
				b.titles = append(b.titles, body.Title)
			}
			b.mu.Unlock()
			_, _ = w.Write([]byte(b.write))
		case "/api/changes":
			_, _ = w.Write([]byte(`{"files":[],"patch":"","branch":"agent/conv-12"}`))
		case "/api/git":
			_, _ = w.Write([]byte(`{"branch":"agent/conv-12","base":"main","commits":[]}`))
		case "/api/branches":
			_, _ = w.Write([]byte(`{"base":"main","current":"agent/conv-12","branches":[]}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(server.Close)
	return b, server
}

func titlesSent(b *mergeBridge) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.titles...)
}

func mergedIDs(b *mergeBridge) []int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]int(nil), b.merged...)
}

const mergeOpen = `{"project":"/w","branch":"agent/conv-12","base":"main",
	"remote":{"kind":"gitlab","webUrl":"https://gitlab.example.com/g/p"},
	"status":"ok","mr":{"iid":3165,"title":"feat: 设备上下文","state":"opened","draft":false,
	"webUrl":"https://gitlab.example.com/g/p/-/merge_requests/3165",
	"source":"agent/conv-12","target":"main","merge":"conflicted","notes":24,"author":"周永国"}}`

// mergeFixture is the merge tab open on a bridge with a canned answer.
func mergeFixture(t *testing.T, read, write string) (*App, *mergeBridge) {
	t.Helper()
	bridge, server := newMergeBridge(t, read, write)
	a := changesFixture(t)
	a.active, a.changes.conv = 12, 12
	a.changes.workspace = "dora-service"
	a.api = api.New(server.URL, "")
	a.changes.open = true
	// The inbox holds one frame's worth of events: a refresh whose answer is not
	// drained first would push the merge tab's own answer out of it.
	a.drain()
	// Draining applies the fixture's own changes answer, which clears conv.
	a.active, a.changes.conv = 12, 12
	a.setChangesTab(tabMerge)
	waitFor(t, func() bool { a.drain(); return a.changes.mr.Status != "" || a.changes.mrFailed != "" })
	return a, bridge
}

// TestMergeTabShowsTheOpenRequest: the tab says which request, whether it would
// merge, and who is talking on it.
func TestMergeTabShowsTheOpenRequest(t *testing.T) {
	a, _ := mergeFixture(t, mergeOpen, `{}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{"!3165", "feat: 设备上下文", "有冲突", "周永国", "合并", "打开网页"} {
		if !tt.HasText(want) {
			t.Errorf("the merge tab does not show %q; drew %v", want, tt.Texts())
		}
	}
}

// TestMergeTabOffersToOpenOne: with no request the tab is a form, and creating one
// sends the title the field holds.
func TestMergeTabOffersToOpenOne(t *testing.T) {
	a, bridge := mergeFixture(t, `{"project":"/w","branch":"agent/conv-12","base":"main","status":"ok"}`,
		`{"status":"created","url":"https://gitlab.example.com/g/p/-/merge_requests/7"}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("这个分支还没有合并请求") {
		t.Fatalf("the empty tab does not say so; drew %v", tt.Texts())
	}
	a.changes.mrTitle = "把分支列表画出来"
	a.createMR()
	waitFor(t, func() bool { a.drain(); return len(titlesSent(bridge)) > 0 })
	if got := titlesSent(bridge); len(got) != 1 || got[0] != "把分支列表画出来" {
		t.Fatalf("the create sent %v", got)
	}
}

// TestMergeTabSendsTheId: the merge button names the request it merges, not the
// branch.
func TestMergeTabSendsTheId(t *testing.T) {
	a, bridge := mergeFixture(t, mergeOpen, `{"status":"merged","iid":3165}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("合并"); err != nil {
		t.Fatalf("the merge button is not clickable: %v", err)
	}
	waitFor(t, func() bool { a.drain(); return len(mergedIDs(bridge)) > 0 })
	if got := mergedIDs(bridge); len(got) != 1 || got[0] != 3165 {
		t.Fatalf("the merge sent %v", got)
	}
}

// TestMergeTabNamesTheMissingLogin: a bridge without a token is not an empty
// drawer, and the tab says which command fixes it.
func TestMergeTabNamesTheMissingLogin(t *testing.T) {
	a, _ := mergeFixture(t, `{"status":"not-authenticated","message":"No token found"}`, `{}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("glab 没有登录") || !tt.HasText("glab auth login") {
		t.Fatalf("the tab does not name the fix; drew %v", tt.Texts())
	}
}

// TestMergeTabOpensTheRequestPage: the button opens the request's own URL, not
// the project's or the branch comparison page.
func TestMergeTabOpensTheRequestPage(t *testing.T) {
	a, _ := mergeFixture(t, mergeOpen, `{}`)
	var opened []string
	openExternal = func(url string) error {
		opened = append(opened, url)
		return nil
	}
	defer func() { openExternal = func(string) error { return nil } }()
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("打开网页"); err != nil {
		t.Fatalf("the page button is not clickable: %v", err)
	}
	if len(opened) != 1 || opened[0] != "https://gitlab.example.com/g/p/-/merge_requests/3165" {
		t.Fatalf("opened %v", opened)
	}
}

// TestSnapshotMerge draws the tab with a request on it.
func TestSnapshotMerge(t *testing.T) {
	a, _ := mergeFixture(t, mergeOpen, `{}`)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	tt.Frame()
	writePNG(t, "/tmp/clientv2-merge.png", tt.Image())
}
