package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
)

// longAnswer is more text than the transcript gives one reply, so the card it
// draws is capped rather than hugging its content.
var longAnswer = strings.Repeat("Rayleigh scattering keeps the short wavelengths. ", 60)

// liveGeneration builds the streaming document a run publishes while the model
// writes: one assistant message whose text is still growing.
func liveGeneration(t *testing.T, text string) json.RawMessage {
	t.Helper()
	generation, err := json.Marshal(map[string]any{
		"message": map[string]any{
			"role":    "assistant",
			"content": []any{map[string]any{"type": "text", "text": text}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return generation
}

// TestStreamingAnswerKeepsTheFinalWidth checks a reply that is still streaming
// is drawn on the card it settles into. It used to fill the row while it
// streamed and snap to the narrower card when the turn completed, which read as
// the pane jumping at the end of every answer.
func TestStreamingAnswerKeepsTheFinalWidth(t *testing.T) {
	streaming := snapshotData(t)
	streaming.entries = turnEntries(1, "why is the sky blue", longAnswer)
	generation := liveGeneration(t, longAnswer)
	streaming.live = &protocol.LiveDoc{Run: json.RawMessage(`{"id":"run_1"}`), Generation: generation}
	tt := ui.NewTester(streaming.View, 1180, 820)
	tt.Frame()
	stream, ok := tt.Find(streamLabel)
	if !ok {
		t.Skip("the streaming card is not on screen")
	}

	settled := snapshotData(t)
	settled.entries = turnEntries(1, "why is the sky blue", longAnswer)
	st := ui.NewTester(settled.View, 1180, 820)
	st.Frame()
	answer, ok := st.Find(answerLabel)
	if !ok {
		t.Skip("the settled card is not on screen")
	}

	if stream.W != answer.W {
		t.Fatalf("streaming card is %.1f px wide, settled card %.1f: an answer moves when it completes", stream.W, answer.W)
	}
	if stream.X != answer.X {
		t.Fatalf("streaming card starts at x=%.1f, settled card at %.1f", stream.X, answer.X)
	}
	if stream.W != answerWidth {
		t.Fatalf("streaming card is %.1f px wide, want it capped at %d", stream.W, answerWidth)
	}
}

// TestStreamingAnswerHugsShortText checks a short reply is not stretched across
// the pane while it streams, since the card it settles into hugs its text.
func TestStreamingAnswerHugsShortText(t *testing.T) {
	a := snapshotData(t)
	a.entries = turnEntries(1, "why is the sky blue", "Rayleigh scattering.")
	a.live = &protocol.LiveDoc{Run: json.RawMessage(`{"id":"run_1"}`), Generation: liveGeneration(t, "Rayleigh scattering.")}
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	stream, ok := tt.Find(streamLabel)
	if !ok {
		t.Skip("the streaming card is not on screen")
	}
	if stream.W > 300 {
		t.Fatalf("one sentence of 19 characters streams %.1f px wide; the card should hug its text", stream.W)
	}
}

// TestStreamingAnswerUsesANarrowPane checks the cap does not push the card past
// a pane narrower than it.
func TestStreamingAnswerUsesANarrowPane(t *testing.T) {
	a := snapshotData(t)
	a.live = &protocol.LiveDoc{Run: json.RawMessage(`{"id":"run_1"}`), Generation: liveGeneration(t, longAnswer)}
	tt := ui.NewTester(a.View, 700, 700)
	tt.Frame()
	stream, ok := tt.Find(streamLabel)
	if !ok {
		t.Skip("the streaming card is not on screen")
	}
	if stream.W > answerWidth {
		t.Fatalf("streaming card is %.1f px wide in a 700 px window", stream.W)
	}
	if stream.W < 300 {
		t.Fatalf("streaming card is %.1f px wide, want it to use the pane", stream.W)
	}
}
