package app

// Finding a file in the checkout.
//
// The path to a file is the one thing this window cannot put on the clipboard
// for the reader: the agent works in a checkout of its own, and the file an
// instruction is about is easiest to name by typing three letters of it. So ⌘P
// lists what git says is in the checkout and puts the chosen path in the
// composer, where the agent can read it.
//
// The list comes from `git ls-files`: it is what the agent itself would see
// (tracked plus untracked-but-not-ignored), it is sorted, and it skips the
// build trees a directory walk would drown in.

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"pitta/internal/i18n"
)

// repoFileMax is how many paths a walk is allowed to collect when git cannot
// answer. A checkout that is not a repository still has the files the reader
// means, but an accidental crawl of a home directory must stop somewhere.
const repoFileMax = 4000

// repoFiles is the checkout's file list, and where it came from. It is fetched
// once per opening: git answers in milliseconds, and a cached list would be
// lying the moment the agent creates a file.
type repoFiles struct {
	seq  int
	root string
	all  []string
	busy bool
	err  string
}

// openFiles arms the palette with the checkout's files. Like every opening it
// starts from an empty query, and the list arrives in the background — the
// dialog should not wait on a round trip to the bridge to show a field.
func (a *App) openFiles() {
	a.palette.open = true
	a.palette.mode = paletteFiles
	a.palette.query = ""
	a.palette.index = 0
	a.repoFiles.busy = true
	a.repoFiles.err = ""
	a.invalidate()
	a.loadRepoFiles()
}

// loadRepoFiles asks for the checkout's files on a goroutine: worktreePath may
// have to read the bridge first, and that read must not hold up the frame.
func (a *App) loadRepoFiles() {
	a.repoFiles.seq++
	seq := a.repoFiles.seq
	go func() {
		root, files, err := a.readRepoFiles()
		a.post(event{kind: evRepoFiles, seq: seq, files: files, path: root, err: err})
	}()
}

// applyRepoFiles takes the list that came back, unless a newer opening has
// already asked for another one.
func (a *App) applyRepoFiles(seq int, root string, files []string, err error) {
	if seq != a.repoFiles.seq {
		return
	}
	a.repoFiles.busy = false
	if err != nil {
		a.repoFiles.err = err.Error()
		a.repoFiles.all = nil
		a.invalidate()
		return
	}
	a.repoFiles.root = root
	a.repoFiles.all = files
	a.repoFiles.err = ""
	a.invalidate()
}

// readRepoFiles is the checkout's paths, relative to its root.
func (a *App) readRepoFiles() (string, []string, error) {
	root, err := a.worktreePath()
	if err != nil {
		return "", nil, err
	}
	files, err := repoFilesAt(root)
	return root, files, err
}

// repoFilesAt lists one checkout, without going through the bridge: the answer
// depends only on what is on disk, which is also what makes it testable.
func repoFilesAt(root string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "ls-files", "-z",
		"--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		// A checkout that is not a repository is not an error the reader can act
		// on; walking it is what the palette would have done anyway.
		return walkFiles(root), nil
	}
	parts := strings.Split(string(out), "\x00")
	files := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			files = append(files, part)
		}
	}
	if len(files) == 0 {
		return nil, errors.New(i18n.T("filesEmpty"))
	}
	sort.Strings(files)
	return files, nil
}

// walkFiles is the fallback list: every file under root, minus the trees that
// are never what an instruction is about.
func walkFiles(root string) []string {
	files := make([]string, 0, 256)
	var walk func(dir, rel string, depth int)
	walk = func(dir, rel string, depth int) {
		if len(files) >= repoFileMax || depth > 8 {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || skipTree(name) {
				continue
			}
			child := filepath.Join(dir, name)
			relChild := name
			if rel != "" {
				relChild = rel + "/" + name
			}
			if entry.IsDir() {
				walk(child, relChild, depth+1)
				continue
			}
			files = append(files, relChild)
			if len(files) >= repoFileMax {
				return
			}
		}
	}
	walk(root, "", 0)
	return files
}

// skipTree names the directories a walk never enters: dependencies, build
// output, the window's own worktrees. git already skips them; this is only for
// the checkout that is not a repository.
func skipTree(name string) bool {
	switch name {
	case "node_modules", "dist", "build", "target", "vendor", "__pycache__",
		".next", ".venv", "Pods", "DerivedData":
		return true
	}
	return false
}

// repoFileRows ranks the checkout's files against the query. The basename is
// what the reader is typing, so a hit in it beats a hit spread across the
// directories above it.
func (a *App) repoFileRows() []paletteRow {
	root := a.repoFiles.root
	files := a.repoFiles.all
	query := strings.TrimSpace(a.palette.query)
	rows := make([]paletteRow, 0, paletteMaxRows*2)
	for _, rel := range files {
		score := 0
		if query != "" {
			base := rel
			dir := ""
			if at := strings.LastIndex(rel, "/"); at >= 0 {
				base, dir = rel[at+1:], rel[:at]
			}
			inBase, okBase := paletteMatch(query, base)
			inPath, okPath := paletteMatch(query, dir+"/"+base)
			switch {
			case okBase:
				score = inBase + 60
			case okPath:
				score = inPath
			default:
				continue
			}
		}
		dir := filepath.Dir(rel)
		if dir == "." {
			dir = "/"
		}
		rows = append(rows, paletteRow{
			label:  filepath.Base(rel),
			detail: dir,
			slug:   "file." + rel,
			score:  score,
			run:    func() { a.putPaths([]string{filepath.Join(root, rel)}) },
		})
	}
	if query != "" {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].score > rows[j].score })
	}
	if len(rows) > paletteMaxRows {
		rows = rows[:paletteMaxRows]
	}
	return rows
}

// filesEmptyLabel says why the list is empty: still reading, failed, or nothing
// matched what was typed.
func (a *App) filesEmptyLabel() string {
	switch {
	case a.repoFiles.busy:
		return i18n.T("filesBusy")
	case a.repoFiles.err != "":
		return a.repoFiles.err
	case strings.TrimSpace(a.palette.query) == "":
		return i18n.T("filesPrompt")
	}
	return i18n.T("filesEmpty")
}

// filesHintText is the keyboard line under the list, which changes with the
// verb: here ⏎ writes a path instead of opening a chat.
func (a *App) filesHintText() string {
	return i18n.T("filesHint")
}
