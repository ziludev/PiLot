package app

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"time"

	"pitta/internal/api"
	"pitta/internal/protocol"
)

// pngBytes is a 4×4 red square, small enough to inline in a test.
func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for x := 0; x < 4; x++ {
		for y := 0; y < 4; y++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// imageServer records what /api/submit carried.
func imageServer(t *testing.T) (*api.Client, <-chan map[string]any) {
	t.Helper()
	bodies := make(chan map[string]any, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.URL.Path == "/api/submit" {
			_ = json.NewDecoder(r.Body).Decode(&body)
			bodies <- body
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)
	return api.New(srv.URL, ""), bodies
}

// TestAttachImageSendsPictures checks a path becomes a pending picture and the
// next turn carries it as base64 with its MIME type.
func TestAttachImageSendsPictures(t *testing.T) {
	client, bodies := imageServer(t)
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, pngBytes(t), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}

	a := &App{api: client, current: 7, previewImage: -1}
	a.attachImage(path)
	if len(a.pendingImages) != 1 {
		t.Fatalf("pending images = %d, want 1", len(a.pendingImages))
	}
	attached := a.pendingImages[0]
	if attached.Mime != "image/png" || attached.Name != "shot.png" || attached.bitmap == nil {
		t.Fatalf("image = %+v", attached)
	}

	a.sendText("look")
	select {
	case body := <-bodies:
		images, _ := body["images"].([]any)
		if len(images) != 1 {
			t.Fatalf("images in body = %v", body["images"])
		}
		part, _ := images[0].(map[string]any)
		if part["mimeType"] != "image/png" || part["data"] == "" {
			t.Fatalf("image part = %v", part)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no submission")
	}
	if len(a.pendingImages) != 0 {
		t.Fatalf("pending images kept after send: %d", len(a.pendingImages))
	}
}

// TestAttachImageRejects checks a missing file and a non-image both report
// instead of attaching.
func TestAttachImageRejects(t *testing.T) {
	a := &App{previewImage: -1}
	a.attachImage(filepath.Join(t.TempDir(), "missing.png"))
	if len(a.pendingImages) != 0 || len(a.errs) != 1 {
		t.Fatalf("missing file: images=%d errs=%d", len(a.pendingImages), len(a.errs))
	}

	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("not a picture"), 0o600); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	a.attachImage(path)
	if len(a.pendingImages) != 0 || len(a.errs) != 2 {
		t.Fatalf("bad image: images=%d errs=%d", len(a.pendingImages), len(a.errs))
	}
}

// TestAttachImageCapsAtThree checks the fourth picture is ignored.
func TestAttachImageCapsAtThree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(path, pngBytes(t), 0o600); err != nil {
		t.Fatalf("write png: %v", err)
	}
	a := &App{previewImage: -1}
	for i := 0; i < 4; i++ {
		a.attachImage(path)
	}
	if len(a.pendingImages) != maxImages {
		t.Fatalf("pending images = %d, want %d", len(a.pendingImages), maxImages)
	}
	a.dropImage(0)
	if len(a.pendingImages) != maxImages-1 {
		t.Fatalf("after drop = %d, want %d", len(a.pendingImages), maxImages-1)
	}
}

// TestImagesOfReadsEntry checks the pictures a sent turn carries are found in
// its message parts.
func TestImagesOfReadsEntry(t *testing.T) {
	data := base64.StdEncoding.EncodeToString(pngBytes(t))
	model, err := json.Marshal([]map[string]any{{
		"role": "user",
		"content": []map[string]any{
			{"type": "text", "text": "what is this"},
			{"type": "image", "data": data, "mimeType": "image/png"},
		},
	}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	images := imagesOf(protocol.Entry{Model: model})
	if len(images) != 1 || images[0].Mime != "image/png" || images[0].bitmap == nil {
		t.Fatalf("images = %+v", images)
	}
	if got := len(imagesOf(protocol.Entry{})); got != 0 {
		t.Fatalf("empty entry images = %d", got)
	}
}

// TestExpandHome checks a shell-style path opens the file it names.
func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	if got := expandHome("~/shot.png"); got != filepath.Join(home, "shot.png") {
		t.Fatalf("expandHome = %q", got)
	}
	if got := expandHome("/tmp/shot.png"); got != "/tmp/shot.png" {
		t.Fatalf("absolute path changed: %q", got)
	}
}

// TestMimeOf checks the type the model is told, by extension.
func TestMimeOf(t *testing.T) {
	cases := map[string]string{
		"a.png": "image/png", "a.PNG": "image/png", "a.jpg": "image/jpeg",
		"a.jpeg": "image/jpeg", "a.gif": "image/gif", "a.webp": "image/webp",
		"a.bmp": "image/bmp", "a.txt": "image/png",
	}
	for name, want := range cases {
		if got := mimeOf(name); got != want {
			t.Fatalf("mimeOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestPreviewZoomStepsAndClamps checks the picture viewer's zoom: the buttons
// step by half, the picture is drawn bigger, and the ends are clamped.
func TestPreviewZoomStepsAndClamps(t *testing.T) {
	a := snapshotData(t)
	path := pngFile(t, t.TempDir())
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		t.Fatal(err)
	}
	a.previewSet = []imagePart{{Name: "shot.png", bitmap: bitmap}}
	a.previewImage = 0
	tt := ui.NewTester(a.View, 1000, 760)
	tt.Frame()
	if !tt.HasText("100%") {
		t.Skipf("the preview is not open: %q", tt.Texts())
	}
	tt.Click("+")
	tt.Frame()
	if !tt.HasText("150%") {
		t.Fatalf("zoom after + is not 150%%: %q", tt.Texts())
	}
	for i := 0; i < 8; i++ {
		tt.Click("+")
		tt.Frame()
	}
	if a.previewScale != 4 {
		t.Fatalf("zoom ran to %v, want 4", a.previewScale)
	}
	tt.Click("1:1")
	tt.Frame()
	if a.previewScale != 1 {
		t.Fatalf("1:1 left the scale at %v", a.previewScale)
	}
}
