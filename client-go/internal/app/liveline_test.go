package app

import (
	"encoding/json"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/brand"
	"pitta/internal/protocol"
)

// liveApp is a transcript with one settled turn and a turn still running.
func liveApp(t *testing.T) *App {
	t.Helper()
	generation, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": "half a sentence"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	a := transcriptApp(append(turnWithTool(), protocol.Entry{
		ID: 9, Kind: "pi.assistant", Model: rawModel(map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "working"}}, "timestamp": 1500,
		}),
	})...)
	a.live = &protocol.LiveDoc{Run: json.RawMessage(`{"id":"run_1"}`), Generation: generation}
	return a
}

// TestLiveLineStartsAtTheLeftEdge checks the running turn's line begins where
// the rest of the turn does. The mark's frame used to be a sibling of its box,
// so the row laid out both and the phase started a mark's width too far in.
func TestLiveLineStartsAtTheLeftEdge(t *testing.T) {
	a := liveApp(t)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()

	mark, ok := tt.Find(brand.MarkLabel)
	if !ok || mark.W == 0 {
		t.Fatalf("the thinking mark is not drawn: %q", tt.Texts())
	}
	if mark.W != 20 {
		t.Fatalf("the mark is %.1f px wide, want 20", mark.W)
	}
	label, ok := tt.Find("Thinking…")
	if !ok {
		t.Skip("the running line is not in this theme's language")
	}
	// The card's padding (20) plus the mark (20) plus the row's gap (6).
	if want := mark.X + mark.W + 6; label.X != want {
		t.Fatalf("the phase starts at x=%.1f, want %.1f — the mark took the room between", label.X, want)
	}
	if mark.X > 20+1 {
		t.Fatalf("the mark starts at x=%.1f, want the card's left padding", mark.X)
	}
}

// TestLiveMarkKeepsItsSize checks the mark is one element's worth of row: a
// sibling box would have widened the row without widening the drawing.
func TestLiveMarkKeepsItsSize(t *testing.T) {
	a := liveApp(t)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()
	mark, ok := tt.Find(brand.MarkLabel)
	if !ok || mark.W == 0 {
		t.Fatalf("the thinking mark is not drawn")
	}
	if mark.H != 20 || mark.W != 20 {
		t.Fatalf("the mark is %.1fx%.1f, want 20x20", mark.W, mark.H)
	}
}
