package side

import (
	"hash/fnv"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// highlightCache keeps the tokenized spans of the file on screen. Tokenizing a
// two-thousand-line file costs a few milliseconds, which is fine once and
// wasteful sixty times a second, so the spans live until the file changes.
type highlightCache struct {
	key   uint64
	spans [][]ui.Span
}

// highlightLang maps a file extension to the name mygo's highlighter knows.
// An extension outside the table gets no colors, which is what an editor does
// with an unknown file type too.
func highlightLang(path string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "go":
		return "go"
	case "ts", "tsx":
		return "ts"
	case "js", "jsx", "mjs", "cjs":
		return "js"
	case "py":
		return "python"
	case "rs":
		return "rust"
	case "c", "h":
		return "c"
	case "cc", "cpp", "hpp", "cxx":
		return "cpp"
	case "java":
		return "java"
	case "kt", "kts":
		return "kotlin"
	case "rb":
		return "ruby"
	case "php":
		return "php"
	case "swift":
		return "swift"
	case "sh", "bash", "zsh":
		return "bash"
	case "sql":
		return "sql"
	case "json":
		return "json"
	case "yaml", "yml":
		return "yaml"
	case "toml":
		return "toml"
	case "md", "markdown":
		return "markdown"
	case "css":
		return "css"
	case "html", "htm":
		return "html"
	}
	return ""
}

// codeLines tokenizes every line of the file, carrying the tokenizer state from
// one line to the next so that comments and strings spanning lines stay
// colored. A file whose type has no tokenizer, or that is too long to be worth
// it, comes back nil and the viewer draws plain text.
func codeLines(c *ui.Context, cache *highlightCache, path, content string, lines []string) [][]ui.Span {
	lang := highlightLang(path)
	if lang == "" || len(lines) > 20000 {
		return nil
	}
	language := editor.LanguageNamed(lang)
	if language == nil || language.Tokenizer == nil {
		return nil
	}
	key := contentKey(path, content)
	if cache.key == key && cache.spans != nil {
		return cache.spans
	}
	colors := editor.DefaultTheme(c.Theme())
	spans := make([][]ui.Span, len(lines))
	state := uint64(0)
	for i, line := range lines {
		tokens, next := language.Tokenizer.Tokenize(line, state, nil)
		state = next
		spans[i] = lineSpans(line, tokens, colors)
	}
	cache.key, cache.spans = key, spans
	return spans
}

// lineSpans turns a line's tokens into colored spans, defaulting the gaps and
// the tail to the plain text color the way the markdown code block does.
func lineSpans(line string, tokens []editor.Token, colors *editor.Theme) []ui.Span {
	if len(tokens) == 0 {
		return []ui.Span{{Text: line, Color: colors.Text}}
	}
	spans := make([]ui.Span, 0, len(tokens)+1)
	start := 0
	for _, token := range tokens {
		end := token.End
		if end > len(line) {
			end = len(line)
		}
		if end <= start {
			continue
		}
		style := colors.Tokens[token.Kind]
		spans = append(spans, ui.Span{
			Text:   line[start:end],
			Color:  colors.TokenColor(token.Kind),
			Italic: style.Italic,
			Weight: weightOf(style.Bold),
		})
		start = end
	}
	if start < len(line) {
		spans = append(spans, ui.Span{Text: line[start:], Color: colors.Text})
	}
	if len(spans) == 0 {
		spans = append(spans, ui.Span{Text: line, Color: colors.Text})
	}
	return spans
}

// weightOf is the span weight a bold token gets, and 0 — the element's own
// weight — for the rest.
func weightOf(bold bool) int {
	if bold {
		return 600
	}
	return 0
}

// contentKey identifies a file's contents, so that a reload of the same bytes
// reuses the spans and a change does not.
func contentKey(path, content string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(content))
	return h.Sum64()
}

// codeSpans is the cache-aware entry the viewer calls per visible row.
func codeSpans(c *ui.Context, cache *highlightCache, path, content string, lines []string) [][]ui.Span {
	return codeLines(c, cache, path, content, lines)
}
