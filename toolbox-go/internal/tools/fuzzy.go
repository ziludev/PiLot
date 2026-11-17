package tools

import (
	"fmt"
	"sort"
	"strings"
)

// matchedEdit is one validated replacement against the replacement base content.
type matchedEdit struct {
	editIndex   int
	matchIndex  int
	matchLength int
	newText     string
}

type fuzzyMatch struct {
	found      bool
	index      int
	matchLen   int
	usedFuzzy  bool
	matchFound string
}

// fuzzyFindText tries an exact match first, then a match in fuzzy-normalized space.
func fuzzyFindText(content, oldText string) fuzzyMatch {
	if idx := strings.Index(content, oldText); idx != -1 {
		return fuzzyMatch{found: true, index: idx, matchLen: len(oldText)}
	}
	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOld := normalizeForFuzzyMatch(oldText)
	idx := strings.Index(fuzzyContent, fuzzyOld)
	if idx == -1 {
		return fuzzyMatch{}
	}
	return fuzzyMatch{found: true, index: idx, matchLen: len(fuzzyOld), usedFuzzy: true, matchFound: fuzzyContent}
}

// countOccurrences counts matches in fuzzy-normalized space, exactly like edit-diff.js.
func countOccurrences(content, oldText string) int {
	return strings.Count(normalizeForFuzzyMatch(content), normalizeForFuzzyMatch(oldText))
}

func applyReplacements(content string, replacements []matchedEdit, offset int) string {
	result := content
	for i := len(replacements) - 1; i >= 0; i-- {
		edit := replacements[i]
		start := edit.matchIndex - offset
		result = result[:start] + edit.newText + result[start+edit.matchLength:]
	}
	return result
}

// applyReplacementsPreservingUnchangedLines keeps untouched line blocks byte-identical to the
// original content when the replacements were matched in fuzzy-normalized space.
func applyReplacementsPreservingUnchangedLines(originalContent, baseContent string, replacements []matchedEdit) (string, error) {
	originalLines := splitLinesWithEndings(originalContent)
	baseLines := getLineSpans(baseContent)
	if len(originalLines) != len(baseLines) {
		return "", fmt.Errorf("Cannot preserve unchanged lines because the base content has a different line count.")
	}
	sorted := make([]matchedEdit, len(replacements))
	copy(sorted, replacements)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].matchIndex < sorted[j].matchIndex })

	type group struct {
		startLine int
		endLine   int
		edits     []matchedEdit
	}
	var groups []*group
	for _, replacement := range sorted {
		start, end, err := replacementLineRange(baseLines, replacement)
		if err != nil {
			return "", err
		}
		if len(groups) > 0 {
			current := groups[len(groups)-1]
			if start < current.endLine {
				if end > current.endLine {
					current.endLine = end
				}
				current.edits = append(current.edits, replacement)
				continue
			}
		}
		groups = append(groups, &group{startLine: start, endLine: end, edits: []matchedEdit{replacement}})
	}

	var result strings.Builder
	originalLineIndex := 0
	for _, g := range groups {
		result.WriteString(strings.Join(originalLines[originalLineIndex:g.startLine], ""))
		groupStart := baseLines[g.startLine].start
		groupEnd := baseLines[g.endLine-1].end
		result.WriteString(applyReplacements(baseContent[groupStart:groupEnd], g.edits, groupStart))
		originalLineIndex = g.endLine
	}
	result.WriteString(strings.Join(originalLines[originalLineIndex:], ""))
	return result.String(), nil
}

func replacementLineRange(lines []lineSpan, replacement matchedEdit) (int, int, error) {
	replacementStart := replacement.matchIndex
	replacementEnd := replacement.matchIndex + replacement.matchLength
	startLine := -1
	for i, line := range lines {
		if replacementStart >= line.start && replacementStart < line.end {
			startLine = i
			break
		}
	}
	if startLine == -1 {
		return 0, 0, fmt.Errorf("Replacement range is outside the base content.")
	}
	endLine := startLine
	for endLine < len(lines) && lines[endLine].end < replacementEnd {
		endLine++
	}
	if endLine >= len(lines) {
		return 0, 0, fmt.Errorf("Replacement range is outside the base content.")
	}
	return startLine, endLine + 1, nil
}

// applyEditsToNormalizedContent is the Go port of edit-diff.js, message texts included.
func applyEditsToNormalizedContent(normalizedContent string, edits []Edit, path string) (baseContent, newContent string, err error) {
	total := len(edits)
	oldTexts := make([]string, total)
	newTexts := make([]string, total)
	for i, edit := range edits {
		oldTexts[i] = normalizeToLF(edit.OldText)
		newTexts[i] = normalizeToLF(edit.NewText)
	}
	for i, oldText := range oldTexts {
		if len(oldText) == 0 {
			return "", "", emptyOldTextError(path, i, total)
		}
	}
	usedFuzzy := false
	for i := range oldTexts {
		if fuzzyFindText(normalizedContent, oldTexts[i]).usedFuzzy {
			usedFuzzy = true
			break
		}
	}
	replacementBase := normalizedContent
	if usedFuzzy {
		replacementBase = normalizeForFuzzyMatch(normalizedContent)
	}
	matched := make([]matchedEdit, 0, total)
	for i := range oldTexts {
		match := fuzzyFindText(replacementBase, oldTexts[i])
		if !match.found {
			return "", "", notFoundError(path, i, total)
		}
		occurrences := countOccurrences(replacementBase, oldTexts[i])
		if occurrences > 1 {
			return "", "", duplicateError(path, i, total, occurrences)
		}
		matched = append(matched, matchedEdit{
			editIndex:   i,
			matchIndex:  match.index,
			matchLength: match.matchLen,
			newText:     newTexts[i],
		})
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].matchIndex < matched[j].matchIndex })
	for i := 1; i < len(matched); i++ {
		previous, current := matched[i-1], matched[i]
		if previous.matchIndex+previous.matchLength > current.matchIndex {
			return "", "", fmt.Errorf("edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions.",
				previous.editIndex, current.editIndex, path)
		}
	}
	baseContent = normalizedContent
	if usedFuzzy {
		newContent, err = applyReplacementsPreservingUnchangedLines(normalizedContent, replacementBase, matched)
		if err != nil {
			return "", "", err
		}
	} else {
		newContent = applyReplacements(replacementBase, matched, 0)
	}
	if baseContent == newContent {
		return "", "", noChangeError(path, total)
	}
	return baseContent, newContent, nil
}

func notFoundError(path string, editIndex, total int) error {
	if total == 1 {
		return fmt.Errorf("Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.", path)
	}
	return fmt.Errorf("Could not find edits[%d] in %s. The oldText must match exactly including all whitespace and newlines.", editIndex, path)
}

func duplicateError(path string, editIndex, total, occurrences int) error {
	if total == 1 {
		return fmt.Errorf("Found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique.", occurrences, path)
	}
	return fmt.Errorf("Found %d occurrences of edits[%d] in %s. Each oldText must be unique. Please provide more context to make it unique.", occurrences, editIndex, path)
}

func emptyOldTextError(path string, editIndex, total int) error {
	if total == 1 {
		return fmt.Errorf("oldText must not be empty in %s.", path)
	}
	return fmt.Errorf("edits[%d].oldText must not be empty in %s.", editIndex, path)
}

func noChangeError(path string, total int) error {
	if total == 1 {
		return fmt.Errorf("No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.", path)
	}
	return fmt.Errorf("No changes made to %s. The replacements produced identical content.", path)
}
