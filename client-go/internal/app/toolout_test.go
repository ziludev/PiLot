package app

import (
	"strings"
	"testing"

	"pilot/internal/i18n"
)

func TestOutputLinesSplitsAndKinds(t *testing.T) {
	i18n.Set(i18n.EN)
	got := outputLines("diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\nexit code 1", maxOutputLines)
	want := []outKind{outPlain, outPlain, outPlain, outPlain, outDel, outAdd, outError}
	if len(got) != len(want) {
		t.Fatalf("got %d lines: %v", len(got), got)
	}
	for i, kind := range want {
		if got[i].kind != kind {
			t.Errorf("line %d %q: kind %v, want %v", i, got[i].text, got[i].kind, kind)
		}
	}
}

func TestOutputLinesDropsTheMiddle(t *testing.T) {
	i18n.Set(i18n.EN)
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", i))
		b.WriteString("\n")
	}
	lines := outputLines(b.String(), maxOutputLines)
	if len(lines) != maxOutputLines {
		t.Fatalf("got %d lines, want %d", len(lines), maxOutputLines)
	}
	// Both ends survive: `read` answers at the top, a build answers at the bottom.
	if !strings.HasPrefix(lines[0].text, "line x") {
		t.Errorf("head lost: %q", lines[0].text)
	}
	if lines[len(lines)-1].text != "line "+strings.Repeat("x", 40) {
		t.Errorf("tail lost: %q", lines[len(lines)-1].text)
	}
	if lines[headLines].kind != outSkip {
		t.Fatalf("no marker at %d: %v", headLines, lines[headLines])
	}
	if got := lines[headLines].text; !strings.Contains(got, "27") {
		t.Errorf("marker should say how many lines went: %q", got)
	}
}

func TestOutputLinesPrettyJSON(t *testing.T) {
	i18n.Set(i18n.EN)
	lines := outputLines(`{"exit":0,"files":["a.go","b.go"]}`, maxOutputLines)
	if len(lines) < 3 {
		t.Fatalf("json left on one line: %v", lines)
	}
	joined := strings.Join(texts(lines), "\n")
	if !strings.Contains(joined, "\n  \"exit\": 0,") {
		t.Errorf("not re-indented: %q", joined)
	}
}

func TestOutputLinesStripsANSI(t *testing.T) {
	i18n.Set(i18n.EN)
	lines := outputLines("\x1b[31merror\x1b[0m: no such file", maxOutputLines)
	if len(lines) != 1 {
		t.Fatalf("got %d lines", len(lines))
	}
	if strings.ContainsRune(lines[0].text, 0x1b) {
		t.Errorf("escape left in %q", lines[0].text)
	}
	if lines[0].kind != outError {
		t.Errorf("kind %v, want error", lines[0].kind)
	}
}

func TestOutputLinesClipsLongLine(t *testing.T) {
	i18n.Set(i18n.EN)
	lines := outputLines(strings.Repeat("あ", maxOutputLine+50), maxOutputLines)
	if len(lines) != 1 {
		t.Fatalf("got %d lines", len(lines))
	}
	if n := len([]rune(lines[0].text)); n > maxOutputLine+1 {
		t.Errorf("line is %d runes", n)
	}
}

func TestClassifyLeavesCleanOutputAlone(t *testing.T) {
	i18n.Set(i18n.EN)
	for _, line := range []string{
		"0 errors, 0 warnings",    // the summary after a good build
		"--- a/file.go",           // a diff header, not a removal
		"+++ b/file.go",           // nor an addition
		"PASS pilot/internal/app", // a plain line with a capital word
	} {
		if kind := classify(line); kind != outPlain && kind != outWarn {
			t.Errorf("%q classified %v", line, kind)
		}
	}
}

func texts(lines []outLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.text
	}
	return out
}

func TestDiffSummaryNamesTheFileAndCounts(t *testing.T) {
	patch := strings.Join([]string{
		"--- a/src/queue.ts",
		"+++ b/src/queue.ts",
		"@@ -12,7 +12,7 @@",
		"-  const retries = 3;",
		"+  const retries = 5;",
		"+  const backoff = 250;",
		"   await run();",
	}, "\n")
	file, add, del, ok := diffSummary(patch)
	if !ok {
		t.Fatal("a unified patch was not recognised as one")
	}
	if file != "src/queue.ts" {
		t.Fatalf("file %q, want src/queue.ts", file)
	}
	if add != 2 || del != 1 {
		t.Fatalf("counts +%d −%d, want +2 −1", add, del)
	}
}

func TestDiffSummaryIgnoresPlainOutput(t *testing.T) {
	if _, _, _, ok := diffSummary("build ok\n2 tests passed"); ok {
		t.Fatal("plain output was read as a patch")
	}
}
