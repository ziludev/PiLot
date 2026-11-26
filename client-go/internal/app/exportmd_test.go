package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pilot/internal/i18n"

	"github.com/egoist/mygo/ui"
)

// TestChatMarkdownKeepsTheWordsAndFoldsTheWork: the file is for someone who was
// not in the window, so the asking and the answer stay in the open and the tool
// calls fold away behind a summary.
func TestChatMarkdownKeepsTheWordsAndFoldsTheWork(t *testing.T) {
	i18n.Set(i18n.ZH)
	at := time.Date(2026, 11, 20, 0, 30, 0, 0, time.Local)
	rows := []threadRow{
		{kind: rowUser, text: "why is the sky blue?"},
		{kind: rowSteps, steps: []step{
			{label: "bash", body: "git status"},
			{label: "read", body: "a.go"},
		}},
		{kind: rowAssistant, text: "Because air scatters blue light."},
		{kind: rowUser, text: "and `code` in a line?"},
	}

	md := chatMarkdown("Why is the sky blue", rows, at)

	for _, want := range []string{
		"# Why is the sky blue",
		i18n.T("exportYou"),
		"why is the sky blue?",
		"PiLot",
		"Because air scatters blue light.",
		"<details><summary>",
		"1. **bash** `git status`",
		"2. **read** `a.go`",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("export is missing %q:\n%s", want, md)
		}
	}
	if !strings.Contains(md, "2026-11-20 00:30") {
		t.Fatalf("export does not say when it was taken:\n%s", md)
	}
}

// TestExportNameIsAFileName: the title is the name, the time keeps two exports
// apart, and nothing that a file system would refuse gets through.
func TestExportNameIsAFileName(t *testing.T) {
	at := time.Date(2026, 11, 20, 0, 30, 0, 0, time.Local)
	cases := []struct{ title, want string }{
		{"Why is the sky blue", "PiLot-Why-is-the-sky-blue-20261120-0030.md"},
		{"修复/上线 前的检查", "PiLot-修复-上线-前的检查-20261120-0030.md"},
		{"", "PiLot-chat-20261120-0030.md"},
		{"////", "PiLot-chat-20261120-0030.md"},
	}
	for _, c := range cases {
		if got := exportName(c.title, at); got != c.want {
			t.Fatalf("exportName(%q) = %q, want %q", c.title, got, c.want)
		}
	}
	// A title longer than the room for it is cut, not passed on.
	got := exportName(strings.Repeat("长", 80), at)
	if n := strings.Count(got, "长"); n > 40 {
		t.Fatalf("long title kept %d runes of it: %q", n, got)
	}
}

// TestWriteExportLeavesAFile: the path it hands back is the one it wrote.
func TestWriteExportLeavesAFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Downloads")
	path, err := writeExport(dir, "a.md", "# hi\n")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "a.md") {
		t.Fatalf("wrote %q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "# hi\n" {
		t.Fatalf("read back %q, %v", body, err)
	}
}

// TestExportMarkdownPutsTheChatOnDisk: the verb draws from the transcript, so a
// chat with two turns and a tool call in it becomes a file with both.
func TestExportMarkdownPutsTheChatOnDisk(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.exportTo(t.TempDir())
	// The word about the file goes through the inbox, so it arrives with the
	// next frame rather than in the call.
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	if !a.noticeOK || a.notice == "" {
		t.Fatalf("no word about the export: ok=%v notice=%q", a.noticeOK, a.notice)
	}
	if !strings.Contains(a.notice, "PiLot-") {
		t.Fatalf("notice does not name the file: %q", a.notice)
	}
}
