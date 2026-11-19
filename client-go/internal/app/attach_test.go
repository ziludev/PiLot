package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pitta/internal/i18n"

	"github.com/egoist/mygo/ui"
)

// TestDroppedFileBecomesItsPath: the window cannot quote a pdf, but the agent
// runs on this machine and can read it — so the drop leaves the path in the
// message instead of an apology. The reader no longer copies paths by hand.
func TestDroppedFileBecomesItsPath(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	path := filepath.Join(t.TempDir(), "report.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.putPaths([]string{path})
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	if !strings.Contains(a.draft, path) {
		t.Fatalf("draft = %q, want the dropped path in it", a.draft)
	}
	if !a.noticeOK {
		t.Fatalf("notice about the path reads as a failure: %q", a.notice)
	}
}

// TestDroppedPathWithASpaceStaysOneWord: a path with a space in it would read as
// two paths to whoever gets the message next.
func TestDroppedPathWithASpaceStaysOneWord(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	big := filepath.Join(t.TempDir(), "big notes.txt")
	if err := os.WriteFile(big, make([]byte, maxInline+1), 0o644); err != nil {
		t.Fatal(err)
	}
	a.putPaths([]string{big})
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	if !strings.Contains(a.draft, "`"+big+"`") {
		t.Fatalf("draft = %q, want the path in backticks", a.draft)
	}
}

// TestTextFileIsStillQuoted: the fallback must not swallow the case that works —
// a source file is worth reading as text, not as a path to go and read.
func TestTextFileIsStillQuoted(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	path := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.attachPath(path)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	if !strings.Contains(a.draft, "package main") || !strings.Contains(a.draft, "```") {
		t.Fatalf("draft = %q, want the file quoted", a.draft)
	}
}
