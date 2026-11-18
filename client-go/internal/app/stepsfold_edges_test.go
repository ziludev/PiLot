package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
)

// stepShape is how a built step ends: with a result, with a failed one, or not
// at all because it is still out there.
type stepShape int

const (
	stepDone stepShape = iota
	stepFailed
	stepRunning
)

// foldTurn builds one turn of n tool calls shaped per step, priced at cost
// dollars on every call. base keeps ids apart in the tests that need two turns
// in one chat.
func foldTurn(base, n int, shape func(i int) stepShape, cost float64) []protocol.Entry {
	entries := []protocol.Entry{
		{ID: base, Kind: "pi.user", Model: rawModel(map[string]any{"role": "user", "content": "跑一遍"})},
	}
	for i := 0; i < n; i++ {
		callID := "call_" + strconv.Itoa(base) + "_" + strconv.Itoa(i)
		message := map[string]any{
			"role": "assistant", "timestamp": float64((i + 1) * 1000),
			"content": []any{map[string]any{
				"type": "toolCall", "id": callID, "name": "bash",
				"arguments": map[string]any{"command": "go test ./..."},
			}},
		}
		if cost > 0 && i == 0 {
			message["usage"] = map[string]any{"cost": map[string]any{"total": cost}}
		}
		entries = append(entries, protocol.Entry{ID: base + 1 + i*2, Kind: "pi.assistant", Model: rawModel(message)})
		if shape(i) == stepRunning {
			continue
		}
		result := map[string]any{
			"role": "toolResult", "toolCallId": callID, "toolName": "bash",
			"content": []any{map[string]any{"type": "text", "text": "ok"}},
		}
		if shape(i) == stepFailed {
			result["isError"] = true
			result["content"] = []any{map[string]any{"type": "text", "text": "FAIL"}}
		}
		entries = append(entries, protocol.Entry{ID: base + 2 + i*2, Kind: "pi.tool-result", Model: rawModel(result)})
	}
	return entries
}

// chatFoldRows is how many collapse lines a transcript is showing.
func chatFoldRows(rows []chatRow) int {
	folds := 0
	for _, row := range rows {
		if row.kind == chatFold {
			folds++
		}
	}
	return folds
}

// TestFoldStartsOneStepAboveTheFloor pins both sides of the threshold. Three
// steps are read at a glance and must not grow a "collapsed" line that costs
// more attention than it saves; four is where folding starts to pay.
func TestFoldStartsOneStepAboveTheFloor(t *testing.T) {
	for _, tc := range []struct {
		steps int
		fold  bool
	}{{steps: 3, fold: false}, {steps: 4, fold: true}} {
		a := transcriptApp(foldTurn(1, tc.steps, func(int) stepShape { return stepDone }, 0)...)
		rows := a.buildChatRows(a.groupedTurns())
		if got := chatFoldRows(rows) == 1; got != tc.fold {
			t.Fatalf("%d steps: fold line = %v, want %v", tc.steps, got, tc.fold)
		}
		if tc.fold {
			for _, row := range rows {
				if row.kind == chatFold && len(row.folded) != tc.steps {
					t.Fatalf("fold line stands for %d steps, want %d", len(row.folded), tc.steps)
				}
			}
		}
	}
}

// TestFoldLineKeepsTheCost is the number the folded line must not swallow: the
// turn's bill. Folded it has to sit on the line with the count, opened it moves
// back to the summary — once, in one place, either way.
func TestFoldLineKeepsTheCost(t *testing.T) {
	entries := foldTurn(1, 4, func(int) stepShape { return stepDone }, 0.04)
	a := transcriptApp(entries...)
	built := a.groupedTurns()
	if built[0].Stats.Cost <= 0 {
		t.Fatalf("fixture did not bill the turn: cost = %v", built[0].Stats.Cost)
	}

	tt := ui.NewTester(a.transcript, 800, 600)
	tt.Frame()
	if !tt.HasText("$0.04") {
		t.Fatalf("folded line drops the cost: %q", tt.Texts())
	}
	if n := textCount(tt.Texts(), "$0.04"); n != 1 {
		t.Fatalf("cost printed %d times while folded, want once: %q", n, tt.Texts())
	}
	if tt.HasText(plural(4, "tool")) {
		t.Fatalf("folded turn also draws its summary line: %q", tt.Texts())
	}

	a.stepsOpen[built[0].ID] = true
	opened := ui.NewTester(a.transcript, 800, 600)
	opened.Frame()
	if n := textCount(opened.Texts(), "$0.04"); n != 1 {
		t.Fatalf("cost printed %d times when open, want once: %q", n, opened.Texts())
	}
	if !opened.HasText(plural(4, "tool")) {
		t.Fatalf("opened turn lost its summary line: %q", opened.Texts())
	}
}

// textCount is how many labels of a frame contain want, so a test can tell
// "the number is still there" from "the number is there twice".
func textCount(texts []string, want string) int {
	n := 0
	for _, text := range texts {
		if strings.Contains(text, want) {
			n++
		}
	}
	return n
}

// TestFoldLineNeedsSomethingToFold is the degenerate long turn: every step
// failed, so no run may fold and the turn keeps its rows (and its summary).
func TestFoldLineNeedsSomethingToFold(t *testing.T) {
	entries := foldTurn(1, 5, func(int) stepShape { return stepFailed }, 0)
	a := transcriptApp(entries...)
	rows := a.buildChatRows(a.groupedTurns())
	if n := chatFoldRows(rows); n != 0 {
		t.Fatalf("turn of nothing but failures grew %d fold lines", n)
	}
	shown := 0
	for _, row := range rows {
		if row.kind == chatSteps {
			shown += len(row.steps)
		}
	}
	if shown != 5 {
		t.Fatalf("%d failed steps drawn as rows, want all 5", shown)
	}
}

// TestStepsOpenIsPerTurn keeps the map honest about its key: opening one turn
// of a chat may not spill into the next one. An opened turn keeps its fold line
// as a header and adds its rows under it; a shut turn draws only the header.
func TestStepsOpenIsPerTurn(t *testing.T) {
	entries := append(
		foldTurn(1, 4, func(int) stepShape { return stepDone }, 0),
		foldTurn(100, 4, func(int) stepShape { return stepDone }, 0)...,
	)
	a := transcriptApp(entries...)
	built := a.groupedTurns()
	if len(built) != 2 {
		t.Fatalf("fixture made %d turns, want 2", len(built))
	}

	a.stepsOpen[built[0].ID] = true
	rows := a.buildChatRows(built)

	if n := chatFoldRows(rows); n != 2 {
		t.Fatalf("chat shows %d fold lines, want one header per long turn", n)
	}
	if !stepsOf(rows, built[0].ID) {
		t.Fatal("the open turn drew none of its steps")
	}
	if stepsOf(rows, built[1].ID) {
		t.Fatal("opening one turn unfolded the other")
	}
}

// stepsOf reports whether the turn has its own step rows in the layout.
func stepsOf(rows []chatRow, turnID int) bool {
	for _, row := range rows {
		if row.kind == chatSteps && row.turn.ID == turnID {
			return true
		}
	}
	return false
}

// TestFoldLineStandsInForItsSteps keeps jumpRow's promise honest: a step under
// the folded line still has a row to be found on, so a search hit never points
// at nothing.
func TestFoldLineStandsInForItsSteps(t *testing.T) {
	entries := foldTurn(1, 5, func(int) stepShape { return stepDone }, 0)
	a := transcriptApp(entries...)
	built := a.groupedTurns()
	rows := a.buildChatRows(built)
	if len(rows) == 0 {
		t.Fatal("no rows built")
	}
	want := built[0].Steps[4].Entry.ID
	for _, row := range rows {
		if row.kind != chatFold {
			continue
		}
		for _, step := range row.folded {
			if step.Entry.ID == want {
				return
			}
		}
		t.Fatalf("last step %d is not in the fold line's %d steps", want, len(row.folded))
	}
	t.Fatal("long turn has no fold line")
}

// stepsFoldAt is the documented floor; the product may move it, but not by
// accident.
func TestStepsFoldFloorMatchesTheDocumentedThree(t *testing.T) {
	if stepsFoldAt != 3 {
		t.Fatalf("stepsFoldAt = %d, the tests and the docs say 3", stepsFoldAt)
	}
}
