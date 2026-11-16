package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

// pickerModels is a small catalogue with the shapes the bridge sends.
func pickerModels() []protocol.ModelEntry {
	return []protocol.ModelEntry{
		{Provider: "anthropic", ProviderName: "Anthropic", ID: "claude-opus", Name: "Opus",
			ContextWindow: 200_000, Cost: protocol.ModelCost{Input: 15, Output: 75},
			ThinkingLevels: []string{"off", "low", "high"}},
		{Provider: "anthropic", ProviderName: "Anthropic", ID: "claude-haiku", Name: "Haiku",
			ContextWindow: 200_000, Cost: protocol.ModelCost{Input: 0.8, Output: 4}},
		{Provider: "sitin", ProviderName: "Sitin", ID: "deepseek-flash", Name: "DeepSeek Flash",
			ContextWindow: 1_048_576, Cost: protocol.ModelCost{}},
	}
}

// TestModelPickerListsAndPicks checks the picker's two jobs: it narrows by
// query, and a row click switches the conversation's model through the bridge.
func TestModelPickerListsAndPicks(t *testing.T) {
	chosen := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/model" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			chosen <- body
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.current = 11
	a.models = pickerModels()
	i18n.Set(i18n.EN)
	a.modelOpen = true

	view := func(c *ui.Context) { a.modelPalette(c) }
	tt := ui.NewTester(view, 420, 460)
	tt.Frame()

	for _, want := range []string{"Opus", "Haiku", "DeepSeek Flash"} {
		if !tt.HasText(want) {
			t.Fatalf("%q missing; texts=%q", want, tt.Texts())
		}
	}
	// The price and window lines are what makes the list usable.
	if !tt.HasText("per M $15 / $75 · 200k") {
		t.Fatalf("price line missing; texts=%q", tt.Texts())
	}
	if !tt.HasText("free · 1M") {
		t.Fatalf("free line missing; texts=%q", tt.Texts())
	}

	if err := tt.Click("Haiku"); err != nil {
		t.Fatalf("click Haiku: %v", err)
	}
	select {
	case body := <-chosen:
		if body["modelId"] != "claude-haiku" || body["provider"] != "anthropic" {
			t.Fatalf("set model body = %v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the picker never asked the bridge")
	}
	if a.model == nil || a.model.ModelID != "claude-haiku" {
		t.Fatalf("active model = %+v", a.model)
	}
}

// TestModelPickerSearchesAndStars checks the query and the favourite star.
func TestModelPickerSearchesAndStars(t *testing.T) {
	a := New(Config{})
	a.models = pickerModels()
	i18n.Set(i18n.EN)
	a.modelOpen = true
	a.modelQuery = "deep"

	tt := ui.NewTester(func(c *ui.Context) { a.modelPalette(c) }, 420, 460)
	tt.Frame()
	if !tt.HasText("DeepSeek Flash") {
		t.Fatalf("query hid the match; texts=%q", tt.Texts())
	}
	if tt.HasText("Opus") {
		t.Fatalf("query kept a non-match; texts=%q", tt.Texts())
	}

	a.modelQuery = ""
	tt.Frame()
	starred := protocol.ModelRef{Provider: "anthropic", ModelID: "claude-haiku"}
	a.apply(func() { a.toggleFavorite(starred) })
	if len(a.modelsFav) != 1 || a.modelsFav[0] != "anthropic/claude-haiku" {
		t.Fatalf("favourites = %v", a.modelsFav)
	}
	// A favourite sorts above the rest even when the name does not.
	if got := a.visibleModels()[0].ID; got != "claude-haiku" {
		t.Fatalf("favourite did not sort first, got %q", got)
	}
}

// TestThinkingRowOnlyOffersWhatTheModelHas keeps the level row honest: a model
// without levels draws none.
func TestThinkingRowOnlyOffersWhatTheModelHas(t *testing.T) {
	a := New(Config{})
	a.models = pickerModels()
	a.model = &protocol.ModelRef{Provider: "anthropic", ModelID: "claude-opus"}
	a.modelOpen = true

	tt := ui.NewTester(func(c *ui.Context) { a.modelPalette(c) }, 420, 460)
	tt.Frame()
	for _, level := range []string{"off", "low", "high"} {
		if !tt.HasText(level) {
			t.Fatalf("level %q missing; texts=%q", level, tt.Texts())
		}
	}
	if tt.HasText("medium") {
		t.Fatalf("level the model lacks was drawn; texts=%q", tt.Texts())
	}

	a.model = &protocol.ModelRef{Provider: "sitin", ModelID: "deepseek-flash"}
	tt.Frame()
	if tt.HasText("off") {
		t.Fatalf("a model without levels drew them; texts=%q", tt.Texts())
	}
}

// TestModelPickerWalksWithArrowKeys covers the earlier client's view's keyboard path:
// the search box keeps focus, the arrows move the highlight, Enter picks.
func TestModelPickerWalksWithArrowKeys(t *testing.T) {
	chosen := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/model" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			chosen <- body
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	a := New(Config{BaseURL: srv.URL, Token: "t"})
	a.current = 11
	a.models = pickerModels()
	i18n.Set(i18n.EN)
	a.modelOpen = true

	tt := ui.NewTester(func(c *ui.Context) { a.modelPalette(c) }, 420, 460)
	tt.Frame()
	if a.modelCursor != 0 {
		t.Fatalf("cursor starts at %d, want 0", a.modelCursor)
	}

	// The query box is the only focusable part of the panel; Enter reaches it as
	// the input's submit, the arrows through the window shortcuts.
	// Tab focuses the query box: the palette owns the keys from there.
	tt.Key(0, ui.KeyTab)
	tt.Frame()
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.modelCursor != 2 {
		t.Fatalf("two arrow presses moved the cursor to %d, want 2", a.modelCursor)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if a.modelCursor != 1 {
		t.Fatalf("arrow up moved the cursor to %d, want 1", a.modelCursor)
	}

	// Read the highlighted row before Enter: choosing it moves it to the top of
	// the recents, which reorders the list.
	want := a.visibleModels()[1]
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	select {
	case body := <-chosen:
		if body["modelId"] != want.ID || body["provider"] != want.Provider {
			t.Fatalf("Enter picked %v, want the highlighted row %s/%s", body, want.Provider, want.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enter did not pick the highlighted model")
	}
}
