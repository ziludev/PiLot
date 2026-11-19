package app

import (
	"encoding/json"
	"strings"

	"pitta/internal/protocol"
)

// inboxText is the words of one waiting message. The harness keeps content the
// way the model takes it, so it arrives as a bare string or as parts where only
// the text ones say anything.
func inboxText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return strings.TrimSpace(text)
	}
	var parts []textPart
	if err := json.Unmarshal(raw, &parts); err != nil {
		var object struct {
			Text  string            `json:"text"`
			Parts []json.RawMessage `json:"parts"`
		}
		if json.Unmarshal(raw, &object) != nil {
			return ""
		}
		if object.Text != "" {
			return strings.TrimSpace(object.Text)
		}
		if parts, err = unmarshalParts(object.Parts); err != nil {
			return ""
		}
	}
	var said []string
	for _, part := range parts {
		if part.Type == "text" && strings.TrimSpace(part.Text) != "" {
			said = append(said, strings.TrimSpace(part.Text))
		}
	}
	return strings.Join(said, " ")
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// unmarshalParts decodes the parts of an object-shaped message.
func unmarshalParts(raw []json.RawMessage) ([]textPart, error) {
	parts := make([]textPart, 0, len(raw))
	for _, one := range raw {
		var part textPart
		if err := json.Unmarshal(one, &part); err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

// inboxItems is the queue in front of the chat on screen.
func (a *App) inboxItems() []protocol.InboxItem {
	if a.active == 0 {
		return nil
	}
	if thread, ok := a.threads[a.active]; ok {
		return thread.inbox
	}
	return nil
}
