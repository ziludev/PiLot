// Package diff parses unified diffs into rows a transcript tool step or a
// side panel can draw, and draws them.
package diff

import (
	"regexp"
	"strconv"
	"strings"
)

// Kind is what a line of a diff is.
type Kind int

const (
	// Context is a line both sides share, text as in the patch, one leading
	// space included.
	Context Kind = iota
	// Add is a line only the new side has, one leading "+" included.
	Add
	// Del is a line only the old side has, one leading "-" included.
	Del
	// Meta is a line about the patch itself: "\ No newline at end of file".
	Meta
	// kindHunk is a hunk header row, which Render draws and Parse keeps in
	// Hunk.Header instead: no Line Parse returns has it.
	kindHunk
)

// File statuses, as Parse reports them.
const (
	StatusModified = "modified"
	StatusAdded    = "added"
	StatusDeleted  = "deleted"
	StatusRenamed  = "renamed"
)

// File is one file of a patch, its hunks in patch order.
type File struct {
	// Path is the new path, without the a/ or b/ prefix; empty for a patch
	// fragment with no file header. A deleted file keeps its old path.
	// Status is one of the Status constants, StatusModified when the patch
	// says nothing more.
	Path    string
	Status  string
	Added   int
	Removed int
	Hunks   []Hunk
	// Meta holds the lines that change no content, as they appear in the
	// patch: mode, rename, similarity and binary notices.
	Meta []string
}

// Hunk is one @@ section. The header itself is in Header, not in Lines.
type Hunk struct {
	Header string
	Lines  []Line
}

// Line is one line of a hunk. Text keeps the patch prefix, so an added line
// reads "+func main() {".
type Line struct {
	Kind  Kind
	Text  string
	OldNo int // 0 outside the old side: an added line has none
	NewNo int // 0 outside the new side: a deleted line has none
}

// hunkRe reads "@@ -5,3 +5,4 @@ context". The counts default to one, as git
// writes them when it leaves the count out.
var hunkRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

// Parse reads a unified diff. It takes a full "git diff" (diff --git, mode
// and index lines, then hunks), a plain unified patch ("--- /dev/null" and
// "+++ b/path" then hunks), or a bare fragment that starts at a hunk, as tool
// output often is. It never fails: a line it does not know becomes meta.
func Parse(patch string) []File {
	var (
		files []File
		cur   *File

		// Hunk counts are tracked to know where a hunk ends, so a "---" line
		// inside one is a deletion of "--", never a file header.
		inHunk         bool
		remOld, remNew int
		oldNo, newNo   int

		// oldNull and newNull remember a /dev/null side, gitHeaders a file
		// announced by "diff --git" whose --- and +++ are still to come.
		oldNull, newNull bool
		gitHeaders       bool
	)

	start := func() *File {
		if cur != nil && (len(cur.Hunks) > 0 || len(cur.Meta) > 0) {
			files = append(files, *cur)
		}
		cur = &File{Status: StatusModified}
		oldNull, newNull, gitHeaders = false, false, false
		return cur
	}
	current := func() *File {
		if cur == nil {
			return start()
		}
		return cur
	}

	for _, raw := range strings.Split(patch, "\n") {
		s := strings.TrimSuffix(raw, "\r")

		// A marker about the line before it, which may be the last line of a
		// hunk that has just used up its counts.
		if strings.HasPrefix(s, `\`) {
			f := current()
			if n := len(f.Hunks); n > 0 {
				f.Hunks[n-1].Lines = append(f.Hunks[n-1].Lines, Line{Kind: Meta, Text: s})
			} else {
				f.Meta = append(f.Meta, s)
			}
			continue
		}

		if inHunk {
			consumed := true
			last := len(cur.Hunks) - 1
			switch {
			case s == "":
				// A hunk cannot hold a blank line: the patch was cut short.
				inHunk = false
				consumed = false
			case strings.HasPrefix(s, "+"):
				cur.Hunks[last].Lines = append(cur.Hunks[last].Lines, Line{Kind: Add, Text: s, NewNo: newNo})
				newNo, remNew, cur.Added = newNo+1, remNew-1, cur.Added+1
			case strings.HasPrefix(s, "-"):
				cur.Hunks[last].Lines = append(cur.Hunks[last].Lines, Line{Kind: Del, Text: s, OldNo: oldNo})
				oldNo, remOld, cur.Removed = oldNo+1, remOld-1, cur.Removed+1
			case strings.HasPrefix(s, " "):
				cur.Hunks[last].Lines = append(cur.Hunks[last].Lines, Line{Kind: Context, Text: s, OldNo: oldNo, NewNo: newNo})
				oldNo, newNo, remOld, remNew = oldNo+1, newNo+1, remOld-1, remNew-1
			default:
				// The counts ran out early: treat the line as a header.
				inHunk = false
				consumed = false
			}
			if consumed {
				if remOld <= 0 && remNew <= 0 {
					inHunk = false
				}
				continue
			}
		}

		switch {
		case strings.HasPrefix(s, "diff --git "):
			f := start()
			f.Path = cleanPath(lastToken(strings.TrimPrefix(s, "diff --git ")))
			gitHeaders = true
		case s == "---" || strings.HasPrefix(s, "--- "):
			old := cleanPath(headerPath(s))
			oldNull = old == ""
			if !gitHeaders {
				start().Path = old
			}
		case s == "+++" || strings.HasPrefix(s, "+++ "):
			f := current()
			gitHeaders = false
			if p := cleanPath(headerPath(s)); p != "" {
				f.Path = p
				newNull = false
			} else {
				newNull = true
			}
			switch {
			case oldNull && !newNull:
				f.Status = StatusAdded
			case newNull && !oldNull:
				f.Status = StatusDeleted
			}
		case strings.HasPrefix(s, "index "):
			// The blob hashes tell a reader nothing the header does not.
		case strings.HasPrefix(s, "@@"):
			f := current()
			m := hunkRe.FindStringSubmatch(s)
			if m == nil {
				f.Meta = append(f.Meta, s)
				continue
			}
			oldStart, oldCount := atoiOr(m[1], 0), atoiOr(m[2], 1)
			newStart, newCount := atoiOr(m[3], 0), atoiOr(m[4], 1)
			f.Hunks = append(f.Hunks, Hunk{Header: s})
			oldNo, newNo = oldStart, newStart
			remOld, remNew = oldCount, newCount
			gitHeaders = false
			inHunk = remOld > 0 || remNew > 0
		case s == "":
		case strings.HasPrefix(s, "new file mode"):
			f := current()
			f.Status = StatusAdded
			f.Meta = append(f.Meta, s)
		case strings.HasPrefix(s, "deleted file mode"):
			f := current()
			f.Status = StatusDeleted
			f.Meta = append(f.Meta, s)
		case strings.HasPrefix(s, "rename from "):
			f := current()
			f.Status = StatusRenamed
			f.Meta = append(f.Meta, s)
		case strings.HasPrefix(s, "rename to "):
			f := current()
			f.Status = StatusRenamed
			if p := cleanPath(strings.TrimPrefix(s, "rename to ")); p != "" {
				f.Path = p
			}
			f.Meta = append(f.Meta, s)
		default:
			f := current()
			f.Meta = append(f.Meta, s)
		}
	}
	if cur != nil && (len(cur.Hunks) > 0 || len(cur.Meta) > 0) {
		files = append(files, *cur)
	}
	return files
}

// Stats totals the files: lines added, lines removed, and how many files.
func Stats(files []File) (added, removed, paths int) {
	for _, f := range files {
		added += f.Added
		removed += f.Removed
	}
	return added, removed, len(files)
}

// headerPath is what follows the "---" or "+++" of a file header.
func headerPath(s string) string { return strings.TrimLeft(s[3:], " ") }

// cleanPath drops the a/ or b/ prefix, a timestamp, quotes and /dev/null
// from a path as the patch writes it.
func cleanPath(p string) string {
	if i := strings.IndexByte(p, '\t'); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimSpace(p)
	if p == "" || p == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(p, `"`) && strings.HasSuffix(p, `"`) && len(p) > 1 {
		p = p[1 : len(p)-1]
	}
	if len(p) > 2 && (p[0] == 'a' || p[0] == 'b') && p[1] == '/' {
		p = p[2:]
	}
	return p
}

func lastToken(s string) string {
	if i := strings.LastIndexByte(s, ' '); i >= 0 {
		return s[i+1:]
	}
	return s
}

func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
