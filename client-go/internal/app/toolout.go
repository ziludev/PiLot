package app

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"

	"pitta/internal/i18n"
)

// Tool output arrives as one string that used to be flattened to eighty
// characters on one line, so a build that failed on line 200 read the same as
// one that passed. The output is already what it is — a diff, a log, an error,
// a JSON blob, a list of files — and these turn it back into lines with a kind
// before the transcript draws it.

type outKind int

const (
	outPlain outKind = iota
	outAdd
	outDel
	outError
	outWarn
	outSkip // the "n lines dropped" marker
)

type outLine struct {
	text string
	kind outKind
}

const (
	// maxOutputLines bounds one step's block. Enough for a stack trace or the
	// end of a build, small enough that the transcript stays a transcript.
	maxOutputLines = 14
	// headLines is how many of those go to the start when output is dropped:
	// `read` puts the answer first, builds put it last, so both ends are kept.
	headLines = 4
	// maxOutputLine stops one minified line from being a paragraph.
	maxOutputLine = 300
)

var (
	ansiRe    = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]|\x1b\\][^\x07]*\x07")
	errRe     = regexp.MustCompile(`(?i)\b(error|fatal|panic|traceback|exception)\b|^FAIL\b|✗|exit (code|status) [1-9]`)
	warnRe    = regexp.MustCompile(`(?i)\bwarn(ing)?\b|⚠`)
	zeroErrRe = regexp.MustCompile(`(?i)\b0 (errors?|problems?|failed)\b`)
)

// outputLines splits one tool result into drawable lines. Output longer than
// limit keeps its head and its tail with a marker between them.
func outputLines(text string, limit int) []outLine {
	text = prettyJSON(stripANSI(text))
	raw := strings.Split(text, "\n")
	for len(raw) > 0 && strings.TrimSpace(raw[len(raw)-1]) == "" {
		raw = raw[:len(raw)-1]
	}
	for len(raw) > 0 && strings.TrimSpace(raw[0]) == "" {
		raw = raw[1:]
	}
	if len(raw) == 0 {
		return nil
	}
	var lines []outLine
	if len(raw) <= limit {
		for _, line := range raw {
			lines = append(lines, outLine{text: clip(strings.TrimRight(line, " \t"), maxOutputLine), kind: classify(line)})
		}
		return lines
	}
	head := headLines
	if head > limit-1 {
		head = limit - 1
	}
	tail := limit - head - 1
	for _, line := range raw[:head] {
		lines = append(lines, outLine{text: clip(strings.TrimRight(line, " \t"), maxOutputLine), kind: classify(line)})
	}
	lines = append(lines, outLine{
		text: i18n.TF("outputMore", map[string]any{"n": len(raw) - head - tail}),
		kind: outSkip,
	})
	for _, line := range raw[len(raw)-tail:] {
		lines = append(lines, outLine{text: clip(strings.TrimRight(line, " \t"), maxOutputLine), kind: classify(line)})
	}
	return lines
}

// classify reads one line. The order matters: a removed diff line can be a
// removed error, and it is still a removal.
func classify(line string) outKind {
	line = ansiRe.ReplaceAllString(line, "")
	trimmed := strings.TrimSpace(line)
	switch {
	case trimmed == "":
		return outPlain
	case strings.HasPrefix(trimmed, "+") && !strings.HasPrefix(trimmed, "+++"):
		return outAdd
	case strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "---"):
		return outDel
	case warnRe.MatchString(trimmed) && !errRe.MatchString(trimmed):
		return outWarn
	case errRe.MatchString(trimmed) && !zeroErrRe.MatchString(trimmed):
		return outError
	}
	return outPlain
}

// stripANSI removes the color and cursor codes a tool leaves behind when it
// thinks it is writing to a terminal: drawn raw they are boxes and blanks.
func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// prettyJSON re-indents a result that is one JSON value, so a tool that answers
// in a single line still reads as a document. Anything else comes back as it
// was — a log that happens to start with a brace is not JSON.
func prettyJSON(s string) string {
	s = stripANSI(s)
	trimmed := strings.TrimSpace(s)
	if len(trimmed) < 2 || (trimmed[0] != '{' && trimmed[0] != '[') || !json.Valid([]byte(trimmed)) {
		return s
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, []byte(trimmed), "", "  "); err != nil {
		return s
	}
	return buf.String()
}
