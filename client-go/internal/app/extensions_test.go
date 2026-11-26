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
	"pilot/internal/protocol"
)

// extBridge answers the extensions list and records the switch that was flipped.
// The list is stateful: a flip has to come back changed, or the window would
// redraw the old value and look like it ignored the click.
type extBridge struct {
	mu      sync.Mutex
	rows    []protocol.Extension
	flipped string
	want    bool
}

func (b *extBridge) snapshot() []protocol.Extension {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]protocol.Extension(nil), b.rows...)
}

func newExtBridge(t *testing.T, rows []protocol.Extension) (*extBridge, *httptest.Server) {
	t.Helper()
	b := &extBridge{rows: rows}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/extensions" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		if r.Method == http.MethodPost {
			var body struct {
				Name    string `json:"name"`
				Enabled bool   `json:"enabled"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			b.mu.Lock()
			b.flipped = body.Name
			b.want = body.Enabled
			for i := range b.rows {
				if b.rows[i].Name == body.Name {
					b.rows[i].Enabled = body.Enabled
				}
			}
			b.mu.Unlock()
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"extensions": b.snapshot()})
	}))
	t.Cleanup(server.Close)
	return b, server
}

// extFixture is the rail on the extensions tab with the list already read.
func extFixture(t *testing.T, rows []protocol.Extension) (*App, *extBridge) {
	t.Helper()
	bridge, server := newExtBridge(t, rows)
	a := changesFixture(t)
	a.api = api.New(server.URL, "")
	a.active = 12
	a.loadExtensions()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && a.ext.busy {
		a.drain()
		time.Sleep(10 * time.Millisecond)
	}
	if a.ext.busy {
		t.Fatal("the extensions list never arrived")
	}
	a.rail = railExtensions
	return a, bridge
}

// TestExtensionsListSaysWhatEachOneBrings: each row is a name, what it adds, and
// whether this chat gets it. The built-in ones say why they cannot be switched.
func TestExtensionsListSaysWhatEachOneBrings(t *testing.T) {
	a, _ := extFixture(t, []protocol.Extension{
		{Name: "coding-tools", Tools: []string{"bash", "read", "write"}, Enabled: true, Fixed: "on"},
		{Name: "digest", Enabled: true},
		{Name: "subagents", Tools: []string{"spawn", "wait", "stop"}, Enabled: false},
	})
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	for _, want := range []string{
		"扩展", "coding-tools", "digest", "subagents",
		"可开关", "系统", "3 个工具", "没有工具，只有钩子",
	} {
		if !tt.HasText(want) {
			t.Errorf("the list does not show %q; drew %v", want, tt.Texts())
		}
	}
}

// TestFlippingAnExtensionNamesIt: the switch sends the chat, the name and the new
// value, and the row follows what came back.
func TestFlippingAnExtensionNamesIt(t *testing.T) {
	a, bridge := extFixture(t, []protocol.Extension{
		{Name: "coding-tools", Enabled: true, Fixed: "on"},
		{Name: "digest", Enabled: true},
	})
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if err := tt.Click("digest"); err != nil {
		t.Fatalf("the digest row is not clickable: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.drain()
		bridge.mu.Lock()
		got := bridge.flipped
		bridge.mu.Unlock()
		if got == "digest" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.want {
		t.Error("flipping an enabled extension should turn it off")
	}
}

// TestExtensionsNeedAChat: the switch is per chat, so with no chat there is
// nothing to switch and the list says so rather than lying with a default.
func TestExtensionsNeedAChat(t *testing.T) {
	a, _ := extFixture(t, []protocol.Extension{{Name: "digest", Enabled: true}})
	a.active = 0
	a.loadExtensions()
	a.drain()
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("先打开一个会话") {
		t.Errorf("an unopened chat should say why the list is empty: %v", tt.Texts())
	}
}

// TestSnapshotExtensions draws the tab for the eye.
func TestSnapshotExtensions(t *testing.T) {
	a, _ := extFixture(t, []protocol.Extension{
		{Name: "coding-tools", Tools: []string{"bash", "read", "write", "edit"}, Enabled: true, Fixed: "on"},
		{Name: "memory", Tools: []string{"remember", "recall"}, Enabled: true},
		{Name: "subagents", Tools: []string{"spawn", "wait", "stop", "steer"}, Enabled: false},
		{Name: "workflow", Tools: []string{"workflow_status"}, Enabled: true},
	})
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	writePNG(t, "/tmp/clientv2-extensions.png", tt.Image())
}
