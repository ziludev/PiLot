package app

import (
	"testing"

	"pitta/internal/api"
)

// TestParseDeepLink covers the shapes a link can arrive in, and the ones that
// must be refused instead of guessed.
func TestParseDeepLink(t *testing.T) {
	ok := map[string]DeepLink{
		"pitta://chat/123":            {ChatID: 123},
		"pitta://chat/123/":           {ChatID: 123},
		"pitta:///chat/123":           {ChatID: 123},
		"PITTA://chat/9753":           {ChatID: 9753},
		"pitta://chat/7?window=new":   {ChatID: 7, NewWindow: true},
		"pitta://chat/7?window=false": {ChatID: 7},
	}
	for raw, want := range ok {
		got, good := ParseDeepLink(raw)
		if !good {
			t.Fatalf("%q was refused", raw)
		}
		if got != want {
			t.Fatalf("%q parsed to %+v, want %+v", raw, got, want)
		}
	}
	for _, raw := range []string{
		"", "https://example.com/chat/1", "pitta://", "pitta://chat/",
		"pitta://chat/abc", "pitta://chat/0", "pitta://chat/-3", "pitta://project/1",
	} {
		if link, good := ParseDeepLink(raw); good {
			t.Fatalf("%q was read as %+v", raw, link)
		}
	}
}

// TestOpenDeepLinkPicksTheWindow checks ?window=new goes through the opener and
// a plain link lands in the current window.
func TestOpenDeepLinkPicksTheWindow(t *testing.T) {
	a := snapshotData(t)
	a.previewImage = -1
	a.api = quietAPI(t)
	opened := 0
	saved := windowOpener
	defer func() { windowOpener = saved }()
	SetWindowOpener(func(cfg Config, client *api.Client, chatID int) { opened = chatID })
	a.OpenDeepLink("pitta://chat/13?window=new")
	if opened != 13 {
		t.Fatalf("opened window for %d, want 13", opened)
	}
	a.OpenDeepLink("pitta://chat/12")
	if a.currentValue() != 12 {
		t.Fatalf("current chat %d, want 12", a.currentValue())
	}
}
