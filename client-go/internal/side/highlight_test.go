package side

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/protocol"
	"pitta/internal/theme"
)

// frameWith renders one path through FileView and returns the tester.
func frameWith(t *testing.T, path, content string) *ui.Tester {
	t.Helper()
	props := FileProps{Path: path, File: &protocol.FilePayload{Path: path, Content: content, Lines: len(strings.Split(content, "\n"))}, OnClose: func() {}}
	tt := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Light())
		FileView(c, props)
	}, 720, 480)
	tt.Frame()
	return tt
}

// TestGoFileIsColored checks a Go file comes back with more than one text
// color: keywords and comments are what makes scanning a file cheaper than
// reading it.
func TestGoFileIsColored(t *testing.T) {
	src := "package main\n\n// a comment\nfunc main() {\n\ts := \"hello\"\n\t_ = s\n}\n"
	colors := ui.NewTester(func(c *ui.Context) {
		theme.Apply(c, theme.Light())
		lines := strings.Split(src, "\n")
		var cache highlightCache
		spans := codeSpans(c, &cache, "main.go", src, lines)
		if len(spans) != len(lines) {
			t.Fatalf("got %d span rows for %d lines", len(spans), len(lines))
		}
		seen := map[ui.Color]bool{}
		for _, row := range spans {
			for _, span := range row {
				seen[span.Color] = true
			}
		}
		if len(seen) < 3 {
			t.Fatalf("a Go file used %d colors, want at least 3", len(seen))
		}
	}, 200, 100)
	colors.Frame()
}

// TestUnknownTypeStaysPlain checks an extension the highlighter does not know
// keeps the viewer's plain text instead of guessing.
func TestUnknownTypeStaysPlain(t *testing.T) {
	if lang := highlightLang("notes.wibble"); lang != "" {
		t.Fatalf("wibble mapped to %q", lang)
	}
	src := "just text\nand more\n"
	tt := ui.NewTester(func(c *ui.Context) {
		var cache highlightCache
		if spans := codeSpans(c, &cache, "notes.wibble", src, strings.Split(src, "\n")); spans != nil {
			t.Fatalf("plain file got %d span rows", len(spans))
		}
	}, 200, 100)
	tt.Frame()
}

// TestLargeFileKeepsTheFirstFrameCheap guards the promise that color does not
// cost the viewer its speed: two thousand lines must render well under the
// 50 ms first-frame budget.
func TestLargeFileKeepsTheFirstFrameCheap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		b.WriteString("func f() { // line ")
		b.WriteString(strings.Repeat("x", i%40))
		b.WriteString("\n}\n")
	}
	src := b.String()
	start := time.Now()
	tt := frameWith(t, "big.go", src)
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Fatalf("first frame of a 2000-line file took %v", elapsed)
	}
	_ = tt
}

// TestHighlightCacheReusesSpans checks the second frame of the same content
// does not tokenize again.
func TestHighlightCacheReusesSpans(t *testing.T) {
	src := "package main\n\nvar x = 1\n"
	lines := strings.Split(src, "\n")
	var cache highlightCache
	var first, second [][]ui.Span
	tt := ui.NewTester(func(c *ui.Context) {
		first = codeSpans(c, &cache, "a.go", src, lines)
		second = codeSpans(c, &cache, "a.go", src, lines)
	}, 200, 100)
	tt.Frame()
	if len(first) == 0 || len(second) == 0 {
		t.Fatal("no spans")
	}
	if &first[0][0] != &second[0][0] {
		t.Fatal("the second call tokenized again instead of using the cache")
	}
	if cache.key != contentKey("a.go", src) {
		t.Fatal("the cache key does not match the content")
	}
}
