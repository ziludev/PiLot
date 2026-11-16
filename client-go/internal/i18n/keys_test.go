package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// keyUse matches a literal translation key: i18n.T("send"), i18n.TF("waited", …).
// The chat menu once asked for "renameChat" and "archiveChat", keys the earlier client
// dictionary never had, so the window drew the key itself where a label
// belonged. This test walks the sources and refuses a key that is not there.
var keyUse = regexp.MustCompile(`i18n\.(?:T|TF)\("([^"]+)"`)

func TestEveryLiteralKeyExists(t *testing.T) {
	root := filepath.Join("..", "..")
	checked := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if name := info.Name(); name == "node_modules" || name == "build" || strings.HasPrefix(name, ".") && name != "." && name != ".." {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, string(filepath.Separator)+"i18n"+string(filepath.Separator)) {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range keyUse.FindAllStringSubmatch(string(src), -1) {
			key := m[1]
			checked++
			if _, ok := en[key]; !ok {
				t.Errorf("%s: key %q is not in the dictionary", path, key)
			}
			if _, ok := zh[key]; !ok {
				t.Errorf("%s: key %q has no zh string", path, key)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	if checked < 80 {
		t.Fatalf("checked %d keys, expected the app's ~100; the walk missed the sources", checked)
	}
}
