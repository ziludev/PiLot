package app

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
)

// solidPNG is a picture with something in it: a gradient, so a render that
// silently draws nothing cannot pass for one that drew.
func solidPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: uint8(30 + x*180/w), G: uint8(90 + y*120/h), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x += 7 {
		img.Set(x, 0, color.RGBA{R: 255, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return buf.Bytes()
}

// TestZoomSizeShrinksToFit: a tall screenshot is the case the overlay exists
// for, and it has to fit the window rather than run off it.
func TestZoomSizeShrinksToFit(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	bitmap, err := ui.DecodeBitmapScaled(pngOf(t, 2400, 1200), zoomEdge)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	w, h := bitmap.Size()
	if w > zoomEdge || h > zoomEdge {
		t.Fatalf("decoded %dx%d, larger than %d", w, h, zoomEdge)
	}
	a.zoom.bitmap = bitmap

	zw, zh := a.zoomSize()
	if zw > 920 || zh > 640 {
		t.Fatalf("overlay %vx%v runs off its box", zw, zh)
	}
	if ratio := zw / zh; ratio < 1.9 || ratio > 2.1 {
		t.Fatalf("ratio %v, want the picture's own 2:1", ratio)
	}
}

// TestZoomSizeNeverEnlarges: a small picture blown up is a blurry picture, and
// the thumbnail already told that story.
func TestZoomSizeNeverEnlarges(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	bitmap, err := ui.DecodeBitmap(pngOf(t, 120, 60))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	a.zoom.bitmap = bitmap

	zw, zh := a.zoomSize()
	if zw != 120 || zh != 60 {
		t.Fatalf("overlay %vx%v, want the picture's own 120x60", zw, zh)
	}
}

func TestHumanBytesReadsLikeACaption(t *testing.T) {
	for n, want := range map[int]string{
		512:        "512 B",
		3 << 10:    "3 KB",
		5 << 20:    "5.0 MB",
		15 << 20:   "15.0 MB",
		1200 << 10: "1.2 MB",
	} {
		if got := humanBytes(n); got != want {
			t.Fatalf("humanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

// TestDividerRowSitsAboveWhatArrived: the line is drawn from the newest entry the
// user had on screen, so it lands above the first row written after it.
func TestDividerRowSitsAboveWhatArrived(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	rows := []threadRow{{entry: 3}, {entry: 4}, {entry: 7}, {entry: 8}}

	a.vm.newFrom[12] = 4
	if got := a.dividerRow(12, rows); got != 2 {
		t.Fatalf("divider at %d, want 2", got)
	}
	// Everything is newer: a line above the first row separates nothing.
	a.vm.newFrom[12] = 2
	if got := a.dividerRow(12, rows); got != -1 {
		t.Fatalf("divider at %d, want none", got)
	}
	// Nothing arrived, and nothing has been read here yet.
	a.vm.newFrom[12] = 8
	if got := a.dividerRow(12, rows); got != -1 {
		t.Fatalf("divider at %d, want none", got)
	}
	if got := a.dividerRow(99, rows); got != -1 {
		t.Fatalf("divider for an unread chat at %d, want none", got)
	}
}

// TestSeenFollowsTheOpenChatOnly: the bridge's frames for other chats pass
// through the same handler, and counting those as read would erase the line.
func TestSeenFollowsTheOpenChatOnly(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	a.active = 12
	a.noteSeen(12, 40)
	a.noteSeen(13, 90)
	if a.vm.seen[12] != 40 {
		t.Fatalf("chat 12 seen = %d", a.vm.seen[12])
	}
	if a.vm.seen[13] != 0 {
		t.Fatalf("a chat that was not open counted as read: %d", a.vm.seen[13])
	}
	// Frames arriving out of order do not walk the mark backwards.
	a.noteSeen(12, 30)
	if a.vm.seen[12] != 40 {
		t.Fatalf("seen went backwards: %d", a.vm.seen[12])
	}
}

// TestDividerShowsInTheTranscript: the line is a view of state, not a row the
// projection builds, so the only proof it lands anywhere is a render.
func TestDividerShowsInTheTranscript(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	// Read up to the first answer; the sunset question and the error came later.
	a.vm.seen[11] = 44
	a.vm.newFrom[11] = 44

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("新消息") {
		t.Fatalf("no line drawn; texts = %v", tt.Texts())
	}
	writePNG(t, "/tmp/pitta-divider.png", tt.Image())

	// With nothing new the line is gone again.
	a.vm.newFrom[11] = 48
	tt2 := ui.NewTester(a.View, 1180, 820)
	tt2.Frame()
	if tt2.HasText("新消息") {
		t.Fatalf("line survived with nothing new")
	}
}

// TestZoomOverlayDraws: the overlay is a modal over the whole window, which is
// the one thing a unit test cannot guess — it has to be seen.
func TestZoomOverlayDraws(t *testing.T) {
	i18n.Set(i18n.ZH)
	a := fixtureApp(t, false)
	bitmap, err := ui.DecodeBitmapScaled(solidPNG(t, 600, 340), zoomEdge)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	a.zoom = zoomState{open: true, bitmap: bitmap, caption: "image/png · 42 KB"}

	tt := ui.NewTester(a.View, 1180, 820)
	tt.Frame()
	if !tt.HasText("image/png · 42 KB") {
		t.Fatalf("overlay drew no caption; texts = %v", tt.Texts())
	}
	if !tt.HasText("关闭") {
		t.Fatalf("overlay has no way out")
	}
	writePNG(t, "/tmp/pitta-zoom.png", tt.Image())
}

// TestRememberKeepsTheReadMarkAcrossRestarts: the line is only useful if it
// survives the rebuild that the client gets every time it is installed.
func TestRememberKeepsTheReadMarkAcrossRestarts(t *testing.T) {
	i18n.Set(i18n.ZH)
	dir := t.TempDir()
	saved := prefsPath
	prefsPath = func() string { return filepath.Join(dir, "window.json") }
	t.Cleanup(func() { prefsPath = saved })

	a := fixtureApp(t, false)
	a.active = 12
	a.noteSeen(12, 88)
	a.remember(12)

	deadline := time.Now().Add(3 * time.Second)
	for readPrefs().Seen["12"] != 88 {
		if time.Now().After(deadline) {
			t.Fatalf("read mark never reached the file")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// And it does not walk backwards when an older mark is written later.
	a.vm.seen[12] = 80
	a.remember(12)
	if got := readPrefs().Seen["12"]; got != 88 {
		t.Fatalf("read mark went backwards: %d", got)
	}
}
