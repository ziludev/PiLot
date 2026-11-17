package i18n

import (
	"bytes"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"testing"
)

// origin/i18n.ts holds 183 keys in each of its two tables. attachImage,
// attachImageHint and attach are ours, for the path-based picture picker a
// native desktop app needs; goalMet and goalNotMet are ours, for the verdict a
// turn ends on, which the React client drew as a tool result instead; close and
// logsEmpty are ours, for the log viewer that reads the file in the window;
// toolCallsCount is ours, for the line a run of tool calls folds into.
// TestOriginDictionaryStillMatches keeps that line.
const wantKeys = 193

// useLang switches the package language for one test and restores it after.
func useLang(t *testing.T, l Lang) {
	t.Helper()
	prev := Current()
	Set(l)
	t.Cleanup(func() { Set(prev) })
}

func TestDictionariesMatch(t *testing.T) {
	keys := Keys()
	if len(keys) != wantKeys {
		t.Fatalf("got %d keys, i18n.ts has %d", len(keys), wantKeys)
	}
	if len(zh) != len(en) {
		t.Fatalf("zh has %d keys, en has %d", len(zh), len(en))
	}
	for _, k := range keys {
		if _, ok := en[k]; !ok {
			t.Errorf("key %q is only in zh", k)
		}
		if _, ok := zh[k]; !ok {
			t.Errorf("key %q is only in en", k)
		}
		if zh[k] == "" {
			t.Errorf("key %q is empty in zh", k)
		}
		if en[k] == "" {
			t.Errorf("key %q is empty in en", k)
		}
	}
}

func TestKeysAreUniqueAndSorted(t *testing.T) {
	keys := Keys()
	for i := 1; i < len(keys); i++ {
		if keys[i-1] >= keys[i] {
			t.Fatalf("Keys not sorted at %d: %q >= %q", i, keys[i-1], keys[i])
		}
	}
}

// Only baseChangedHint interpolates in i18n.ts:30 and i18n.ts:215; a placeholder
// creeping into other copy means the tables drifted from the source.
func TestPlaceholdersMatchSource(t *testing.T) {
	var withPlaceholder []string
	for k, v := range zh {
		if strings.Contains(v, "{") {
			withPlaceholder = append(withPlaceholder, k)
		}
		if strings.Contains(en[k], "{") != strings.Contains(v, "{") {
			t.Errorf("key %q interpolates in one language only", k)
		}
	}
	sort.Strings(withPlaceholder)
	if want := []string{"baseChangedHint", "toolCallsCount"}; !slices.Equal(withPlaceholder, want) {
		t.Fatalf("interpolated keys = %v, want %v", withPlaceholder, want)
	}
}

func TestInterpolation(t *testing.T) {
	cases := []struct {
		name string
		lang Lang
		key  string
		args map[string]any
		want string
	}{
		{
			name: "zh origin",
			lang: ZH,
			key:  "baseChangedHint",
			args: map[string]any{"origin": "origin/main"},
			want: "本会话从 origin/main 分出，换基线会把它上面的提交也算进来",
		},
		{
			name: "en origin",
			lang: EN,
			key:  "baseChangedHint",
			args: map[string]any{"origin": "origin/main"},
			want: "This chat forked from origin/main; another base includes its commits",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useLang(t, tc.lang)
			if got := TF(tc.key, tc.args); got != tc.want {
				t.Fatalf("TF(%q) = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
}

func TestRender(t *testing.T) {
	cases := []struct {
		name string
		in   string
		args map[string]any
		want string
	}{
		{"single", "from {origin}", map[string]any{"origin": "main"}, "from main"},
		{"two", "{a} then {b}", map[string]any{"a": "1", "b": "2"}, "1 then 2"},
		{"repeated", "{n} of {n}", map[string]any{"n": 3}, "3 of 3"},
		{"missing arg stays literal", "from {origin}", map[string]any{"base": "main"}, "from {origin}"},
		{"no args", "from {origin}", nil, "from {origin}"},
		{"unterminated brace", "from {origin", map[string]any{"origin": "main"}, "from {origin"},
		{"value not re-expanded", "{a}", map[string]any{"a": "{b}", "b": "boom"}, "{b}"},
		{"non-string value", "{n} files", map[string]any{"n": 12}, "12 files"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := render(tc.in, tc.args); got != tc.want {
				t.Fatalf("render(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestMissingKeyFallsBackToEnglishOnce(t *testing.T) {
	useLang(t, ZH)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	const key = "this.key.does.not.exist"
	for i := 0; i < 3; i++ {
		if got := T(key); got != key {
			t.Fatalf("T(%q) = %q, want the key itself", key, got)
		}
	}
	if n := strings.Count(buf.String(), fmt.Sprintf("i18n: missing key %q", key)); n != 1 {
		t.Fatalf("logged %d times, want exactly 1:\n%s", n, buf.String())
	}
}

func TestSetAndCurrent(t *testing.T) {
	useLang(t, EN)
	if got := Current(); got != EN {
		t.Fatalf("Current = %q, want %q", got, EN)
	}
	if got := T("sessions"); got != "Sessions" {
		t.Fatalf("T(sessions) = %q in en", got)
	}
	Set(ZH)
	if got := T("sessions"); got != "会话" {
		t.Fatalf("T(sessions) = %q after Set(ZH)", got)
	}
	// Only the two supported languages exist; anything else keeps English.
	Set(Lang("fr"))
	if got := Current(); got != EN {
		t.Fatalf("Current = %q after Set(fr), want %q", got, EN)
	}
}

func TestDetectLang(t *testing.T) {
	cases := []struct {
		lang string
		want Lang
	}{
		{"zh_CN.UTF-8", ZH},
		{"zh_TW.UTF-8", ZH},
		{"en_US.UTF-8", EN},
		{"", EN},
	}
	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			t.Setenv("LANG", tc.lang)
			if got := detectLang(); got != tc.want {
				t.Fatalf("detectLang() with LANG=%q = %q, want %q", tc.lang, got, tc.want)
			}
		})
	}
}
