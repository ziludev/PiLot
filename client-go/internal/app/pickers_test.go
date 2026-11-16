package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"pitta/internal/protocol"
)

// TestAttachUsesTheSystemPanel checks the picker flow: the panel returns paths
// and each image becomes an attachment, as the paperclip does.
func TestAttachUsesTheSystemPanel(t *testing.T) {
	a := New(Config{})
	a.previewImage = -1
	path := pngFile(t, t.TempDir())
	SetPicker(func(opts mygo.OpenDialogOptions) ([]string, error) {
		if len(opts.Filters) == 0 || opts.Filters[0].MIMETypes[0] != "image/*" {
			t.Errorf("unexpected filters %+v", opts.Filters)
		}
		return []string{path}, nil
	})
	t.Cleanup(func() { SetPicker(nil) })
	a.chooseImages()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(a.pendingImages) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if len(a.pendingImages) != 1 {
		t.Fatalf("attached %d images", len(a.pendingImages))
	}
}

// TestProjectPanelAsksForADirectory checks the workspace flow.
func TestProjectPanelAsksForADirectory(t *testing.T) {
	var opened []protocol.ProjectOpen
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/projects" {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		opened = append(opened, protocol.ProjectOpen{Status: "added"})
		_ = json.NewEncoder(w).Encode(protocol.ProjectOpen{Status: "added"})
	}))
	defer server.Close()
	a := New(Config{BaseURL: server.URL})
	a.previewImage = -1
	SetPicker(func(opts mygo.OpenDialogOptions) ([]string, error) {
		if !opts.Directory {
			t.Error("panel should ask for a directory")
		}
		return []string{"/tmp/workspace"}, nil
	})
	t.Cleanup(func() { SetPicker(nil) })
	a.chooseProject()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(opened) == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if len(opened) != 1 {
		t.Fatalf("project posts: %d", len(opened))
	}
}
