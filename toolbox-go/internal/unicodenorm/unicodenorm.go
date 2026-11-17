// Package unicodenorm implements the two normalization forms the reference implementation uses
// (NFKC for fuzzy matching, NFD for path lookups) without a dependency: the tables in
// tables_gen.go are data generated from the Unicode database, and the rest is the standard
// algorithm. Behaviour is checked against Python's unicodedata in the tests.
package unicodenorm

import (
	"strings"
	"sync"
)

const (
	hangulSBase  = 0xAC00
	hangulLBase  = 0x1100
	hangulVBase  = 0x1161
	hangulTBase  = 0x11A7
	hangulLCount = 19
	hangulVCount = 21
	hangulTCount = 28
	hangulNCount = hangulVCount * hangulTCount
	hangulSCount = hangulLCount * hangulNCount
)

// table holds the parsed data; parsing once keeps every call allocation free on the hot path.
type table struct {
	canonical     map[rune]string
	compatibility map[rune]string
	combining     map[rune]uint8
	composites    map[[2]rune]rune
}

var once sync.Once
var data table

func load() {
	once.Do(func() {
		data = table{
			canonical:     parseRecords(canonicalDecomposition, canonicalDecompositionExtra),
			compatibility: parseRecords(compatibilityDecomposition, compatibilityDecompositionExtra),
			combining:     map[rune]uint8{},
			composites:    map[[2]rune]rune{},
		}
		for class, runes := range combiningByClass {
			for _, ch := range runes {
				data.combining[ch] = class
			}
		}
		for _, record := range strings.Split(primaryComposites, "\n") {
			if runes := []rune(record); len(runes) == 3 {
				data.composites[[2]rune{runes[0], runes[1]}] = runes[2]
			}
		}
	})
}

// parseRecords reads "source|decomposition" lines, where every source is exactly one code point.
func parseRecords(plain, extra string) map[rune]string {
	records := make(map[rune]string, 6000)
	for _, source := range []string{plain, extra} {
		for _, line := range strings.Split(source, "\n") {
			separator := strings.IndexRune(line, '|')
			if separator < 0 {
				continue
			}
			source, target := line[:separator], line[separator+1:]
			if runes := []rune(source); len(runes) == 1 {
				records[runes[0]] = target
			}
		}
	}
	return records
}

// NFD returns the canonical decomposition, canonically ordered.
func NFD(s string) string {
	load()
	return string(reorder(decompose(s, data.canonical)))
}

// NFKC returns the compatibility decomposition, canonically ordered and then composed.
func NFKC(s string) string {
	load()
	return string(compose(reorder(decompose(s, data.compatibility))))
}

// decompose expands every code point, including the Hangul syllables that decompose by arithmetic.
func decompose(s string, form map[rune]string) []rune {
	out := make([]rune, 0, len(s))
	for _, ch := range s {
		if index := int(ch) - hangulSBase; index >= 0 && index < hangulSCount {
			out = append(out,
				rune(hangulLBase+index/hangulNCount),
				rune(hangulVBase+(index%hangulNCount)/hangulTCount))
			if trailing := index % hangulTCount; trailing != 0 {
				out = append(out, rune(hangulTBase+trailing))
			}
			continue
		}
		if expanded, ok := form[ch]; ok {
			out = append(out, []rune(expanded)...)
			continue
		}
		out = append(out, ch)
	}
	return out
}

// reorder applies canonical ordering: within a run of non-starters, sort by combining class,
// keeping the relative order of equal classes.
func reorder(runes []rune) []rune {
	for start := 0; start < len(runes); {
		if data.combining[runes[start]] == 0 {
			start++
			continue
		}
		end := start
		for end < len(runes) && data.combining[runes[end]] != 0 {
			end++
		}
		// Insertion sort: these runs are short, and a stable order is required.
		for index := start + 1; index < end; index++ {
			for back := index; back > start; back-- {
				if data.combining[runes[back-1]] <= data.combining[runes[back]] {
					break
				}
				runes[back-1], runes[back] = runes[back], runes[back-1]
			}
		}
		start = end
	}
	return runes
}

// compose is the NFC composition step: an unblocked starter and a following mark combine.
func compose(runes []rune) []rune {
	out := make([]rune, 0, len(runes))
	starter := -1
	lastClass := uint8(0)
	for _, ch := range runes {
		class := data.combining[ch]
		if starter >= 0 && (lastClass == 0 || lastClass < class) {
			if combined, ok := compositeOf(out[starter], ch); ok {
				out[starter] = combined
				continue
			}
		}
		if class == 0 {
			starter = len(out)
			lastClass = 0
		} else {
			lastClass = class
		}
		out = append(out, ch)
	}
	return out
}

// compositeOf maps a pair to its primary composite, Hangul by arithmetic and the rest by table.
func compositeOf(first, second rune) (rune, bool) {
	if l := int(first) - hangulLBase; l >= 0 && l < hangulLCount {
		if v := int(second) - hangulVBase; v >= 0 && v < hangulVCount {
			return rune(hangulSBase + (l*hangulVCount+v)*hangulTCount), true
		}
	}
	if s := int(first) - hangulSBase; s >= 0 && s < hangulSCount && s%hangulTCount == 0 {
		if t := int(second) - hangulTBase; t > 0 && t < hangulTCount {
			return rune(hangulSBase + s + t), true
		}
	}
	combined, ok := data.composites[[2]rune{first, second}]
	return combined, ok
}
