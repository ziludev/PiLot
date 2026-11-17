// Package proto is the wire format between the agent (Node) and the toolbox.
//
// One unix-socket connection carries exactly one call: the agent writes one
// Request value, the toolbox answers with zero or more streamed events and
// exactly one terminal event (result or error). A call per connection keeps the
// read side free of request demultiplexing, and lets a closed connection mean
// "the agent gave up", which cancels the work.
package proto

import "encoding/json"

// Request is what the agent sends for one call. Args stays raw so the toolbox
// never validates twice: the agent's pi-schema already did.
type Request struct {
	Tool   string          `json:"tool"`
	Args   json.RawMessage `json:"args,omitempty"`
	Cwd    string          `json:"cwd,omitempty"`
	CallID string          `json:"callId,omitempty"`
}

// EventType names what an Event carries.
type EventType string

const (
	// TypeChunk appends text to the running output, as api.output() does.
	TypeChunk EventType = "chunk"
	// TypeDiagnostic is a model-visible remark about the call.
	TypeDiagnostic EventType = "diagnostic"
	// TypeResult carries the tool's structured result and ends the call.
	TypeResult EventType = "result"
	// TypeError ends the call with a message the tool throws.
	TypeError EventType = "error"
)

// Severity values a diagnostic may use, matching pi-durable's ToolDiagnostic.
const (
	SeverityInfo  = "info"
	SeverityWarn  = "warning"
	SeverityError = "error"
)

// Event is one value the toolbox writes.
type Event struct {
	Type EventType `json:"type"`

	// TypeChunk: the text to append, and how much earlier output the toolbox
	// itself dropped before this chunk (0 when it keeps everything).
	Data    string `json:"data,omitempty"`
	Skipped int    `json:"skipped,omitempty"`

	// TypeDiagnostic
	Severity string `json:"severity,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`

	// TypeResult
	Result json.RawMessage `json:"result,omitempty"`

	// TypeError: the tool's failure code (timeout, aborted, spawn_error) and, when the
	// output was saved anyway, the file that holds it.
	SpillPath string `json:"spillPath,omitempty"`
}
