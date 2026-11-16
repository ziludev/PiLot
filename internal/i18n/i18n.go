// Package i18n carries the UI strings of the React client so the Go side can
// phrase the same messages in the same two languages.
package i18n

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
)

// Lang is a supported UI language.
type Lang string

const (
	EN Lang = "en"
	ZH Lang = "zh"
)

var (
	mu      sync.RWMutex
	current = detectLang()

	// A missing key is a bug, not user input; report each one once instead of
	// flooding the log of a running session.
	missing sync.Map
)

// detectLang stands in for the browser's initialLang: there is no ?lang query
// and no localStorage here, so the process locale is the closest equivalent of
// navigator.language. Anything that is not zh keeps the English default.
func detectLang() Lang {
	if strings.HasPrefix(strings.ToLower(os.Getenv("LANG")), "zh") {
		return ZH
	}
	return EN
}

// Set overrides the language for the rest of the process; a caller that wants a
// persisted choice reads it themselves and calls Set at startup.
func Set(l Lang) {
	mu.Lock()
	if l == ZH {
		current = ZH
	} else {
		current = EN
	}
	mu.Unlock()
}

// Current reports the language in use.
func Current() Lang {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T renders key in the current language without arguments.
func T(key string) string {
	return TF(key, nil)
}

// TF renders key in the current language, substituting "{name}" with
// args[name]. Placeholders without a matching argument are left untouched, the
// same as the client leaving "{origin}" in place when it does not replace it.
func TF(key string, args map[string]any) string {
	table := en
	if Current() == ZH {
		table = zh
	}
	s, ok := table[key]
	if !ok {
		if s, ok = en[key]; !ok {
			if _, seen := missing.LoadOrStore(key, struct{}{}); !seen {
				log.Printf("i18n: missing key %q", key)
			}
			return key
		}
	}
	return render(s, args)
}

// render makes one pass over s so a substituted value can never be expanded
// again, and so the result does not depend on map iteration order.
func render(s string, args map[string]any) string {
	if len(args) == 0 {
		return s
	}
	var b strings.Builder
	for {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(s[open:], '}')
		if end < 0 {
			break
		}
		b.WriteString(s[:open])
		if v, ok := args[s[open+1:open+end]]; ok {
			fmt.Fprint(&b, v)
		} else {
			b.WriteString(s[open : open+end+1])
		}
		s = s[open+end+1:]
	}
	b.WriteString(s)
	return b.String()
}

// Keys lists every key, sorted, for whole-dictionary checks.
func Keys() []string {
	keys := make([]string, 0, len(zh))
	for k := range zh {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
