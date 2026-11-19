package app

import (
	"strings"
	"testing"

	"pitta/internal/i18n"

	"github.com/egoist/mygo/ui"
)

// TestPaletteMatchFindsWhatWasTyped: the query is a subsequence with a bonus for
// a letter that starts a word, which is what makes "dlchk" find a chat called
// "Deploy checklist".
func TestPaletteMatchFindsWhatWasTyped(t *testing.T) {
	cases := []struct {
		name  string
		query string
		text  string
		want  bool
	}{
		{name: "empty query keeps everything", query: "", text: "anything", want: true},
		{name: "plain substring", query: "sky", text: "Why is the sky blue", want: true},
		{name: "word starts", query: "dc", text: "Deploy checklist", want: true},
		{name: "in order but scattered", query: "dpl", text: "Deploy checklist", want: true},
		{name: "out of order", query: "psd", text: "Deploy checklist", want: false},
		{name: "absent", query: "zzz", text: "Deploy checklist", want: false},
		{name: "case folded", query: "SKY", text: "Why is the sky blue", want: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := paletteMatch(c.query, c.text); ok != c.want {
				t.Fatalf("paletteMatch(%q, %q) = %v, want %v", c.query, c.text, ok, c.want)
			}
		})
	}

	// A run of adjacent letters beats the same letters scattered: "sky" in a
	// title that says "sky" should outrank one that only spells it out.
	close, _ := paletteMatch("sky", "the sky is blue")
	far, _ := paletteMatch("sky", "some kind of yellowish")
	if close <= far {
		t.Fatalf("adjacent letters scored %d, scattered scored %d", close, far)
	}
}

// TestPaletteRowsAnswerWithChatsAndVerbs: with nothing typed the palette offers
// the verbs, and a few letters land on the chat that carries them.
func TestPaletteRowsAnswerWithChatsAndVerbs(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)

	a.openPalette()
	rows := a.paletteRows()
	if len(rows) == 0 {
		t.Fatal("empty palette offers nothing")
	}
	if rows[0].slug != "verb.new" {
		t.Fatalf("first row is %q, want the chat verb", rows[0].slug)
	}
	verbs := 0
	for _, row := range rows {
		if strings.HasPrefix(row.slug, "verb.") {
			verbs++
		}
	}
	if verbs != len(a.paletteVerbs()) {
		t.Fatalf("palette shows %d verbs of %d", verbs, len(a.paletteVerbs()))
	}

	a.palette.query = "sky"
	rows = a.paletteRows()
	if len(rows) == 0 || rows[0].slug != "chat.11" {
		t.Fatalf("typing the title did not find the chat: %+v", rows)
	}

	// Two verbs share the word "export" now, so what matters is that the one
	// being typed for is among the answers, not that it comes first.
	a.palette.query = "export"
	rows = a.paletteRows()
	found := false
	for _, row := range rows {
		if row.slug == "verb.export" {
			found = true
		}
	}
	if !found {
		t.Fatalf("typing the verb did not find it: %+v", rows)
	}

	a.palette.query = "qwertyuiop"
	if rows = a.paletteRows(); len(rows) != 0 {
		t.Fatalf("a query nothing matches still offered %d rows", len(rows))
	}
}

// TestPaletteOpensFromTheKeyboard: ⌘K is the way in, and typing there narrows to
// the chat the reader named; ⏎ opens it.
func TestPaletteOpensFromTheKeyboard(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()

	tt.Key(ui.Super, ui.KeyK)
	tt.Frame()
	if !a.palette.open {
		t.Fatal("⌘K did not open the palette")
	}
	if !tt.HasText(i18n.T("paletteHint")) {
		t.Fatalf("palette did not draw: %v", tt.Texts())
	}

	tt.Type("deploy")
	tt.Frame()
	if !tt.HasText("Deploy checklist for the 0.4 train") {
		t.Fatalf("typed title not drawn: %v", tt.Texts())
	}

	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.palette.open {
		t.Fatal("⏎ left the palette open")
	}
	if a.active != 12 {
		t.Fatalf("⏎ opened chat %d, want 12", a.active)
	}
}

// TestPaletteArrowKeysWalkTheRows: ↓ and ↑ move the highlight and wrap, so the
// hand never has to reach for the mouse mid-query.
func TestPaletteArrowKeysWalkTheRows(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	tt.Key(ui.Super, ui.KeyK)
	tt.Frame()

	rows := a.paletteRows()
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.palette.index != 1 {
		t.Fatalf("↓ left the highlight on %d", a.palette.index)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if a.palette.index != 0 {
		t.Fatalf("↑ left the highlight on %d", a.palette.index)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if a.palette.index != len(rows)-1 {
		t.Fatalf("↑ from the top went to %d of %d", a.palette.index, len(rows))
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.palette.open {
		t.Fatal("esc left the palette open")
	}
}
