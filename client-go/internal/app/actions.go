package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"pitta/internal/api"
	"pitta/internal/bridge"
	"pitta/internal/i18n"
)

func (a *App) submit() {
	text := strings.TrimSpace(a.draft)
	images := a.draftImage
	convID := a.active
	if convID == 0 || (text == "" && len(images) == 0) {
		return
	}
	if text != "" {
		a.history = append(a.history, text)
		if len(a.history) > 50 {
			a.history = a.history[len(a.history)-50:]
		}
	}
	a.histIndex = len(a.history)
	a.draft, a.draftImage = "", nil
	request := api.SubmitRequest{
		ConversationID: convID,
		Text:           text,
		Mode:           api.ModeFollowUp,
		Images:         attachmentImages(images),
		RequestID:      fmt.Sprintf("%d-%d", convID, time.Now().UnixNano()),
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		defer cancel()
		if err := a.api.Submit(ctx, request); err != nil {
			a.post(event{kind: evNotice, notice: i18n.T("sendFailed") + " · " + err.Error()})
			return
		}
		a.loadThread(convID)
	}()
}

// abort stops the active chat's run and clears whatever is queued behind it:
// pressing stop and then watching three more turns go by is the same stop
// failing, just slower.
func (a *App) abort() {
	convID := a.active
	if convID == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		outcome, err := a.api.Abort(ctx, convID, true)
		if err != nil {
			a.post(event{kind: evNotice, notice: i18n.T("stopFailed") + " · " + err.Error()})
			return
		}
		if outcome.Cancelled > 0 {
			a.post(event{kind: evNotice, notice: i18n.TF("stopCleared", map[string]any{"n": outcome.Cancelled})})
		}
	}()
}

// decide answers one pending approval. The protocol addresses a decision by
// call id alone; approvals are global, not tied to the chat on screen.
func (a *App) decide(callID string, approved bool) {
	if callID == "" || a.busy[callID] {
		return
	}
	a.busy[callID] = true
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
		defer cancel()
		err := a.api.DecideApproval(ctx, api.ApprovalDecision{CallID: callID, Approved: approved})
		if err != nil {
			a.post(event{kind: evNotice, notice: i18n.T("approvalFailed") + " · " + err.Error()})
		}
		a.post(event{kind: evDecided, callID: callID})
		a.requestRefresh()
	}()
}

// newChat starts an empty chat in the built-in workspace and opens it.
func (a *App) newChat() {
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
		defer cancel()
		convID, _, err := a.api.NewConversation(ctx, "")
		if err != nil {
			a.post(event{kind: evNotice, notice: i18n.T("sendFailed") + " · " + err.Error()})
			return
		}
		a.post(event{kind: evSelect, convID: convID})
		a.requestRefresh()
	}()
}

// attachImages opens the file picker and reads the picks, off the frame thread
// because the panel blocks until it closes.
func (a *App) attachImages() {
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title: i18n.T("attachImage"),
			Filters: []mygo.FileFilter{
				{Name: "Images", Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}},
			},
			Multiple: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		var images []imageRef
		for _, path := range paths {
			if img, ok := readImage(path); ok {
				images = append(images, img)
			}
		}
		if len(images) > 0 {
			a.post(event{kind: evFiles, images: images})
		}
	}()
}

// attachPath reads one file the app was asked to open (a drop, or the menu's
// Open item) into the composer.
func (a *App) attachPath(path string) {
	go func() {
		if img, ok := readImage(path); ok {
			a.post(event{kind: evFiles, images: []imageRef{img}})
		}
	}()
}

func readImage(path string) (imageRef, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return imageRef{}, false
	}
	mime := "image/png"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".gif":
		mime = "image/gif"
	case ".webp":
		mime = "image/webp"
	}
	return imageRef{mime: mime, data: data}, true
}

func (a *App) addImages(images []imageRef) {
	if len(images) == 0 {
		return
	}
	a.draftImage = append(a.draftImage, images...)
	a.invalidate()
}

// restartAgent restarts the agent process, not the bridge.
func (a *App) restartAgent() {
	go func() {
		if out := bridge.Agentctl("restart"); strings.Contains(out, "error") {
			a.post(event{kind: evNotice, notice: out})
		}
	}()
}

// View is the frame: apply what arrived, then draw.

// same actions the composer's controls do.
func (a *App) NewChat() { a.newChat() }

func (a *App) AttachImages() { a.attachImages() }

func (a *App) RestartAgent() { a.restartAgent() }
