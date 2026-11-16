package app

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/theme"
)

// composerTester opens the composer with the caret in the text area.
func composerTester(t *testing.T, a *App) *ui.Tester {
	t.Helper()
	tt := ui.NewTester(func(c *ui.Context) { a.composer(c) }, 480, 180)
	tt.Frame()
	tt.ClickAt(40, 40)
	tt.Frame()
	return tt
}

// TestPasteInsertsClipboardText is the clipboard path: Cmd+V puts the system
// clipboard into the draft, which is how a log or a path gets in.
func TestPasteInsertsClipboardText(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	a.current = 7
	tt := composerTester(t, a)

	tt.SetClipboard("see /tmp/pitta-live.png")
	tt.Key(ui.Super, ui.KeyV)
	tt.Frame()
	if a.draft != "see /tmp/pitta-live.png" {
		t.Fatalf("draft = %q", a.draft)
	}
}

// TestSelectAllAndBackspaceClears is the selection path: Cmd+A then Backspace
// empties the draft, and typing replaces nothing stale.
func TestSelectAllAndBackspaceClears(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	a.current = 7
	tt := composerTester(t, a)

	tt.Type("hello world")
	tt.Frame()
	tt.Key(ui.Super, ui.KeyA)
	tt.Key(0, ui.KeyBackspace)
	tt.Frame()
	if a.draft != "" {
		t.Fatalf("draft after select-all delete = %q", a.draft)
	}
	tt.Type("fresh")
	tt.Frame()
	if a.draft != "fresh" {
		t.Fatalf("draft after retype = %q", a.draft)
	}

	// Shift+Left selects one character; the next keystroke replaces it.
	tt.Key(ui.Shift, ui.KeyLeft)
	tt.Type("X")
	tt.Frame()
	if a.draft != "fresX" {
		t.Fatalf("draft after replacing a selection = %q", a.draft)
	}
}

// TestIMECompositionDoesNotSend is the Chinese input path: a preedit is not
// part of the draft, Enter during composition neither sends nor inserts a
// newline, and the committed characters land in the draft.
func TestIMECompositionDoesNotSend(t *testing.T) {
	srv, sent := composerServer(t)
	a := New(Config{BaseURL: srv.URL})
	a.previewImage = -1
	a.current = 7
	tt := composerTester(t, a)

	tt.Compose("nihao", 5)
	tt.Frame()
	if a.draft != "" {
		t.Fatalf("preedit leaked into the draft: %q", a.draft)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	select {
	case got := <-sent:
		t.Fatalf("Enter during composition sent %q", got.Text)
	default:
	}
	// Typing while a composition is marked commits it, as the input method
	// does when the candidate is chosen.
	tt.Compose("nihao", 5)
	tt.Frame()
	tt.Type("你好")
	tt.Frame()
	if a.draft != "你好" {
		t.Fatalf("draft after commit = %q", a.draft)
	}

	// Cmd+Enter sends what is committed, as the earlier client composer does.
	tt.Key(ui.Super, ui.KeyEnter)
	tt.Frame()
	select {
	case got := <-sent:
		if got.Text != "你好" {
			t.Fatalf("sent %q", got.Text)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cmd+Enter did not send")
	}
}

// TestSelectAllHighlightsText is the pixel side of selection: mygo paints the
// highlight only while the editor has focus, and it must use the theme's
// Selection — which the theme used to set to the accent color, the near-white
// and near-black surface color, so a selected draft looked unselected.
func TestSelectAllHighlightsText(t *testing.T) {
	for _, dark := range []bool{false, true} {
		tok := theme.Light()
		if dark {
			tok = theme.Dark()
		}
		a := New(Config{})
		a.previewImage = -1
		a.current = 7
		a.draft = "select me"
		// The palette has to be the app's own: mygo's default Selection is
		// blue, so a bare tester would pass without the theme doing anything.
		tt := ui.NewTester(func(c *ui.Context) {
			theme.Apply(c, tok)
			a.composer(c)
		}, 480, 180)
		tt.SetDark(dark)
		tt.ClickAt(40, 40)
		tt.Frame()
		before := tt.Image()

		tt.Key(ui.Super, ui.KeyA)
		tt.Frame()
		after := tt.Image()

		if n := highlighted(before, after, tok.Selection); n < 400 {
			t.Fatalf("dark=%v: %d pixels changed to the selection color; the highlight is not painted",
				dark, n)
		}
	}
}

// highlighted counts pixels that changed from before to the selection color
// drawn over what they were.
func highlighted(before, after *image.RGBA, sel ui.Color) int {
	if before.Bounds() != after.Bounds() {
		return 0
	}
	n := 0
	for i := 0; i < len(before.Pix); i += 4 {
		under := ui.RGBA(before.Pix[i], before.Pix[i+1], before.Pix[i+2], 0xff)
		want := theme.Composite(sel, under)
		if nearColor(after.Pix[i], want.R) && nearColor(after.Pix[i+1], want.G) && nearColor(after.Pix[i+2], want.B) && !nearColor(after.Pix[i], before.Pix[i]) {
			n++
		}
	}
	return n
}
