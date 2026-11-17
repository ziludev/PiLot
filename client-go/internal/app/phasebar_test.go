package app

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
)

// TestPhaseTooltipNamesThePhaseAndItsShare checks what one stretch of the phase
// bar says: which phase it is, how long it took, and its share of the bar.
func TestPhaseTooltipNamesThePhaseAndItsShare(t *testing.T) {
	if got, want := phaseTooltip("tool", 100, 400), "tool 100ms · 25%"; got != want {
		t.Fatalf("tooltip = %q, want %q", got, want)
	}
	if got, want := phaseTooltip("wait", 1_500, 3_000), "wait 1.5s · 50%"; got != want {
		t.Fatalf("tooltip = %q, want %q", got, want)
	}
	// A bar the phases do not fill (a turn with time in no phase at all) still
	// says something rather than dividing by zero.
	if got, want := phaseTooltip("think", 0, 0), "think 0ms · 0%"; got != want {
		t.Fatalf("tooltip for an empty bar = %q, want %q", got, want)
	}
}

// TestPhaseBarExplainsItsColours checks the bar is legible on its own: under the
// pointer each stretch says what its colour was spent on. A three-pixel bar is
// otherwise a colour with no name.
func TestPhaseBarExplainsItsColours(t *testing.T) {
	a := transcriptApp(phaseEntries()...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()

	bar, ok := tt.Find(phaseBarLabel)
	if !ok || bar.W == 0 {
		t.Fatalf("the phase bar is not drawn: %q", tt.Texts())
	}
	// The legend under the bar says "think 100ms · tool 100ms" — no share — so
	// the strings below can only come from a tooltip.
	for _, phase := range []struct{ name, want string }{
		{"think", phaseTooltip("think", 100, 200)},
		{"tool", phaseTooltip("tool", 100, 200)},
	} {
		segment, ok := tt.Find(phaseBarLabel + "-" + phase.name)
		if !ok || segment.W == 0 {
			t.Fatalf("the %s stretch of the bar is not drawn", phase.name)
		}
		tt.Move(segment.X+segment.W/2, segment.Y+segment.H/2)
		tt.Frame()
		tt.Advance(1500 * time.Millisecond)
		tt.Frame()
		if !tt.HasText(phase.want) {
			t.Fatalf("resting on the %s stretch says nothing; texts=%q", phase.name, tt.Texts())
		}
	}
}

// TestPhaseBarFillsItsWidth checks the bar is the turn's time in four colours
// and not a fixed-length stub: a stretch covers its share of the whole bar, so a
// turn that spent all its time in tools is a bar that is nearly all tool.
func TestPhaseBarFillsItsWidth(t *testing.T) {
	a := transcriptApp(phaseEntries()...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()

	bar, ok := tt.Find(phaseBarLabel)
	if !ok || bar.W == 0 {
		t.Fatalf("the phase bar is not drawn")
	}
	var filled float32
	for _, phase := range []struct {
		name string
		ms   float32
	}{{"think", 100}, {"tool", 100}} {
		segment, ok := tt.Find(phaseBarLabel + "-" + phase.name)
		if !ok || segment.W == 0 {
			t.Fatalf("the %s stretch of the bar is not drawn", phase.name)
		}
		filled += segment.W
		if want := bar.W * phase.ms / 200; math.Abs(float64(segment.W-want)) > 1 {
			t.Fatalf("the %s stretch is %.1f px wide, want %.1f", phase.name, segment.W, want)
		}
	}
	if math.Abs(float64(filled-bar.W)) > 1 {
		t.Fatalf("the stretches cover %.1f of a %.1f px bar", filled, bar.W)
	}
	if bar.H < 6 {
		t.Fatalf("the bar is %.1f px tall, too short to rest a pointer on", bar.H)
	}
}

// TestPhaseBarThickensUnderThePointer checks the bar answers the pointer: it
// grows while the pointer rests on it and goes back when it leaves, and the
// stretches grow with it so the pointer keeps its target.
func TestPhaseBarThickensUnderThePointer(t *testing.T) {
	a := transcriptApp(phaseEntries()...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()

	rest, ok := tt.Find(phaseBarLabel)
	if !ok || rest.H == 0 {
		t.Fatalf("the phase bar is not drawn")
	}
	if rest.H != phaseBarHit {
		t.Fatalf("the bar rests at %.1f px, want %.1f", rest.H, float32(phaseBarHit))
	}

	tt.Move(rest.X+rest.W/2, rest.Y+rest.H/2)
	tt.Frame()
	// Part way through, it is between the two heights: a bar that jumped would
	// be at the grown height already.
	tt.Advance(40 * time.Millisecond)
	tt.Frame()
	partial, _ := tt.Find(phaseBarLabel)
	if partial.H <= rest.H || partial.H >= phaseBarHover {
		t.Fatalf("the bar is %.1f px part way through the growth, want between %.1f and %.1f", partial.H, rest.H, phaseBarHover)
	}

	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	grown, _ := tt.Find(phaseBarLabel)
	if grown.H < phaseBarHover-0.5 {
		t.Fatalf("the bar is %.1f px under the pointer, want %.1f", grown.H, phaseBarHover)
	}
	segment, ok := tt.Find(phaseBarLabel + "-tool")
	if !ok || math.Abs(float64(segment.H-grown.H)) > 0.5 {
		t.Fatalf("the %s stretch is %.1f px tall in a %.1f px bar", "tool", segment.H, grown.H)
	}

	tt.Move(0, 0)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	tt.Frame()
	if back, _ := tt.Find(phaseBarLabel); back.H > rest.H+0.5 {
		t.Fatalf("the bar stays at %.1f px once the pointer leaves, want %.1f", back.H, rest.H)
	}
}

// TestPhaseBarSkipsPhasesTheTurnNeverHad checks a stretch is only drawn — and so
// only explained — where there is time to draw.
func TestPhaseBarSkipsPhasesTheTurnNeverHad(t *testing.T) {
	a := transcriptApp(phaseEntries()...)
	tt := ui.NewTester(a.transcript, 800, 400)
	tt.Frame()
	for _, name := range []string{"wait", "error"} {
		if r, ok := tt.Find(phaseBarLabel + "-" + name); ok && r.W > 0 {
			t.Fatalf("the %s stretch is drawn for a turn that never waited or failed", name)
		}
	}
}

// phaseEntries is one turn with a stretch of thinking, a tool call, and an
// answer: enough phases for a bar with more than one colour.
func phaseEntries() []protocol.Entry {
	entry := func(id int, kind string, value any) protocol.Entry {
		data, err := json.Marshal([]any{value})
		if err != nil {
			panic(err)
		}
		return protocol.Entry{ID: id, Kind: kind, Model: data}
	}
	return []protocol.Entry{
		entry(1, "pi.user", map[string]any{
			"role": "user", "content": []any{map[string]any{"type": "text", "text": "run it"}}, "timestamp": 1000,
		}),
		entry(2, "pi.assistant", map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "let me look"}}, "timestamp": 1100,
		}),
		entry(3, "pi.assistant", map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": "c1", "name": "bash", "arguments": map[string]any{"command": "ls"}}}, "timestamp": 1200,
		}),
		entry(4, "pi.tool-result", map[string]any{
			"role": "toolResult", "toolCallId": "c1", "content": []any{map[string]any{"type": "text", "text": "a.go"}}, "timestamp": 1300,
		}),
		entry(5, "pi.assistant", map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "done"}}, "timestamp": 1400,
		}),
	}
}
