package app

import (
	"fmt"
	"log"
	"strings"

	"pitta/internal/turns"
)

// saver writes an exported chat wherever the user points the save panel. main.go
// sets it; the tests replace it to see the bytes.
var saver func(name string, data []byte) error

// SetSaver lets main.go hand the app the system's save panel.
func SetSaver(fn func(name string, data []byte) error) { saver = fn }

// MenuExportChat writes the open chat as Markdown, so a conversation outlives
// the app that showed it. A page cannot offer this without a server.
func (a *App) MenuExportChat() {
	a.apply(func() {
		name, text := a.exportMarkdown()
		if saver == nil {
			return
		}
		data := []byte(text)
		go func() {
			if err := saver(name, data); err != nil {
				log.Printf("app: export: %v", err)
				return
			}
			a.apply(func() {
				a.notice = i18nExported
				a.invalidate()
			})
		}()
	})
}

// exportMarkdown renders the chat the way a reader would keep it: the question,
// what the agent did, and the answers, with the cost at the end.
func (a *App) exportMarkdown() (string, string) {
	title := a.exportTitle()
	var b strings.Builder
	b.WriteString("# " + title + "\n\n")
	for _, turn := range turns.GroupTurns(a.entries, a.waits) {
		if turn.User != nil {
			if text := turns.Text(*turn.User); text != "" {
				b.WriteString("## You\n\n" + text + "\n\n")
			}
		}
		for _, step := range turn.Steps {
			fmt.Fprintf(&b, "- %s\n", step.Label)
		}
		if len(turn.Steps) > 0 {
			b.WriteString("\n")
		}
		for _, answer := range turn.Finals {
			if text := turns.Text(answer); text != "" {
				b.WriteString(text + "\n\n")
			}
		}
		for _, note := range turn.Notes {
			if text := noteText(note); text != "" {
				b.WriteString("> " + text + "\n\n")
			}
		}
	}
	if a.surfaces != nil {
		for _, s := range a.surfaces {
			if s.ConversationID == a.currentValue() {
				fmt.Fprintf(&b, "---\n\ncost $%.2f · %d tokens\n", s.Cost, s.Tokens)
			}
		}
	}
	return exportName(title, a.currentValue()), b.String()
}

// exportTitle is the chat's own title, without the rail's stand-ins for an
// untitled one.
func (a *App) exportTitle() string {
	for _, s := range a.surfaces {
		if s.ConversationID == a.currentValue() {
			if s.Title != "" {
				return s.Title
			}
			break
		}
	}
	return "Chat"
}

// exportName is the file name the save panel starts from.
func exportName(title string, conversationID int) string {
	name := strings.Map(func(r rune) rune {
		if r == '/' || r == ':' || r == 0 {
			return '-'
		}
		return r
	}, strings.TrimSpace(title))
	if name == "" {
		name = "chat"
	}
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	return fmt.Sprintf("%s #%d.md", name, conversationID)
}

// i18nExported is the confirmation the export leaves in the rail; the React
// client has no export, so it has no key in the dictionary.
const i18nExported = "Chat exported"
