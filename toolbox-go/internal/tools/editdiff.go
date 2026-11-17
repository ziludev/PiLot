package tools

import (
	"fmt"

	"strconv"
	"strings"
)

// This file ports the parts of the `diff` package (jsdiff 8.x) that pi-durable's edit tool uses,
// so the toolbox produces byte-identical `details.diff` and `details.patch`.

type diffPart struct {
	value   string
	added   bool
	removed bool
}

// tokenizeLines mirrors jsdiff's line tokenizer: lines keep their terminator, an empty trailing
// token is dropped, and empty tokens are removed.
func tokenizeLines(value string) []string {
	var pieces []string
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] == '\r' && i+1 < len(value) && value[i+1] == '\n' {
			pieces = append(pieces, value[start:i], "\r\n")
			i++
			start = i + 1
			continue
		}
		if value[i] == '\n' {
			pieces = append(pieces, value[start:i], "\n")
			start = i + 1
		}
	}
	pieces = append(pieces, value[start:])
	if len(pieces) > 0 && pieces[len(pieces)-1] == "" {
		pieces = pieces[:len(pieces)-1]
	}
	// Merge content tokens with their separators; a separator is every odd-indexed piece.
	tokens := make([]string, 0, len(pieces))
	for i := 0; i < len(pieces); i++ {
		if i%2 == 1 {
			if len(tokens) == 0 {
				continue
			}
			tokens[len(tokens)-1] += pieces[i]
			continue
		}
		tokens = append(tokens, pieces[i])
	}
	nonEmpty := tokens[:0]
	for _, token := range tokens {
		if token != "" {
			nonEmpty = append(nonEmpty, token)
		}
	}
	return nonEmpty
}

type component struct {
	count   int
	added   bool
	removed bool
	prev    *component
}

type diffPath struct {
	oldPos int
	last   *component
}

func pathExtractCommon(base *diffPath, newTokens, oldTokens []string, diagonal int) int {
	oldPos := base.oldPos
	newPos := oldPos - diagonal
	common := 0
	for newPos+1 < len(newTokens) && oldPos+1 < len(oldTokens) && oldTokens[oldPos+1] == newTokens[newPos+1] {
		newPos++
		oldPos++
		common++
	}
	if common > 0 {
		base.last = &component{count: common, prev: base.last}
	}
	base.oldPos = oldPos
	return newPos
}

func pathAdd(base *diffPath, added, removed bool, oldPosInc int) *diffPath {
	last := base.last
	if last != nil && last.added == added && last.removed == removed {
		return &diffPath{
			oldPos: base.oldPos + oldPosInc,
			last:   &component{count: last.count + 1, added: added, removed: removed, prev: last.prev},
		}
	}
	return &diffPath{
		oldPos: base.oldPos + oldPosInc,
		last:   &component{count: 1, added: added, removed: removed, prev: last},
	}
}

func buildDiffParts(last *component, newTokens, oldTokens []string) []diffPart {
	var reversed []*component
	for last != nil {
		reversed = append(reversed, last)
		last = last.prev
	}
	components := make([]*component, len(reversed))
	for i, c := range reversed {
		components[len(reversed)-1-i] = c
	}
	parts := make([]diffPart, 0, len(components))
	newPos, oldPos := 0, 0
	for _, c := range components {
		switch {
		case c.removed:
			parts = append(parts, diffPart{value: strings.Join(oldTokens[oldPos:oldPos+c.count], ""), removed: true})
			oldPos += c.count
		case c.added:
			parts = append(parts, diffPart{value: strings.Join(newTokens[newPos:newPos+c.count], ""), added: true})
			newPos += c.count
		default:
			parts = append(parts, diffPart{value: strings.Join(newTokens[newPos:newPos+c.count], "")})
			newPos += c.count
			oldPos += c.count
		}
	}
	return parts
}

// diffLines is jsdiff's line diff: Myers with the diagonal trimming optimization.
func diffLines(oldStr, newStr string) []diffPart {
	oldTokens := tokenizeLines(oldStr)
	newTokens := tokenizeLines(newStr)
	oldLen, newLen := len(oldTokens), len(newTokens)

	bestPath := map[int]*diffPath{0: {oldPos: -1}}
	newPos := pathExtractCommon(bestPath[0], newTokens, oldTokens, 0)
	if bestPath[0].oldPos+1 >= oldLen && newPos+1 >= newLen {
		return buildDiffParts(bestPath[0].last, newTokens, oldTokens)
	}
	minDiagonal, maxDiagonal := -1<<31, 1<<31-1
	maxEditLength := newLen + oldLen
	for editLength := 1; editLength <= maxEditLength; editLength++ {
		start := maxInt(minDiagonal, -editLength)
		end := minInt(maxDiagonal, editLength)
		for diagonal := start; diagonal <= end; diagonal += 2 {
			removePath := bestPath[diagonal-1]
			addPath := bestPath[diagonal+1]
			if removePath != nil {
				delete(bestPath, diagonal-1)
			}
			canAdd := false
			if addPath != nil {
				addPathNewPos := addPath.oldPos - diagonal
				canAdd = addPathNewPos >= 0 && addPathNewPos < newLen
			}
			canRemove := removePath != nil && removePath.oldPos+1 < oldLen
			if !canAdd && !canRemove {
				delete(bestPath, diagonal)
				continue
			}
			var basePath *diffPath
			if !canRemove || (canAdd && removePath.oldPos < addPath.oldPos) {
				basePath = pathAdd(addPath, true, false, 0)
			} else {
				basePath = pathAdd(removePath, false, true, 1)
			}
			currentNewPos := pathExtractCommon(basePath, newTokens, oldTokens, diagonal)
			if basePath.oldPos+1 >= oldLen && currentNewPos+1 >= newLen {
				return buildDiffParts(basePath.last, newTokens, oldTokens)
			}
			bestPath[diagonal] = basePath
			if basePath.oldPos+1 >= oldLen {
				maxDiagonal = minInt(maxDiagonal, diagonal-1)
			}
			if currentNewPos+1 >= newLen {
				minDiagonal = maxInt(minDiagonal, diagonal+1)
			}
		}
	}
	return buildDiffParts(bestPath[0].last, newTokens, oldTokens)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// generateDiffString is pi-durable's display diff: +/- lines with line numbers, four context lines.
func generateDiffString(oldContent, newContent string, contextLines int) (string, *int) {
	parts := diffLines(oldContent, newContent)
	var output []string
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")
	maxLineNum := maxInt(len(oldLines), len(newLines))
	lineNumWidth := len(strconv.Itoa(maxLineNum))
	oldLineNum, newLineNum := 1, 1
	lastWasChange := false
	var firstChangedLine *int
	pad := func(n int) string {
		s := strconv.Itoa(n)
		if len(s) >= lineNumWidth {
			return s
		}
		return strings.Repeat(" ", lineNumWidth-len(s)) + s
	}
	blank := strings.Repeat(" ", lineNumWidth)
	for i, part := range parts {
		raw := strings.Split(part.value, "\n")
		if len(raw) > 0 && raw[len(raw)-1] == "" {
			raw = raw[:len(raw)-1]
		}
		if part.added || part.removed {
			if firstChangedLine == nil {
				line := newLineNum
				firstChangedLine = &line
			}
			for _, line := range raw {
				if part.added {
					output = append(output, fmt.Sprintf("+%s %s", pad(newLineNum), line))
					newLineNum++
				} else {
					output = append(output, fmt.Sprintf("-%s %s", pad(oldLineNum), line))
					oldLineNum++
				}
			}
			lastWasChange = true
			continue
		}
		nextPartIsChange := i < len(parts)-1 && (parts[i+1].added || parts[i+1].removed)
		hasLeadingChange := lastWasChange
		hasTrailingChange := nextPartIsChange
		context := func(lines []string) {
			for _, line := range lines {
				output = append(output, fmt.Sprintf(" %s %s", pad(oldLineNum), line))
				oldLineNum++
				newLineNum++
			}
		}
		switch {
		case hasLeadingChange && hasTrailingChange:
			if len(raw) <= contextLines*2 {
				context(raw)
			} else {
				leading := raw[:contextLines]
				trailing := raw[len(raw)-contextLines:]
				skipped := len(raw) - len(leading) - len(trailing)
				context(leading)
				output = append(output, fmt.Sprintf(" %s ...", blank))
				oldLineNum += skipped
				newLineNum += skipped
				context(trailing)
			}
		case hasLeadingChange:
			shown := raw
			if len(raw) > contextLines {
				shown = raw[:contextLines]
			}
			skipped := len(raw) - len(shown)
			context(shown)
			if skipped > 0 {
				output = append(output, fmt.Sprintf(" %s ...", blank))
				oldLineNum += skipped
				newLineNum += skipped
			}
		case hasTrailingChange:
			skipped := maxInt(0, len(raw)-contextLines)
			if skipped > 0 {
				output = append(output, fmt.Sprintf(" %s ...", blank))
				oldLineNum += skipped
				newLineNum += skipped
			}
			context(raw[skipped:])
		default:
			oldLineNum += len(raw)
			newLineNum += len(raw)
		}
		lastWasChange = false
	}
	return strings.Join(output, "\n"), firstChangedLine
}

// splitLinesWithNewlines is jsdiff's splitLines: every line keeps its terminator, except a final
// line that has none. An empty string has no lines at all.
func splitLinesWithNewlines(text string) []string {
	var out []string
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			out = append(out, text[start:i+1])
			start = i + 1
		case '\r':
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			out = append(out, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		out = append(out, text[start:])
	}
	return out
}

type patchHunk struct {
	oldStart, oldLines int
	newStart, newLines int
	lines              []string
}

// generateUnifiedPatch is createTwoFilesPatch(path, path, old, new, undefined, undefined,
// {context: 4, headerOptions: FILE_HEADERS_ONLY}).
func generateUnifiedPatch(path, oldContent, newContent string, contextLines int) string {
	diff := diffLines(oldContent, newContent)
	diff = append(diff, diffPart{value: ""})
	valueLines := make([][]string, len(diff))
	contextLinesOf := func(lines []string) []string {
		out := make([]string, len(lines))
		for i, entry := range lines {
			out[i] = " " + entry
		}
		return out
	}
	var hunks []patchHunk
	oldRangeStart, newRangeStart, oldLine, newLine := 0, 0, 1, 1
	var curRange []string
	for i := 0; i < len(diff); i++ {
		current := diff[i]
		lines, ok := valueLines[i], false
		if !ok {
			lines = splitLinesWithNewlines(current.value)
			valueLines[i] = lines
		}
		if current.added || current.removed {
			if oldRangeStart == 0 {
				oldRangeStart = oldLine
				newRangeStart = newLine
				if i > 0 {
					prev := valueLines[i-1]
					if contextLines > 0 {
						tail := prev
						if len(tail) > contextLines {
							tail = tail[len(tail)-contextLines:]
						}
						curRange = contextLinesOf(tail)
					} else {
						curRange = nil
					}
					oldRangeStart -= len(curRange)
					newRangeStart -= len(curRange)
				}
			}
			prefix := "-"
			if current.added {
				prefix = "+"
			}
			for _, line := range lines {
				curRange = append(curRange, prefix+line)
			}
			if current.added {
				newLine += len(lines)
			} else {
				oldLine += len(lines)
			}
			continue
		}
		if oldRangeStart != 0 {
			if len(lines) <= contextLines*2 && i < len(diff)-2 {
				curRange = append(curRange, contextLinesOf(lines)...)
			} else {
				contextSize := minInt(len(lines), contextLines)
				curRange = append(curRange, contextLinesOf(lines[:contextSize])...)
				hunks = append(hunks, patchHunk{
					oldStart: oldRangeStart,
					oldLines: oldLine - oldRangeStart + contextSize,
					newStart: newRangeStart,
					newLines: newLine - newRangeStart + contextSize,
					lines:    curRange,
				})
				oldRangeStart, newRangeStart = 0, 0
				curRange = nil
			}
		}
		oldLine += len(lines)
		newLine += len(lines)
	}
	for h := range hunks {
		lines := hunks[h].lines
		for i := 0; i < len(lines); i++ {
			if strings.HasSuffix(lines[i], "\n") {
				lines[i] = lines[i][:len(lines[i])-1]
				continue
			}
			lines = append(lines[:i+1], append([]string{"\\ No newline at end of file"}, lines[i+1:]...)...)
			i++
		}
		hunks[h].lines = lines
	}
	var out []string
	out = append(out, "--- "+path)
	out = append(out, "+++ "+path)
	for _, hunk := range hunks {
		oldStart, oldLines := hunk.oldStart, hunk.oldLines
		newStart, newLines := hunk.newStart, hunk.newLines
		if oldLines == 0 {
			oldStart--
		}
		if newLines == 0 {
			newStart--
		}
		out = append(out, fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldLines, newStart, newLines))
		out = append(out, hunk.lines...)
	}
	return strings.Join(out, "\n") + "\n"
}
