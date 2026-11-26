package app

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"pilot/internal/i18n"
)

// The keys the package draws, read from its own source: T("x") and TF("x", ...).
// A key that is used but missing from the table renders as itself, so a window
// shows "taskStopped" where a sentence should be — and a test that asserts
// i18n.T("taskStopped") passes either way, because both sides are the key.
var keyUse = regexp.MustCompile(`i18n\.T(?:F)?\("([A-Za-z0-9]+)"`)

func TestEveryKeyTheWindowDrawsIsInTheTable(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	missing := map[string][]string{}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range keyUse.FindAllStringSubmatch(string(body), -1) {
			if !i18n.IsKey(match[1]) {
				missing[match[1]] = append(missing[match[1]], file)
			}
		}
	}
	if len(missing) == 0 {
		return
	}
	keys := make([]string, 0, len(missing))
	for key := range missing {
		keys = append(keys, key+" ("+strings.Join(missing[key], ", ")+")")
	}
	sort.Strings(keys)
	t.Fatalf("keys drawn but not in the table: %s", strings.Join(keys, "; "))
}
