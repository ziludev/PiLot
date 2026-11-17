package turns

import "encoding/json"

// fileTools maps a call that writes a file to the argument that names it. A
// turn's "2 files" count comes from the engine; this is what says which two.
var fileTools = map[string][]string{
	"write":         {"path", "file_path"},
	"write_file":    {"path", "file_path"},
	"edit":          {"path", "file_path"},
	"edit_file":     {"path", "file_path"},
	"multi_edit":    {"path", "file_path"},
	"patch":         {"path", "file_path"},
	"patch_file":    {"path", "file_path"},
	"apply_patch":   {"path", "file_path"},
	"notebook_edit": {"notebook_path", "path"},
}

// EditedFiles names the files a turn's calls wrote to, in the order they were
// first touched. A call whose arguments do not name a file — a failed call, a
// patch carried inline — contributes nothing rather than a guess.
func EditedFiles(steps []Step) []string {
	seen := make(map[string]bool)
	var files []string
	for _, step := range steps {
		name, _ := ToolCall(step.Entry)
		keys, ok := fileTools[name]
		if !ok {
			continue
		}
		args := decodeargs(ToolCallArgs(step.Entry))
		if args == nil {
			continue
		}
		for _, key := range keys {
			file, ok := args[key].(string)
			if !ok || file == "" || seen[file] {
				continue
			}
			seen[file] = true
			files = append(files, file)
		}
	}
	return files
}

// decodeargs reads a call's arguments, which the bridge normally writes as an
// object and sometimes as the string of one.
func decodeargs(raw string) map[string]any {
	if raw == "" {
		return nil
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return nil
	}
	return args
}
