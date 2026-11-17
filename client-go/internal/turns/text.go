package turns

import (
	"encoding/json"
	"strings"

	"pitta/internal/protocol"
)

// Text is the readable text an entry carries: the text parts of its messages,
// joined as paragraphs. Views render it as Markdown; entries with no text
// (tool results, notes) give an empty string.
func Text(entry protocol.Entry) string {
	return textOf(partsOf(messages(entry.Model)))
}

// Thinking is the reasoning an assistant entry kept, shown folded above the
// answer.
func Thinking(entry protocol.Entry) string {
	var out strings.Builder
	for _, part := range partsOf(messages(entry.Model)) {
		if part["type"] != "thinking" {
			continue
		}
		if value, ok := part["thinking"].(string); ok && value != "" {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString(value)
		}
	}
	return out.String()
}

// ToolCall is the tool an entry calls: its name and call id, empty when the
// entry calls nothing. A call carries id/name/arguments; partsOf overwrites
// toolName with the message's field, so the part's own name is read first.
func ToolCall(entry protocol.Entry) (name, callID string) {
	for _, part := range partsOf(messages(entry.Model)) {
		if part["type"] != "toolCall" {
			continue
		}
		name = stringValue(part["name"])
		if name == "" {
			name = stringValue(part["toolName"])
		}
		callID = stringValue(part["id"])
		if callID == "" {
			callID = stringValue(part["toolCallId"])
		}
		return name, callID
	}
	return "", ""
}

// ToolCallArgs is the raw arguments of the entry's tool call, for the views
// that show a command line.
func ToolCallArgs(entry protocol.Entry) string {
	for _, part := range partsOf(messages(entry.Model)) {
		if part["type"] != "toolCall" {
			continue
		}
		args, ok := part["arguments"]
		if !ok {
			args = part["args"]
		}
		if ok || args != nil {
			if text, ok := args.(string); ok {
				return text
			}
			if encoded, err := json.Marshal(args); err == nil {
				return string(encoded)
			}
		}
	}
	return ""
}

// Verdict is what a report_done call claims: whether the turn reached the goal
// it was given, and the line saying how that was checked. ok is false when the
// entry carries no verdict to read, so a view can fall back to the plain call.
func Verdict(entry protocol.Entry) (met, ok bool, why string) {
	args := ToolCallArgs(entry)
	if args == "" {
		return false, false, ""
	}
	var call struct {
		Met *bool  `json:"met"`
		Why string `json:"why"`
	}
	if err := json.Unmarshal([]byte(args), &call); err != nil || call.Met == nil {
		return false, false, ""
	}
	return *call.Met, true, strings.TrimSpace(call.Why)
}

// ToolResult is what a tool answered: its text and whether it failed.
func ToolResult(entry protocol.Entry) (text string, isError bool) {
	var out strings.Builder
	for _, part := range partsOf(messages(entry.Model)) {
		if part["type"] != "text" {
			continue
		}
		if value, ok := part["text"].(string); ok {
			if out.Len() > 0 {
				out.WriteString("\n")
			}
			out.WriteString(value)
		}
	}
	return out.String(), entry.Kind == "pi.tool-result" && isErrorEntry(entry)
}

// isErrorEntry reads the isError flag tool results carry, which the wire keeps
// as a plain JSON field rather than a Go one.
func isErrorEntry(entry protocol.Entry) bool {
	for _, message := range messages(entry.Model) {
		if value, ok := message["isError"].(bool); ok && value {
			return true
		}
	}
	return false
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}
