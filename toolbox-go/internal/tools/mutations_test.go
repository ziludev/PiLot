package tools

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFileMutationSerializesOnePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.txt")
	if err := os.WriteFile(path, []byte("start\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var running, most int32
	var applied int32
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			err := withFileMutation(path, func() error {
				now := atomic.AddInt32(&running, 1)
				if now > atomic.LoadInt32(&most) {
					atomic.StoreInt32(&most, now)
				}
				time.Sleep(5 * time.Millisecond) // long enough for a second call to overlap
				atomic.AddInt32(&applied, 1)
				atomic.AddInt32(&running, -1)
				return nil
			})
			if err != nil {
				t.Errorf("mutation: %v", err)
			}
		}()
	}
	wait.Wait()
	if most != 1 {
		t.Fatalf("one file ran %d mutations at once", most)
	}
	if applied != 8 {
		t.Fatalf("applied = %d, want 8", applied)
	}
}

func TestFileMutationDoesNotSerializeDifferentPaths(t *testing.T) {
	dir := t.TempDir()
	var running, most int32
	var wait sync.WaitGroup
	for index := range 4 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			path := filepath.Join(dir, "file-"+string(rune('a'+index))+".txt")
			_ = withFileMutation(path, func() error {
				now := atomic.AddInt32(&running, 1)
				if now > atomic.LoadInt32(&most) {
					atomic.StoreInt32(&most, now)
				}
				time.Sleep(20 * time.Millisecond)
				atomic.AddInt32(&running, -1)
				return nil
			})
		}()
	}
	wait.Wait()
	if most < 2 {
		t.Fatalf("different files ran one at a time (most=%d)", most)
	}
}

func TestConcurrentEditsOnOneFileAllApply(t *testing.T) {
	dir := t.TempDir()
	path := writeFixture(t, dir, "count.txt", "line\n")
	srv := newTestServer(t)
	var wait sync.WaitGroup
	for index := range 6 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			// Each call appends its own marker: without the queue, read-modify-write would drop some.
			_, err := callTool(t, srv, "edit", EditArgs{
				Path:  path,
				Edits: []Edit{{OldText: "line\n", NewText: "line\n" + string(rune('a'+index))}},
			}, dir)
			if err != nil {
				t.Errorf("edit %d: %v", index, err)
			}
		}()
	}
	wait.Wait()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	// Each call inserted its marker right after "line\n", so the file ends with all six markers in
	// whatever order the queue ran them: a lost update would drop one or leave a bare line.
	seen := string(text)
	if !strings.HasPrefix(seen, "line\n") {
		t.Fatalf("content = %q", seen)
	}
	suffix := strings.TrimPrefix(seen, "line\n")
	if len(suffix) != 6 {
		t.Fatalf("markers = %q, want six", suffix)
	}
	counted := map[rune]int{}
	for _, marker := range suffix {
		counted[marker]++
	}
	for _, marker := range "abcdef" {
		if counted[marker] != 1 {
			t.Fatalf("markers = %q, %q appears %d times", suffix, marker, counted[marker])
		}
	}
}

func TestResolveReadPathFindsTypographicNames(t *testing.T) {
	dir := t.TempDir()
	// The name as macOS and copy-pasted text spell it: narrow no-break space before AM/PM, curly
	// apostrophe instead of a straight one.
	narrow := filepath.Join(dir, "report 3\u202FAM.txt")
	if err := os.WriteFile(narrow, []byte("narrow\n"), 0o644); err != nil {
		t.Fatalf("seed narrow: %v", err)
	}
	curly := filepath.Join(dir, "it\u2019s.txt")
	if err := os.WriteFile(curly, []byte("curly\n"), 0o644); err != nil {
		t.Fatalf("seed curly: %v", err)
	}
	if got := resolveReadPath(filepath.Join(dir, "report 3 AM.txt")); got != narrow {
		t.Fatalf("AM variant = %q, want %q", got, narrow)
	}
	if got := resolveReadPath(filepath.Join(dir, "it's.txt")); got != curly {
		t.Fatalf("apostrophe variant = %q, want %q", got, curly)
	}
	plain := writeFixture(t, dir, "plain.txt", "plain\n")
	if got := resolveReadPath(plain); got != plain {
		t.Fatalf("existing path = %q, want %q", got, plain)
	}
}
