package i18n

import (
	_ "embed"
	"regexp"
	"testing"
)

// i18nOrigin is the React client's dictionary, vendored here so that the two
// stay comparable after the React client is gone. It is provenance, not a
// build input: dict.go carries the strings.
//
//go:embed origin/i18n.ts
var i18nOrigin string

// originLanguages splits the vendored TS into its two tables, so a key added
// to one and forgotten in the other fails the comparison below.
func originLanguages(t *testing.T) map[string]map[string]bool {
	t.Helper()
	table := regexp.MustCompile(`(?m)^\s{2}(zh|en): \{`)
	entry := regexp.MustCompile(`(?m)^\s{4}([A-Za-z0-9_]+):`)
	matches := table.FindAllStringSubmatchIndex(i18nOrigin, -1)
	if len(matches) != 2 {
		t.Fatalf("found %d language tables in origin/i18n.ts, want 2", len(matches))
	}
	out := map[string]map[string]bool{}
	for i, loc := range matches {
		lang := i18nOrigin[loc[2]:loc[3]]
		end := len(i18nOrigin)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		seen := map[string]bool{}
		for _, key := range entry.FindAllStringSubmatch(i18nOrigin[loc[1]:end], -1) {
			seen[key[1]] = true
		}
		out[lang] = seen
	}
	return out
}

// TestOriginDictionaryStillMatches checks every key of the vendored React
// dictionary is in ours, and that we added nothing but the keys the native
// client needed. This is the constraint the goal keeps: the dictionary holds
// exactly what i18n.ts holds.
func TestOriginDictionaryStillMatches(t *testing.T) {
	origin := originLanguages(t)
	ours := map[string]map[string]string{"zh": zh, "en": en}
	allowed := map[string]bool{
		// Ours: a native window has no clipboard image paste, so the picture
		// picker names itself.
		"attachImage": true, "attachImageHint": true, "attach": true,
	}
	for lang, keys := range origin {
		if len(keys) == 0 {
			t.Fatalf("no keys parsed for %s", lang)
		}
		if len(keys) != len(ours[lang])-len(allowed) {
			t.Logf("%s: origin has %d keys, ours %d (allowed extras %d)", lang, len(keys), len(ours[lang]), len(allowed))
		}
		for key := range keys {
			if _, ok := ours[lang][key]; !ok {
				t.Errorf("%s key %q is missing from dict.go", lang, key)
			}
		}
		for key := range ours[lang] {
			if keys[key] || allowed[key] {
				continue
			}
			t.Errorf("%s key %q is not in i18n.ts and is not an allowed extra", lang, key)
		}
	}
}
