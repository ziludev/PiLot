package icons

import "testing"

// TestEveryIconParses guards the embedded SVGs: a typo would otherwise only
// show as a blank spot at runtime.
func TestEveryIconParses(t *testing.T) {
	for _, icon := range []struct {
		name string
		svg  interface {
			Size() (float32, float32)
		}
	}{
		{"Paperclip", Paperclip}, {"Send", Send}, {"Square", Square},
		{"ChevronDown", ChevronDown}, {"ChevronRight", ChevronRight},
		{"Check", Check}, {"Plus", Plus}, {"Search", Search}, {"X", X},
		{"MessagePlus", MessagePlus}, {"ShieldAlert", ShieldAlert},
		{"AlertTriangle", AlertTriangle}, {"Clock", Clock},
		{"RefreshCw", RefreshCw}, {"PlugZap", PlugZap},
	} {
		if w, h := icon.svg.Size(); w != 24 || h != 24 {
			t.Fatalf("%s: size %vx%v, want 24x24", icon.name, w, h)
		}
	}
}
