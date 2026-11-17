package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pitta/toolbox/internal/proto"
	"pitta/toolbox/internal/server"
)

func callTool(t *testing.T, srv *server.Server, tool string, args any, cwd string) (map[string]any, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	handler := srv.Handler(tool)
	if handler == nil {
		t.Fatalf("tool %q is not registered", tool)
	}
	value, callErr := handler(context.Background(), proto.Request{Tool: tool, Args: raw, Cwd: cwd}, func(proto.Event) {})
	if callErr != nil {
		return nil, callErr
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	return out, nil
}

func newTestServer(t *testing.T) *server.Server {
	t.Helper()
	srv := server.New(context.Background(), nil)
	RegisterRead(srv)
	RegisterWrite(srv)
	RegisterEdit(srv)
	t.Cleanup(srv.Close)
	return srv
}

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestEditReplacesUniqueText(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "a.txt", "one\ntwo\nthree\n")
	srv := newTestServer(t)
	out, err := callTool(t, srv, "edit", EditArgs{Path: path, Edits: []Edit{{OldText: "two", NewText: "TWO"}}}, dir)
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := out["text"]; got != "Successfully replaced 1 block(s) in "+path+"." {
		t.Fatalf("text = %v", got)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(content) != "one\nTWO\nthree\n" {
		t.Fatalf("content = %q", content)
	}
	diff, _ := out["diff"].(string)
	if !strings.Contains(diff, "+2 TWO") || !strings.Contains(diff, "-2 two") {
		t.Fatalf("diff = %q", diff)
	}
	if _, ok := out["firstChangedLine"]; !ok {
		t.Fatalf("firstChangedLine missing: %v", out)
	}
	patch, _ := out["patch"].(string)
	if !strings.HasPrefix(patch, "--- "+path+"\n+++ "+path+"\n") {
		t.Fatalf("patch = %q", patch)
	}
}

func TestEditErrorsMatchTheInProcessTool(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t)
	cases := []struct {
		name    string
		content string
		edits   []Edit
		want    string
	}{
		{
			name:    "not found",
			content: "alpha\n",
			edits:   []Edit{{OldText: "beta", NewText: "x"}},
			want:    "Could not find the exact text in PATH. The old text must match exactly including all whitespace and newlines.",
		},
		{
			name:    "duplicate",
			content: "dup\ndup\n",
			edits:   []Edit{{OldText: "dup", NewText: "x"}},
			want:    "Found 2 occurrences of the text in PATH. The text must be unique. Please provide more context to make it unique.",
		},
		{
			name:    "duplicate among several edits",
			content: "dup\ndup\nend\n",
			edits:   []Edit{{OldText: "dup", NewText: "x"}, {OldText: "end", NewText: "y"}},
			want:    "Found 2 occurrences of edits[0] in PATH. Each oldText must be unique. Please provide more context to make it unique.",
		},
		{
			name:    "overlap",
			content: "abcdef\n",
			edits:   []Edit{{OldText: "abc", NewText: "x"}, {OldText: "cde", NewText: "y"}},
			want:    "edits[0] and edits[1] overlap in PATH. Merge them into one edit or target disjoint regions.",
		},
		{
			name:    "empty old text",
			content: "abc\n",
			edits:   []Edit{{OldText: "", NewText: "x"}},
			want:    "oldText must not be empty in PATH.",
		},
		{
			name:    "no change",
			content: "same\n",
			edits:   []Edit{{OldText: "same", NewText: "same"}},
			want:    "No changes made to PATH. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeFixture(t, dir, "case.txt", testCase.content)
			_, err := callTool(t, srv, "edit", EditArgs{Path: path, Edits: testCase.edits}, dir)
			if err == nil {
				t.Fatalf("expected an error")
			}
			if got, want := err.Error(), strings.Replace(testCase.want, "PATH", path, 1); got != want {
				t.Fatalf("error = %q, want %q", got, want)
			}
		})
	}
}

func TestEditKeepsBomAndCrlf(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "win.txt", "\uFEFFalpha\r\nbeta\r\n")
	srv := newTestServer(t)
	if _, err := callTool(t, srv, "edit", EditArgs{Path: path, Edits: []Edit{{OldText: "beta", NewText: "gamma"}}}, dir); err != nil {
		t.Fatalf("edit: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(content) != "\uFEFFalpha\r\ngamma\r\n" {
		t.Fatalf("content = %q", content)
	}
}

func TestEditOnMissingFileReportsACode(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t)
	path := filepath.Join(dir, "absent.txt")
	_, err := callTool(t, srv, "edit", EditArgs{Path: path, Edits: []Edit{{OldText: "a", NewText: "b"}}}, dir)
	if err == nil || err.Error() != "Could not edit file: "+path+". Error code: not_found." {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteCreatesParentsAndReportsTheGivenPath(t *testing.T) {
	dir := t.TempDir()
	srv := newTestServer(t)
	out, err := callTool(t, srv, "write", WriteArgs{Path: "nested/deep/file.txt", Content: "hello"}, dir)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := out["text"]; got != "Successfully wrote to nested/deep/file.txt" {
		t.Fatalf("text = %v", got)
	}
	content, err := os.ReadFile(filepath.Join(dir, "nested/deep/file.txt"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("content = %q", content)
	}
}

func TestReadWholeFileHasNoDiagnostics(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "small.txt", "a\nb\n")
	srv := newTestServer(t)
	out, err := callTool(t, srv, "read", ReadArgs{Path: path}, dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := out["text"]; got != "a\nb\n" {
		t.Fatalf("text = %q", got)
	}
	if _, ok := out["details"]; ok {
		t.Fatalf("unexpected details: %v", out)
	}
	if _, ok := out["diagnostics"]; ok {
		t.Fatalf("unexpected diagnostics: %v", out)
	}
}

func TestReadOffsetAndLimit(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "lines.txt", "1\n2\n3\n4\n5\n")
	srv := newTestServer(t)
	offset, limit := 2.0, 2.0
	out, err := callTool(t, srv, "read", ReadArgs{Path: path, Offset: &offset, Limit: &limit}, dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := out["text"]; got != "2\n3" {
		t.Fatalf("text = %q", got)
	}
	diagnostics, _ := out["diagnostics"].([]any)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %v", out["diagnostics"])
	}
	first, _ := diagnostics[0].(map[string]any)
	if first["message"] != "3 more lines in file. Use offset=4 to continue." {
		t.Fatalf("message = %v", first["message"])
	}
}

func TestReadTruncatesLongFiles(t *testing.T) {
	dir := t.TempDir()
	var builder strings.Builder
	for i := 1; i <= 3000; i++ {
		builder.WriteString("line ")
		builder.WriteString(strings.Repeat("x", 3))
		builder.WriteString("\n")
	}
	path := writeFixture(t, dir, "big.txt", builder.String())
	srv := newTestServer(t)
	out, err := callTool(t, srv, "read", ReadArgs{Path: path}, dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	details, ok := out["details"].(map[string]any)
	if !ok {
		t.Fatalf("details missing: %v", out)
	}
	truncation, _ := details["truncation"].(map[string]any)
	if truncation["truncated"] != true || truncation["truncatedBy"] != "lines" {
		t.Fatalf("truncation = %v", truncation)
	}
	if truncation["totalLines"] != float64(3000) || truncation["outputLines"] != float64(2000) {
		t.Fatalf("truncation = %v", truncation)
	}
	diagnostics, _ := out["diagnostics"].([]any)
	first, _ := diagnostics[0].(map[string]any)
	if first["message"] != "Showing lines 1-2000 of 3001. Use offset=2001 to continue." {
		t.Fatalf("message = %v", first["message"])
	}
}

func TestReadOffsetBeyondEnd(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "short.txt", "a\nb\n")
	srv := newTestServer(t)
	offset := 9.0
	_, err := callTool(t, srv, "read", ReadArgs{Path: path, Offset: &offset}, dir)
	if err == nil || err.Error() != "Offset 9 is beyond end of file (3 lines total)" {
		t.Fatalf("err = %v", err)
	}
}

func TestReadImageRoundTripsBytes(t *testing.T) {
	dir := t.TempDir()
	png := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	png = append(png, 0, 0, 0, 13, 'I', 'H', 'D', 'R')
	png = append(png, 0, 0, 0, 2, 0, 0, 0, 3, 8, 6, 0, 0, 0)
	path := filepath.Join(dir, "tiny.png")
	if err := os.WriteFile(path, png, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	srv := newTestServer(t)
	out, err := callTool(t, srv, "read", ReadArgs{Path: path}, dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	image, ok := out["image"].(map[string]any)
	if !ok {
		t.Fatalf("image missing: %v", out)
	}
	if image["mimeType"] != "image/png" {
		t.Fatalf("mimeType = %v", image["mimeType"])
	}
	decoded, err := base64.StdEncoding.DecodeString(image["data"].(string))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(decoded) != string(png) {
		t.Fatalf("bytes changed: %v", decoded)
	}
}

func TestReadDirectoryAndWriteUnderAFileUseNodeText(t *testing.T) {
	dir := t.TempDir()
	srv := server.New(context.Background(), discardLogger())
	RegisterRead(srv)
	RegisterWrite(srv)
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	// Node's read error carries no path, because the open succeeds and the read is what fails.
	if _, err := callTool(t, srv, "read", map[string]any{"path": "folder"}, dir); err == nil ||
		err.Error() != "EISDIR: illegal operation on a directory, read" {
		t.Fatalf("read a directory: %v", err)
	}
	// Node's mkdir reports the target through realpath, which names the file it could not create.
	_, err := callTool(t, srv, "write", map[string]any{"path": "blocker/deeper/file.txt", "content": "hi"}, dir)
	want := "ENOTDIR: not a directory, realpath '" + filepath.Join(dir, "blocker/deeper/file.txt") + "'"
	if err == nil || err.Error() != want {
		t.Fatalf("write under a file: %v, want %s", err, want)
	}
}

// A large file must not be loaded to answer a read at its start.
func TestReadLargeFileStaysBounded(t *testing.T) {
	dir := t.TempDir()
	srv := server.New(context.Background(), discardLogger())
	RegisterRead(srv)
	line := strings.Repeat("x", 99) + "\n"
	path := filepath.Join(dir, "big.txt")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for index := 0; index < 200_000; index++ {
		if _, err := file.WriteString(line); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	file.Close()
	result, err := callTool(t, srv, "read", map[string]any{"path": "big.txt"}, dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	truncation, ok := result["details"].(map[string]any)["truncation"].(map[string]any)
	if !ok {
		t.Fatalf("details = %v, want truncation", result["details"])
	}
	// 100-byte lines hit the byte budget first, so the answer is 512 whole lines of the 200000.
	if truncation["totalLines"] != float64(200_000) || truncation["outputLines"] != float64(512) ||
		truncation["truncatedBy"] != "bytes" {
		t.Fatalf("truncation = %v", truncation)
	}
	text, _ := result["text"].(string)
	if len(text) != 51_199 {
		t.Fatalf("kept %d bytes, want 51199", len(text))
	}
}

// discardLogger keeps the per-call log lines out of the test output.
func discardLogger() *log.Logger { return log.New(io.Discard, "", 0) }

// A selection that starts with an empty line used to lose that line's separator, which shifted the
// whole text. The reference keeps it, so a blank first line is visible to the model.
func TestReadKeepsBlankLinesAtTheStartOfTheSelection(t *testing.T) {
	dir := t.TempDir()
	srv := server.New(context.Background(), discardLogger())
	RegisterRead(srv)
	cases := []struct {
		name    string
		content string
		args    map[string]any
		want    string
	}{
		{"blank then text", "\nhello\n", map[string]any{}, "\nhello\n"},
		{"two blanks then text", "\n\nhello\n", map[string]any{}, "\n\nhello\n"},
		{"only a newline", "\n", map[string]any{}, "\n"},
		{"window opens on a blank", "hello\n\nworld\n", map[string]any{"offset": 2}, "\nworld\n"},
		{"limit ends before the blank", "\nhello\nworld\n", map[string]any{"limit": 2}, "\nhello"},
		{"blank first line then truncation", "\n" + strings.Repeat("line\n", 2001), map[string]any{}, "\nline\nline"},
	}
	for index, testCase := range cases {
		path := filepath.Join(dir, fmt.Sprintf("blank-%d.txt", index))
		if err := os.WriteFile(path, []byte(testCase.content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		result, err := callTool(t, srv, "read", mergePath(map[string]any{"path": filepath.Base(path)}, testCase.args), dir)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		text, _ := result["text"].(string)
		if testCase.name == "blank first line then truncation" {
			if !strings.HasPrefix(text, testCase.want) {
				t.Fatalf("%s: text starts %q, want %q", testCase.name, text[:12], testCase.want)
			}
			continue
		}
		if text != testCase.want {
			t.Fatalf("%s: text = %q, want %q", testCase.name, text, testCase.want)
		}
	}
}

func mergePath(base, extra map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}
