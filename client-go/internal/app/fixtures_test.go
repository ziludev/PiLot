package app

import (
	"encoding/json"

	"pitta/internal/protocol"
)

// turnWithTool is one turn that thinks, calls a tool and answers: enough of a
// transcript for the lines that hang off a turn's end.
func turnWithTool() []protocol.Entry {
	entry := func(id int, kind string, value any) protocol.Entry {
		data, err := json.Marshal([]any{value})
		if err != nil {
			panic(err)
		}
		return protocol.Entry{ID: id, Kind: kind, Model: data}
	}
	return []protocol.Entry{
		entry(1, "pi.user", map[string]any{
			"role": "user", "content": []any{map[string]any{"type": "text", "text": "run it"}}, "timestamp": 1000,
		}),
		entry(2, "pi.assistant", map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "thinking", "thinking": "let me look"}}, "timestamp": 1100,
		}),
		entry(3, "pi.assistant", map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "toolCall", "id": "c1", "name": "bash", "arguments": map[string]any{"command": "ls"}}}, "timestamp": 1200,
		}),
		entry(4, "pi.tool-result", map[string]any{
			"role": "toolResult", "toolCallId": "c1", "content": []any{map[string]any{"type": "text", "text": "a.go"}}, "timestamp": 1300,
		}),
		entry(5, "pi.assistant", map[string]any{
			"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "done"}}, "timestamp": 1400,
		}),
	}
}
