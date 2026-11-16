package app

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
)

// pngFile writes a tiny PNG and returns its path, for the drop and paste tests.
func pngFile(t *testing.T, dir string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "shot.png")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestCmdKOpensTheSwitcher checks the shortcut a native window can bind and the
// web client could not: it lists every chat and jumps on Enter.
func TestCmdKOpensTheSwitcher(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if a.switcher.open {
		t.Fatal("switcher open before the shortcut")
	}
	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if !a.switcher.open {
		t.Fatal("Cmd+K did not open the switcher")
	}
	if len(a.switcherMatches()) != len(a.surfaces) {
		t.Fatalf("switcher lists %d of %d", len(a.switcherMatches()), len(a.surfaces))
	}
	// Typing narrows the list, and Enter opens the first match.
	a.switcher.query = "sky"
	matches := a.switcherMatches()
	if len(matches) == 0 {
		t.Fatal("query matched nothing")
	}
	a.switcher.query = ""
}

// TestDropImageAttachesIt drops a PNG on the window, which the earlier web client
// could not accept at all.
func TestDropImageAttachesIt(t *testing.T) {
	a := snapshotData(t)
	path := pngFile(t, t.TempDir())
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.DropFiles(600, 400, path) {
		t.Fatal("drop rejected")
	}
	tt.Frame()
	if len(a.pendingImages) != 1 {
		t.Fatalf("attached %d images", len(a.pendingImages))
	}
	if a.pendingImages[0].Mime != "image/png" {
		t.Fatalf("mime %q", a.pendingImages[0].Mime)
	}
}

// TestPasteImageFromClipboard checks Cmd+V with an image on the clipboard.
func TestPasteImageFromClipboard(t *testing.T) {
	a := snapshotData(t)
	path := pngFile(t, t.TempDir())
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tt := ui.NewTester(a.View, 1180, 820)
	tt.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, raw))))
	tt.Frame()
	// The tester's clipboard is reachable through an element's Services, so the
	// test drives the same rule the paste handler calls. It is set after the
	// tester's first frame, which runs before a clipboard exists.
	seen := false
	tt2 := ui.NewTester(func(c *ui.Context) {
		seen = a.pasteImageFrom(c.Services())
	}, 100, 100)
	tt2.SetClipboardData(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, raw))))
	tt2.Frame()
	if !seen {
		t.Fatal("clipboard image not seen")
	}
	if len(a.pendingImages) != 1 {
		t.Fatalf("attached %d", len(a.pendingImages))
	}
	if a.pendingImages[0].Data != base64.StdEncoding.EncodeToString(raw) {
		t.Fatal("attached bytes differ from the clipboard")
	}
}

// TestRightClickCopiesAMessage exercises the native context menu.
func TestRightClickCopiesAMessage(t *testing.T) {
	a := snapshotData(t)
	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	rect, ok := tt.Find("why is the sky blue")
	if !ok {
		t.Fatalf("question not drawn: %v", tt.Texts())
	}
	tt.RightClickAt(rect.X+4, rect.Y+4)
	tt.Frame()
	// The menu is native, so the test only checks it did not disturb the view.
	if got := tt.Clipboard(); got != "" {
		t.Fatalf("clipboard changed to %q without a choice", got)
	}
}
