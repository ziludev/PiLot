package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pilot/internal/i18n"

	"github.com/egoist/mygo/ui"
)

// TestGoToFileRanksTheNameYouTyped: the basename is what the reader types, so it
// has to win over a hit spread across the directories above it.
func TestGoToFileRanksTheNameYouTyped(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.palette.mode = paletteFiles
	a.palette.query = "pal"
	a.repoFiles = repoFiles{
		root: "/repo",
		all: []string{
			"client-go/internal/app/palette.go",
			"docs/policies/plain-english.md",
			"README.md",
		},
	}

	rows := a.paletteRows()
	if len(rows) == 0 {
		t.Fatal("no rows for pal")
	}
	if rows[0].label != "palette.go" {
		t.Fatalf("first row = %q, want palette.go", rows[0].label)
	}
	for _, row := range rows {
		if row.label == "README.md" {
			t.Fatalf("README.md matched pal")
		}
	}
}

// TestGoToFileWritesThePathIntoTheMessage: choosing a file is only worth a key
// if it saves the copy — so ⏎ puts the absolute path in the composer.
func TestGoToFileWritesThePathIntoTheMessage(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.palette.mode = paletteFiles
	a.palette.query = "pal"
	a.repoFiles = repoFiles{root: "/repo", all: []string{"internal/app/palette.go"}}

	rows := a.paletteRows()
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	rows[0].run()
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	if !strings.Contains(a.draft, "/repo/internal/app/palette.go") {
		t.Fatalf("draft = %q, want the file's path", a.draft)
	}
	if !a.noticeOK {
		t.Fatalf("notice about the path reads as a failure: %q", a.notice)
	}
}

// TestGoToFileSaysWhyTheListIsEmpty: still reading, failed, and nothing matched
// are three different sentences — a blank dialog would be a mystery.
func TestGoToFileSaysWhyTheListIsEmpty(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.palette.mode = paletteFiles

	a.repoFiles.busy = true
	if got := a.filesEmptyLabel(); got != i18n.T("filesBusy") {
		t.Fatalf("busy label = %q", got)
	}
	a.repoFiles.busy = false
	a.palette.query = ""
	if got := a.filesEmptyLabel(); got != i18n.T("filesPrompt") {
		t.Fatalf("empty query label = %q", got)
	}
	a.palette.query = "zzz"
	if got := a.filesEmptyLabel(); got != i18n.T("filesEmpty") {
		t.Fatalf("no-match label = %q", got)
	}
}

// TestGoToFileReadsTheCheckout: what the palette lists is what git says is here —
// tracked and untracked-but-not-ignored, and nothing from the ignored trees.
func TestGoToFileReadsTheCheckout(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("secret/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.go", "docs/plan.md", "secret/key.txt"} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := repoFilesAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(files, " ")
	if !strings.Contains(got, "main.go") || !strings.Contains(got, "docs/plan.md") {
		t.Fatalf("files = %v, want the two real ones", files)
	}
	if strings.Contains(got, "secret/") {
		t.Fatalf("files = %v, ignored file listed", files)
	}
}

// TestGoToFileOpensOnTheKey: ⌘P arms the file list with an empty query.
func TestGoToFileOpensOnTheKey(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	if a.palette.open {
		t.Fatal("palette open at rest")
	}
	a.palette.query = "left over"
	a.openFiles()
	if !a.palette.open || a.palette.mode != paletteFiles {
		t.Fatalf("palette = %+v, want the file list open", a.palette)
	}
	if a.palette.query != "" {
		t.Fatalf("query = %q, want it cleared", a.palette.query)
	}
	if !a.repoFiles.busy {
		t.Fatal("file list is not marked as loading")
	}
}
