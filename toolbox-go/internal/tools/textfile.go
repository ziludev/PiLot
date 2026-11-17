package tools

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const bomPrefix = "\uFEFF"

// stripBom returns the BOM (if any) and the text without it.
func stripBom(content string) (string, string) {
	if strings.HasPrefix(content, bomPrefix) {
		return bomPrefix, content[len(bomPrefix):]
	}
	return "", content
}

// detectLineEnding: CRLF only when the first CRLF comes before the first LF.
func detectLineEnding(content string) string {
	crlf := strings.Index(content, "\r\n")
	lf := strings.Index(content, "\n")
	if lf == -1 || crlf == -1 {
		return "\n"
	}
	if crlf < lf {
		return "\r\n"
	}
	return "\n"
}

func normalizeToLF(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// nfkcCompat approximates String.prototype.normalize("NFKC"). Go's standard library has no
// normalization tables, so this covers the width folding that actually shows up in pasted code:
// full-width ASCII (U+FF01-U+FF5E) and the ideographic space.
func nfkcCompat(text string) string {
	if isASCII(text) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r >= '\uFF01' && r <= '\uFF5E':
			b.WriteRune(r - 0xFEE0)
		case r == '\u3000':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if text[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// normalizeForFuzzyMatch mirrors edit-diff.js: NFKC, trailing whitespace per line removed, smart
// quotes/dashes/spaces folded to ASCII.
func normalizeForFuzzyMatch(text string) string {
	normalized := nfkcCompat(text)
	lines := strings.Split(normalized, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	joined := strings.Join(lines, "\n")
	var b strings.Builder
	b.Grow(len(joined))
	for _, r := range joined {
		switch {
		case r == '\u2018' || r == '\u2019' || r == '\u201a' || r == '\u201b':
			b.WriteRune('\'')
		case r == '\u201c' || r == '\u201d' || r == '\u201e' || r == '\u201f':
			b.WriteRune('"')
		case (r >= '\u2010' && r <= '\u2015') || r == '\u2212':
			b.WriteRune('-')
		case isUnicodeSpace(r) && r != '\u3000':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// splitLinesWithEndings is /[^\n]*\n|[^\n]+/g: every line keeps its terminator, and a trailing
// newline does not produce an extra empty line.
func splitLinesWithEndings(content string) []string {
	if content == "" {
		return nil
	}
	lines := make([]string, 0, strings.Count(content, "\n")+1)
	for len(content) > 0 {
		idx := strings.IndexByte(content, '\n')
		if idx < 0 {
			lines = append(lines, content)
			break
		}
		lines = append(lines, content[:idx+1])
		content = content[idx+1:]
	}
	return lines
}

// lineSpan is the byte range of one line without its terminator.
type lineSpan struct {
	start int
	end   int
}

func getLineSpans(content string) []lineSpan {
	lines := splitLinesWithEndings(content)
	spans := make([]lineSpan, 0, len(lines))
	offset := 0
	for _, line := range lines {
		lines := line
		spans = append(spans, lineSpan{start: offset, end: offset + len(lines)})
		offset += len(lines)
	}
	return spans
}

// decodeUTF8 replaces invalid sequences the way Node's "utf8" decoding does: one U+FFFD per
// maximal invalid subpart.
func decodeFileUTF8(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.ToValidUTF8(string(data), "\uFFFD")
}
