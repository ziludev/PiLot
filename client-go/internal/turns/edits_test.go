package turns

import "testing"

// callStep is a step holding one tool call with the arguments the bridge writes.
func callStep(id int, name string, args any) Step {
	return Step{Entry: entry(id, "pi.assistant", []map[string]any{{
		"role":    "assistant",
		"content": []any{map[string]any{"type": "toolCall", "id": "c" + name, "name": name, "arguments": args}},
	}})}
}

// TestEditedFilesNamesEachFileOnce keeps a turn's file list to what it touched: a
// call that only reads, or one that shells out, adds nothing, and a file written
// twice is listed once.
func TestEditedFilesNamesEachFileOnce(t *testing.T) {
	steps := []Step{
		callStep(1, "write", map[string]any{"path": "a.go"}),
		callStep(2, "read", map[string]any{"path": "b.go"}),
		callStep(3, "edit", map[string]any{"file_path": "c.go"}),
		callStep(4, "write", map[string]any{"path": "a.go"}),
		callStep(5, "bash", map[string]any{"command": "sed -i '' s/a/b/ d.go"}),
		callStep(6, "patch", `{"path":"e.go"}`),
	}
	got := EditedFiles(steps)
	want := []string{"a.go", "c.go", "e.go"}
	if len(got) != len(want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("files = %v, want %v", got, want)
		}
	}
}

// TestEditedFilesIgnoresACallWithoutAFile: a write that failed before it named a
// path has nothing to contribute, and must not invent one.
func TestEditedFilesIgnoresACallWithoutAFile(t *testing.T) {
	steps := []Step{callStep(1, "write", map[string]any{"content": "x"})}
	if got := EditedFiles(steps); len(got) != 0 {
		t.Fatalf("files = %v, want none", got)
	}
}
