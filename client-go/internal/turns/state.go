// Live state and coarse status of a turn. Ported from
// the old client/lib/turn-state.ts and the live-doc wiring in
// the old client/components/the transcript view.
package turns

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"pitta/internal/protocol"
)

// StateKind is what the newest turn is doing right now.
type StateKind int

const (
	StateIdle StateKind = iota
	StateThinking
	StateTool
	StateStreaming
	StateSettled
)

// State is one derivation for the whole live window.
//
// Tool is the name of the running tool; Text is the partial answer written
// before it started, and ToolTail is the last non-empty line of its output.
type State struct {
	Kind     StateKind
	Text     string
	Tool     string
	ToolTail string
}

// Live reports whether this turn still has work coming.
func (s State) Live() bool {
	return s.Kind == StateThinking || s.Kind == StateTool || s.Kind == StateStreaming
}

// State derives what the turn is doing from the conversation's live doc. A
// live doc left over after the run ended is a ghost: the record decides.
func (t *Turn) State(live *protocol.LiveDoc) State {
	hasAnswer := len(t.Finals) > 0
	busy := live != nil && len(live.Run) > 0
	if !busy {
		if hasAnswer {
			return State{Kind: StateSettled}
		}
		return State{Kind: StateIdle}
	}

	text := liveText(live.Generation)
	tool, hasTool := runningTool(live.Tools)
	if hasTool {
		return State{Kind: StateTool, Text: text, Tool: tool.name, ToolTail: tool.tail}
	}
	if text != "" {
		return State{Kind: StateStreaming, Text: text}
	}
	if hasAnswer {
		return State{Kind: StateSettled}
	}
	// Between two tool calls the model is thinking: no tool running, no text
	// yet. This is the state that must never render as nothing.
	return State{Kind: StateThinking}
}

type toolSlot struct {
	name string
	tail string
}

// runningTool finds the tool slot the run is inside right now, with the last
// non-empty line it printed.
func runningTool(tools []json.RawMessage) (toolSlot, bool) {
	for _, raw := range tools {
		var slot map[string]any
		if err := json.Unmarshal(raw, &slot); err != nil {
			log.Printf("turns: tool slot: %v", err)
			continue
		}
		if slot["status"] != "running" {
			continue
		}
		name := ""
		if value := slot["name"]; value != nil {
			name = fmt.Sprint(value)
		}
		output := ""
		if value := slot["output"]; value != nil {
			output = fmt.Sprint(value)
		}
		tail := ""
		for _, line := range strings.Split(output, "\n") {
			if strings.TrimSpace(line) != "" {
				tail = line
			}
		}
		return toolSlot{name: name, tail: tail}, true
	}
	return toolSlot{}, false
}

// liveText reads the text committed so far. The bridge commits a single
// AssistantMessage as generation.message while the client's partsOf only
// accepts a message array, so accept both shapes here.
func liveText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var generation map[string]any
	if err := json.Unmarshal(raw, &generation); err != nil {
		log.Printf("turns: generation: %v", err)
		return ""
	}
	switch message := generation["message"].(type) {
	case map[string]any:
		return textOf(partsOf([]map[string]any{message}))
	case []any:
		list := make([]map[string]any, 0, len(message))
		for _, item := range message {
			if record, ok := item.(map[string]any); ok {
				list = append(list, record)
			}
		}
		return textOf(partsOf(list))
	}
	return ""
}

// Status is the coarse state of a turn for summaries.
type Status int

const (
	StatusDone Status = iota
	StatusRunning
	StatusError
	StatusAborted
)

// Status derives the turn's outcome. A live run wins; otherwise the last
// assistant message decides, so an earlier failed retry does not mark a turn
// error after it recovered. stopReason comes from pi-ai's AssistantMessage
// (the earlier web client does not read it for turns).
func (t *Turn) Status(live *protocol.LiveDoc) Status {
	if live != nil && len(live.Run) > 0 {
		return StatusRunning
	}

	var last *protocol.Entry
	for _, entries := range [][]protocol.Entry{t.Internals, t.Finals} {
		for i := range entries {
			entry := &entries[i]
			if entry.Kind != "pi.assistant" {
				continue
			}
			if last == nil || entry.ID > last.ID {
				last = entry
			}
		}
	}
	if last == nil {
		return StatusDone
	}

	reason := ""
	for _, message := range messages(last.Model) {
		if value, ok := message["stopReason"].(string); ok {
			reason = value
			break
		}
	}
	switch reason {
	case "error":
		return StatusError
	case "aborted":
		return StatusAborted
	default:
		return StatusDone
	}
}
