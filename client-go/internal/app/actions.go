package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

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
	a.sendText(convID, text, images)
}

// sendText posts one message; it is the same path the composer takes and the
// one a retry takes with the words that failed.
func (a *App) sendText(convID int, text string, images []imageRef) {
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
func (a *App) newChat() { a.newChatIn("") }

// newChatIn starts an empty chat in a project's worktree. An empty id is the
// built-in sandbox — the same thing the rail's + has always done.
func (a *App) newChatIn(projectID string) {
	go func() {
		ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
		defer cancel()
		convID, _, err := a.api.NewConversation(ctx, projectID)
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
				{Name: "Images and text", Extensions: append(append([]string{}, imageExts...), textExts...)},
				{Name: "Images", Extensions: imageExts},
			},
			Multiple: true,
		})
		if err != nil || len(paths) == 0 {
			return
		}
		var images []imageRef
		var pointed []string
		for _, path := range paths {
			if img, ok := readImage(path); ok {
				images = append(images, img)
			} else if !a.inlineText(path) {
				pointed = append(pointed, path)
			}
		}
		if len(images) > 0 {
			a.post(event{kind: evFiles, images: images})
		}
		a.putPaths(pointed)
	}()
}

// attachPath reads one file the app was asked to open (a drop, or the menu's
// Open item) into the composer.
func (a *App) attachPath(path string) {
	go func() {
		if img, ok := readImage(path); ok {
			a.post(event{kind: evFiles, images: []imageRef{img}})
			return
		}
		if !a.inlineText(path) {
			a.putPaths([]string{path})
		}
	}()
}

// putPaths puts the files the window cannot take into the message as paths.
// The agent runs on this machine and can read all of it — a pdf, a log, a
// directory — so refusing the drop only made the reader copy the path by hand.
func (a *App) putPaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	lines := make([]string, 0, len(paths))
	for _, path := range paths {
		abs, err := filepath.Abs(path)
		if err != nil {
			abs = path
		}
		// A path with a space would read as two paths; the backticks keep it
		// one word for the reader and for the model.
		if strings.ContainsRune(abs, ' ') {
			abs = "`" + abs + "`"
		}
		lines = append(lines, abs)
	}
	a.post(event{kind: evDraft, text: strings.Join(lines, "\n")})
	a.post(event{kind: evNotice, ok: true, notice: i18n.TF("pathDropped", map[string]any{"n": len(lines)})})
}

// imageExts and textExts are what the window can take. An image travels as an
// image part; a text file is quoted into the message, because the model reads
// source better as text than as an attachment it cannot open.
var imageExts = []string{"png", "jpg", "jpeg", "gif", "webp"}
var textExts = []string{"go", "ts", "tsx", "js", "jsx", "json", "md", "txt", "yaml", "yml", "toml", "sh", "py", "rs", "sql", "css", "html"}

// maxInline is where quoting a file stops being a message and starts being a
// payload: past this the reader is better off pointing the agent at the path.
const maxInline = 200 << 10

// inlineText puts a text file in the composer as a fenced block, and reports
// whether it did.
func (a *App) inlineText(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if !slices.Contains(textExts, ext) {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxInline {
		// Too big to quote is not too big to point at.
		return false
	}
	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return false
	}
	quoted := "```" + ext + "\n" + string(data) + "\n```"
	a.post(event{kind: evDraft, text: quoted})
	return true
}

// readImage reads a file only if it is one of the image types the provider
// takes: guessing png for anything else sent pdfs to the model as a picture.
func readImage(path string) (imageRef, bool) {
	mime := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
	}[strings.ToLower(filepath.Ext(path))]
	if mime == "" {
		return imageRef{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return imageRef{}, false
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

// setLang switches the window's language and remembers it: the language of a
// button is not something to re-pick on every launch.
func (a *App) setLang(lang i18n.Lang) {
	if i18n.Current() == lang {
		return
	}
	i18n.Set(lang)
	a.prefs.Lang = string(lang)
	a.stamp++
	go savePrefs(a.prefs)
	a.invalidate()
}

// setTheme picks the theme and remembers it. "" follows macOS; naming a side
// settles the choice, and from then on the system setting no longer moves the
// window.
func (a *App) setTheme(mode string) {
	if a.prefs.Theme == mode {
		return
	}
	a.prefs.Theme = mode
	go savePrefs(a.prefs)
	a.invalidate()
}

// setSilent switches the banners off or on. Off means no banner at all — the
// rail's unread dot stays, so nothing is lost, just quieter.
func (a *App) setSilent(silent bool) {
	if a.prefs.Silent == silent {
		return
	}
	a.prefs.Silent = silent
	go savePrefs(a.prefs)
	a.invalidate()
}

// setRailGroup files the rail by kind ("") or by project. The headings are
// rebuilt on the next frame; nothing about the chats themselves changes.
func (a *App) setRailGroup(group string) {
	if a.prefs.Group == group {
		return
	}
	a.prefs.Group = group
	go savePrefs(a.prefs)
	a.invalidate()
}

// toggleRailGroup flips the grouping, for the palette's one-line verb.
func (a *App) toggleRailGroup() {
	if a.prefs.groupByProject() {
		a.setRailGroup("")
		a.post(event{kind: evNotice, notice: i18n.T("railGroupKindDone"), ok: true})
		return
	}
	a.setRailGroup(groupProject)
	a.post(event{kind: evNotice, notice: i18n.T("railGroupProjectDone"), ok: true})
}

// retryTurn sends the words that started the failed turn again: the error row
// is the only place the reader learns a turn never answered, so it should be
// the place that sends it once more.
func (a *App) retryTurn(turn int) {
	if turn == 0 {
		return
	}
	for _, row := range a.threadRows(a.active) {
		if row.kind == rowUser && row.id == "u"+itoa(turn) {
			if strings.TrimSpace(row.text) != "" || len(row.images) > 0 {
				a.sendText(a.active, row.text, row.images)
			}
			return
		}
	}
}
