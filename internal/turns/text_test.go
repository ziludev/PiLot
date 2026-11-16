package turns

import (
	"testing"

	"pitta/internal/protocol"
)

// TestEntryReaders pins the view-facing readers to the shapes the bridge
// really sends: content parts, tool calls and tool results.
func TestEntryReaders(t *testing.T) {
	answer := entry(1, "pi.assistant", []map[string]any{
		{"role": "assistant", "content": []any{
			map[string]any{"type": "thinking", "thinking": "why"},
			map[string]any{"type": "text", "text": "hello"},
			map[string]any{"type": "text", "text": "world"},
		}},
	})
	if got := Text(answer); got != "helloworld" {
		t.Fatalf("Text = %q", got)
	}
	if got := Thinking(answer); got != "why" {
		t.Fatalf("Thinking = %q", got)
	}

	user := entry(2, "pi.user", []map[string]any{{"role": "user", "content": "hi there"}})
	if got := Text(user); got != "hi there" {
		t.Fatalf("string content Text = %q", got)
	}

	call := entry(3, "pi.assistant", []map[string]any{
		{"role": "assistant", "content": []any{
			map[string]any{"type": "toolCall", "name": "bash", "id": "call_1", "arguments": map[string]any{"command": "ls"}},
		}},
	})
	if name, id := ToolCall(call); name != "bash" || id != "call_1" {
		t.Fatalf("ToolCall = %q %q", name, id)
	}
	if got := ToolCallArgs(call); got != `{"command":"ls"}` {
		t.Fatalf("ToolCallArgs = %q", got)
	}

	result := entry(4, "pi.tool-result", []map[string]any{
		{"role": "toolResult", "toolCallId": "call_1", "toolName": "bash", "isError": true,
			"content": []any{map[string]any{"type": "text", "text": "boom"}}},
	})

	text, isError := ToolResult(result)
	if text != "boom" || !isError {
		t.Fatalf("ToolResult = %q %v", text, isError)
	}

	if got := Text(protocol.Entry{Kind: "app.note"}); got != "" {
		t.Fatalf("note Text = %q", got)
	}
}
