package app

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

const modelCatalog = `{"models":[
 {"provider":"anthropic","providerName":"Anthropic","id":"claude-sonnet-4.5","name":"Claude Sonnet 4.5","contextWindow":200000,"cost":{"input":3,"output":15},"thinkingLevels":["off","low","high"]},
 {"provider":"anthropic","providerName":"Anthropic","id":"claude-haiku-4.5","name":"Claude Haiku 4.5","contextWindow":200000,"cost":{"input":0.8,"output":4}},
 {"provider":"google","providerName":"Google","id":"gemini-2.5-pro","name":"Gemini 2.5 Pro","contextWindow":1000000,"cost":{"input":1.25,"output":10}},
 {"provider":"deepseek","providerName":"DeepSeek","id":"deepseek-chat","name":"DeepSeek Chat","contextWindow":64000,"cost":{"input":0.27,"output":1.1}}
 ],"default":{"provider":"anthropic","providerName":"Anthropic","id":"claude-sonnet-4.5"}}`

// modelBridge serves the catalog and records the writes that point a chat at
// a model. status decides whether the write lands.
type modelBridge struct {
	srv    *httptest.Server
	writes chan map[string]any
	status int
}

func newModelBridge(t *testing.T) *modelBridge {
	t.Helper()
	b := &modelBridge{writes: make(chan map[string]any, 8), status: http.StatusOK}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models":
			_, _ = w.Write([]byte(modelCatalog))
		case "/api/model":
			body, _ := io.ReadAll(r.Body)
			var write map[string]any
			_ = json.Unmarshal(body, &write)
			b.writes <- write
			w.WriteHeader(b.status)
			_, _ = w.Write([]byte(`{}`))
		case "/api/conversation":
			_, _ = w.Write([]byte(`{"conversationId":12,"entries":[],"model":{"provider":"anthropic","modelId":"claude-sonnet-4.5"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *modelBridge) take(t *testing.T) map[string]any {
	t.Helper()
	select {
	case write := <-b.writes:
		return write
	case <-time.After(2 * time.Second):
		t.Fatal("the client never told the bridge about the model")
		return nil
	}
}

// tuckedPrefs keeps a test's model choices out of the real window's file.
func tuckedPrefs(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	saved := prefsPath
	prefsPath = func() string { return filepath.Join(dir, "window.json") }
	t.Cleanup(func() { prefsPath = saved })
}

// pickerApp is one chat on a model, with the catalog already in hand.
func pickerApp(t *testing.T, b *modelBridge) *App {
	t.Helper()
	tuckedPrefs(t)
	a := railApp(t, b.srv.URL)
	a.model = &protocol.ModelRef{Provider: "anthropic", ModelID: "claude-sonnet-4.5"}
	a.pick.catalog = catalogOf(t)
	a.pick.fetched = true
	return a
}

func catalogOf(t *testing.T) []protocol.ModelEntry {
	t.Helper()
	var list protocol.Models
	if err := json.Unmarshal([]byte(modelCatalog), &list); err != nil {
		t.Fatal(err)
	}
	return list.Models
}

func TestModelMatches(t *testing.T) {
	i18n.Set(i18n.EN)
	b := newModelBridge(t)
	a := pickerApp(t, b)

	// Nothing typed: the chat's own model leads, the rest keep the catalog order.
	got := a.modelMatches()
	if len(got) != 4 || got[0].ID != "claude-sonnet-4.5" || got[1].ID != "claude-haiku-4.5" {
		t.Fatalf("unfiltered = %v", ids(got))
	}
	// The current model is first even when it is last in the catalog.
	a.model = &protocol.ModelRef{Provider: "deepseek", ModelID: "deepseek-chat"}
	if got := a.modelMatches(); got[0].ID != "deepseek-chat" {
		t.Fatalf("current model not first: %v", ids(got))
	}
	// The filter reads the name, the id and the provider.
	a.pick.filter = "gem"
	if got := a.modelMatches(); len(got) != 1 || got[0].ID != "gemini-2.5-pro" {
		t.Fatalf("filter gem = %v", ids(got))
	}
	a.pick.filter = "ANTHROPIC"
	if got := a.modelMatches(); len(got) != 2 {
		t.Fatalf("filter is case-insensitive: %v", ids(got))
	}
	a.pick.filter = "nothing here"
	if got := a.modelMatches(); len(got) != 0 {
		t.Fatalf("filter that matches nothing = %v", ids(got))
	}
}

func ids(entries []protocol.ModelEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.ID
	}
	return out
}

// TestModelChipOpensAndPicks proves the path a mouse takes: the chip opens the
// panel, a click on a row points the chat at that model and closes it.
func TestModelChipOpensAndPicks(t *testing.T) {
	i18n.Set(i18n.EN)
	b := newModelBridge(t)
	a := pickerApp(t, b)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	if err := tt.Click("Claude Sonnet 4.5"); err != nil {
		t.Fatalf("chip: %v", err)
	}
	tt.Frame()
	if !a.pick.open {
		t.Fatal("chip did not open the panel")
	}
	// Providers start folded except the one the chat is on, so a model from
	// another provider is two clicks: the provider, then the model.
	if err := tt.Click("Google"); err != nil {
		t.Fatalf("provider heading: %v", err)
	}
	tt.Frame()
	if err := tt.Click("Gemini 2.5 Pro"); err != nil {
		t.Fatalf("row: %v", err)
	}
	tt.Frame()
	if a.pick.open {
		t.Error("panel stayed open after a pick")
	}
	if a.model.ModelID != "gemini-2.5-pro" {
		t.Errorf("model = %v", a.model)
	}
	write := b.take(t)
	if write["conversationId"] != float64(12) || write["modelId"] != "gemini-2.5-pro" ||
		write["provider"] != "google" {
		t.Errorf("write = %v", write)
	}
	if _, ok := write["thinkingLevel"]; ok {
		t.Errorf("a model with no levels was sent one: %v", write)
	}
}

// TestModelKeyboardPicks is the other path: the filter takes the keyboard, the
// arrows walk it, Enter chooses the highlighted row.
func TestModelKeyboardPicks(t *testing.T) {
	i18n.Set(i18n.EN)
	b := newModelBridge(t)
	a := pickerApp(t, b)
	tt := ui.NewTester(a.View, 1180, 820)
	a.openModelPicker()
	tt.Frame()

	a.pick.filter = "claude"
	tt.Frame()
	if a.pick.sel != 0 {
		t.Fatalf("a new filter starts at the top, sel = %d", a.pick.sel)
	}
	a.modelKey(ui.TextKey{Key: ui.KeyDown})
	if a.pick.sel != 1 {
		t.Fatalf("down did not move the selection: %d", a.pick.sel)
	}
	// The list ends where it began.
	a.modelKey(ui.TextKey{Key: ui.KeyDown})
	if a.pick.sel != 0 {
		t.Fatalf("down past the end did not wrap: %d", a.pick.sel)
	}
	a.modelKey(ui.TextKey{Key: ui.KeyDown})
	if !a.modelKey(ui.TextKey{Key: ui.KeyEnter}) {
		t.Fatal("Enter was not taken by the panel")
	}
	if write := b.take(t); write["modelId"] != "claude-haiku-4.5" {
		t.Errorf("Enter chose %v", write)
	}
	a.openModelPicker()
	a.modelKey(ui.TextKey{Key: ui.KeyEscape})
	if a.pick.open {
		t.Error("Escape left the panel open")
	}
}

// TestModelLevelSlider changes the level of the chat's own model, which is the
// only other thing the panel does. The levels are ordered, so the control is a
// slider: clicking its middle lands on the middle level.
func TestModelLevelSlider(t *testing.T) {
	i18n.Set(i18n.EN)
	b := newModelBridge(t)
	a := pickerApp(t, b)
	a.pick.open = true
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	label, ok := tt.Find("Thinking")
	if !ok {
		t.Fatalf("no slider label; drew %v", tt.Texts())
	}
	// Nothing has been chosen in this chat, so the readout starts at the bottom
	// stop, which is also what the bridge does with no level set.
	readout, ok := tt.Find("off")
	if !ok {
		t.Fatalf("no level readout; drew %v", tt.Texts())
	}
	middle := label.X + label.W + (readout.X-(label.X+label.W))/2
	tt.ClickAt(middle, readout.Y+readout.H/2)
	tt.Frame()
	if a.thinking != "low" {
		t.Fatalf("thinking = %q, want the middle stop", a.thinking)
	}
	write := b.take(t)
	if write["modelId"] != "claude-sonnet-4.5" || write["thinkingLevel"] != "low" {
		t.Errorf("write = %v", write)
	}
}

// TestModelSwapDropsTheLevel: a level belongs to the model that takes it, so
// moving to one without levels must not carry "high" along.
func TestModelSwapDropsTheLevel(t *testing.T) {
	i18n.Set(i18n.EN)
	b := newModelBridge(t)
	a := pickerApp(t, b)
	a.thinking = "high"
	a.chooseModel(protocol.ModelEntry{Provider: "deepseek", ID: "deepseek-chat"})
	if a.thinking != "" {
		t.Errorf("level survived the swap: %q", a.thinking)
	}
	if write := b.take(t); write["thinkingLevel"] != nil {
		t.Errorf("write carried a level: %v", write)
	}
}

// TestModelFailureReloads: a bridge that refuses the write leaves the chip
// showing a model the chat is not on, so the chat is asked again.
func TestModelFailureReloads(t *testing.T) {
	i18n.Set(i18n.EN)
	b := newModelBridge(t)
	b.status = http.StatusInternalServerError
	a := pickerApp(t, b)
	a.chooseModel(protocol.ModelEntry{Provider: "google", ID: "gemini-2.5-pro"})
	b.take(t)
	waitFor(t, func() bool {
		a.drain()
		return a.model != nil && a.model.ModelID == "claude-sonnet-4.5"
	})
}

// TestModelRecentsLeadThePanel: the catalog's own order starts with whichever
// provider sorts first, which is nobody's question. What the person picked last
// is the better first guess, and it survives in prefs.
func TestModelRecentsLeadThePanel(t *testing.T) {
	i18n.Set(i18n.EN)
	b := newModelBridge(t)
	a := pickerApp(t, b)
	a.openModelPicker()
	a.pick.filter = "gemini"
	a.chooseModel(a.modelMatches()[0])
	waitFor(t, func() bool { return a.model != nil && a.model.ModelID == "gemini-2.5-pro" })

	a.openModelPicker()
	got := ids(a.modelMatches())
	if got[0] != "gemini-2.5-pro" {
		t.Fatalf("current model first: %v", got)
	}
	// The model just picked is remembered even when another chat opens the panel.
	a.model = &protocol.ModelRef{Provider: "deepseek", ModelID: "deepseek-chat"}
	got = ids(a.modelMatches())
	if got[0] != "deepseek-chat" || got[1] != "gemini-2.5-pro" {
		t.Fatalf("recent model second, behind the current one: %v", got)
	}
	if _, err := os.Stat(prefsPath()); err != nil {
		t.Fatalf("prefs: %v", err)
	}
	data, err := os.ReadFile(prefsPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "google/gemini-2.5-pro") {
		t.Fatalf("prefs = %s", data)
	}
}

// TestModelGroupsFoldByProvider: the panel is a folded list of providers, so a
// model from one that is not the chat's own is behind its heading — and the
// heading folds again when asked.
func TestModelGroupsFoldByProvider(t *testing.T) {
	i18n.Set(i18n.EN)
	b := newModelBridge(t)
	a := pickerApp(t, b)
	a.openModelPicker()
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	if tt.HasText("Gemini 2.5 Pro") {
		t.Fatalf("a folded provider drew its models; texts = %v", tt.Texts())
	}
	if !tt.HasText("Google") {
		t.Fatalf("no heading for the folded provider; texts = %v", tt.Texts())
	}
	if err := tt.Click("Google"); err != nil {
		t.Fatalf("heading: %v", err)
	}
	tt.Frame()
	if !tt.HasText("Gemini 2.5 Pro") {
		t.Fatalf("unfolding did not show the model; texts = %v", tt.Texts())
	}
	if err := tt.Click("Google"); err != nil {
		t.Fatalf("heading again: %v", err)
	}
	tt.Frame()
	if tt.HasText("Gemini 2.5 Pro") {
		t.Fatalf("folding back did not hide the model; texts = %v", tt.Texts())
	}
}
