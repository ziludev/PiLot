package side

import (
	"fmt"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/protocol"
)

func english(t *testing.T) {
	t.Helper()
	old := i18n.Current()
	i18n.Set(i18n.EN)
	t.Cleanup(func() { i18n.Set(old) })
}

func testChanges() *protocol.Changes {
	return &protocol.Changes{
		Repo:   true,
		Branch: "agent/work",
		Files: []protocol.FileChange{
			{Path: "main.go", Status: "modified", Added: 12, Removed: 4},
			{Path: "internal/app/composer.go", Status: "added", Added: 30, Removed: 0},
			{Path: "old.txt", Status: "deleted", Added: 0, Removed: 60},
		},
	}
}

func testCommits() *protocol.Git {
	return &protocol.Git{
		Worktree:   "/tmp/work",
		Branch:     "agent/work",
		Base:       "main",
		Remote:     protocol.Remote{Kind: "github", URL: "git@github.com:a/b.git"},
		CompareURL: "https://github.com/a/b/compare/main...agent/work",
		Commits: []protocol.Commit{
			{Hash: "aaa111122223333", Short: "aaa1111", Date: "2026-05-01T10:00:00Z", Subject: "Add the side panel", Files: 3, Added: 20, Removed: 2},
			{Hash: "bbb222233334444", Short: "bbb2222", Date: "2026-05-02T10:00:00Z", Subject: "Fix the file viewer", Files: 1, Added: 4, Removed: 1},
			{Hash: "ccc333344445555", Short: "ccc3333", Date: "2026-05-03T10:00:00Z", Subject: "Send review notes", Files: 2, Added: 9, Removed: 0},
		},
	}
}

func testFile() *protocol.FilePayload {
	var b strings.Builder
	for i := 1; i <= 200; i++ {
		fmt.Fprintf(&b, "line %03d of the file\n", i)
	}
	return &protocol.FilePayload{Path: "main.go", Content: b.String(), Lines: 200, Size: 4096}
}

// savePNG writes the frame to /tmp/pitta-side.png for a look at the panel.
func savePNG(t *testing.T, tt *ui.Tester) {
	t.Helper()
	f, err := os.Create("/tmp/pitta-side.png")
	if err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
}

func TestChangesViewShowsFilesAndOpensOne(t *testing.T) {
	english(t)
	props := ChangesProps{
		Changes: testChanges(),
		Review:  []ReviewNote{{Path: "main.go", Line: "3", Note: "rename this"}},
	}
	opened := ""
	props.OnOpenFile = func(path string) { opened = path }
	props.OnCommit = func() {}
	props.OnMerge = func() {}
	props.OnReload = func() {}

	tt := ui.NewTester(func(c *ui.Context) { ChangesView(c, props) }, 460, 620)
	tt.Frame()
	for _, want := range []string{"3 files · agent/work", "main.go", "old.txt", "+12", "−4", "rename this", "Commit", "Merge"} {
		if !tt.HasText(want) {
			t.Fatalf("changes panel is missing %q, shows %q", want, tt.Texts())
		}
	}
	if err := tt.Click("main.go"); err != nil {
		t.Fatalf("click main.go: %v", err)
	}
	if opened != "main.go" {
		t.Fatalf("open = %q, want main.go", opened)
	}
	savePNG(t, tt)
}

func TestFileViewNumbersLinesAndComments(t *testing.T) {
	english(t)
	commented := ""
	props := FileProps{
		Path:      "main.go",
		File:      testFile(),
		OnClose:   func() {},
		OnComment: func(line string) { commented = line },
	}
	tt := ui.NewTester(func(c *ui.Context) { FileView(c, props) }, 520, 420)
	tt.Frame()
	for _, want := range []string{"main.go", "200 lines", "4.0 KB", "line 007 of the file"} {
		if !tt.HasText(want) {
			t.Fatalf("file view is missing %q, shows %q", want, tt.Texts())
		}
	}
	if err := tt.Click("line 007 of the file"); err != nil {
		t.Fatalf("click a line: %v", err)
	}
	if commented != "line 007 of the file" {
		t.Fatalf("commented line = %q", commented)
	}
}

func TestCommitDialogSubmitsAndCancels(t *testing.T) {
	english(t)
	message := "seed"
	submitted, cancelled := 0, 0
	props := CommitProps{
		Message:   message,
		Open:      true,
		OnMessage: func(m string) { message = m },
		OnSubmit:  func() { submitted++ },
		OnCancel:  func() { cancelled++ },
	}
	tt := ui.NewTester(func(c *ui.Context) { CommitDialog(c, props) }, 520, 400)
	tt.Frame()
	for _, want := range []string{"Commit", "Cancel"} {
		if !tt.HasText(want) {
			t.Fatalf("commit dialog is missing %q, shows %q", want, tt.Texts())
		}
	}
	tt.Type("!")
	tt.Key(0, ui.KeyEnter)
	if submitted != 1 {
		t.Fatalf("submit = %d, want 1", submitted)
	}
	if !strings.HasSuffix(message, "!") {
		t.Fatalf("message = %q, want it to keep the typed text", message)
	}
	if cancelled != 0 {
		t.Fatalf("cancel = %d after submit", cancelled)
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatalf("click Cancel: %v", err)
	}
	if cancelled != 1 {
		t.Fatalf("cancel = %d, want 1", cancelled)
	}
}

func TestCommitsViewOpensAndClosesAPatch(t *testing.T) {
	english(t)
	expanded := ""
	opened, closed := 0, 0
	props := CommitsProps{Git: testCommits()}
	props.OnOpen = func(hash string) { expanded, opened = hash, opened+1 }
	props.OnClose = func() { expanded, closed = "", closed+1 }
	props.OnPush = func(dryRun bool) {}
	patch := "--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-old\n+new\n"
	props.Diff = patch

	tt := ui.NewTester(func(c *ui.Context) {
		props.Expanded = expanded
		CommitsView(c, props)
	}, 520, 620)
	tt.Frame()
	// The base is a select now, so the arrow is its own text node.
	for _, want := range []string{"agent/work", "→", "main", "Add the side panel", "aaa1111", "Open PR"} {
		if !tt.HasText(want) {
			t.Fatalf("commits view is missing %q, shows %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Add the side panel"); err != nil {
		t.Fatalf("click a commit: %v", err)
	}
	if opened != 1 || expanded != "aaa111122223333" {
		t.Fatalf("opened = %d, expanded = %q", opened, expanded)
	}
	tt.Advance(600 * time.Millisecond)
	tt.Frame()
	if !tt.HasText("-old") || !tt.HasText("+new") {
		t.Fatalf("the open commit shows no patch: %q", tt.Texts())
	}
	if err := tt.Click("Add the side panel"); err != nil {
		t.Fatalf("click the open commit: %v", err)
	}
	if closed != 1 || expanded != "" {
		t.Fatalf("closed = %d, opened = %d, expanded = %q", closed, opened, expanded)
	}
}

func TestMergeButtonShowsOutcomes(t *testing.T) {
	english(t)
	merged := 0
	tt := ui.NewTester(func(c *ui.Context) {
		MergeButton(c, &protocol.MergeResult{Status: "conflict", Conflicts: []string{"main.go"}}, func() { merged++ })
	}, 420, 200)
	tt.Frame()
	for _, want := range []string{"Merge", "main.go", "Ask the agent"} {
		if !tt.HasText(want) {
			t.Fatalf("merge button is missing %q, shows %q", want, tt.Texts())
		}
	}
	if err := tt.Click("Merge"); err != nil {
		t.Fatalf("click Merge: %v", err)
	}
	if merged != 1 {
		t.Fatalf("merge = %d, want 1", merged)
	}
}
