package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"pitta/internal/theme"
)

// logsAt points the agent home at a temp dir and makes room for a log, so the
// viewer reads a file this test wrote rather than the one on the machine.
func logsAt(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PI_DURABLE_HOME", home)
	dir := filepath.Join(home, "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	return filepath.Join(dir, "agent.log")
}

func TestReadLogTailKeepsTheEnd(t *testing.T) {
	path := logsAt(t)
	var b strings.Builder
	for i := 1; i <= 4000; i++ {
		fmt.Fprintf(&b, "[2026-11-17 10:00:00] [toolbox] line %04d padded to be worth a few bytes\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	lines, size, _ := readLogTail(path)
	if size != int64(b.Len()) {
		t.Errorf("size = %d, want %d", size, b.Len())
	}
	if len(lines) == 0 {
		t.Fatal("no lines read from a log with 4000")
	}
	if want := "line 4000 padded to be worth a few bytes"; !strings.HasSuffix(lines[len(lines)-1], want) {
		t.Errorf("last line = %q, want it to end with %q", lines[len(lines)-1], want)
	}
	// The cut lands mid-line; that half line must not come through as one.
	if !strings.HasPrefix(lines[0], "[2026-11-17 10:00:00] [toolbox] line ") {
		t.Errorf("first line = %q, want a whole log line", lines[0])
	}
	if total := len(strings.Join(lines, "\n")); total > logTailBytes {
		t.Errorf("tail is %d bytes, want at most %d", total, logTailBytes)
	}
}

func TestReadLogTailSmallFile(t *testing.T) {
	path := logsAt(t)
	if err := os.WriteFile(path, []byte("first\nsecond\nthird\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	lines, _, _ := readLogTail(path)
	if len(lines) != 3 || lines[0] != "first" || lines[2] != "third" {
		t.Errorf("lines = %q, want first/second/third", lines)
	}
}

func TestReadLogTailSaysNothingForAMissingFile(t *testing.T) {
	path := logsAt(t)
	lines, size, _ := readLogTail(path)
	if len(lines) != 0 || size != 0 {
		t.Errorf("lines = %q size = %d, want nothing", lines, size)
	}
}

func TestLogGrewFollowsAnAppendedLine(t *testing.T) {
	path := logsAt(t)
	if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	lines, size, mod := readLogTail(path)
	if len(lines) != 1 {
		t.Fatalf("lines = %q, want the one line", lines)
	}
	if _, _, _, changed := logGrew(path, size, mod); changed {
		t.Error("logGrew reported a change for a file nobody wrote")
	}
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("append: %v", err)
	}
	lines, _, _, changed := logGrew(path, size, mod)
	if !changed {
		t.Fatal("logGrew missed an appended line")
	}
	if len(lines) != 2 || lines[1] != "two" {
		t.Errorf("lines = %q, want one/two", lines)
	}
}

func TestToggleLogsOpensAndClosesThePoller(t *testing.T) {
	path := logsAt(t)
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	a := &App{}
	a.toggleLogs()
	if !a.logs.open || len(a.logs.lines) != 2 || a.logs.stop == nil {
		t.Fatalf("after open: open=%v lines=%d stop=%v", a.logs.open, len(a.logs.lines), a.logs.stop != nil)
	}
	stop := a.logs.stop
	a.toggleLogs()
	if a.logs.open || a.logs.stop != nil {
		t.Fatalf("after close: open=%v stop=%v", a.logs.open, a.logs.stop != nil)
	}
	select {
	case _, ok := <-stop:
		if ok {
			t.Error("the poller's stop channel is not closed")
		}
	default:
		t.Error("the poller was left running")
	}
}

func TestLogsDialogDrawsTheTail(t *testing.T) {
	path := logsAt(t)
	line := "[2026-11-17 10:00:00] [toolbox] 2026/11/17 10:00:00 bash: tool=bash exit=0"
	if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
	a := &App{}
	a.toggleLogs()
	defer a.toggleLogs()

	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Dark())
		a.logsDialog(c, theme.Dark())
	}, 640, 520)
	tt.Frame()
	noRawKeys(t, tt)
	if texts := strings.Join(tt.Texts(), "\n"); !strings.Contains(texts, "bash: tool=bash exit=0") {
		t.Errorf("the dialog drew %q, want the log line in it", texts)
	}
}
