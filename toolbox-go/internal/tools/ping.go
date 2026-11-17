// Package tools holds the toolbox's tool implementations.
package tools

import (
	"context"
	"os"

	"pitta/toolbox/internal/proto"
	"pitta/toolbox/internal/server"
)

// PingResult is what the agent checks once at startup: the toolbox answering
// means the socket and the protocol version are both usable.
type PingResult struct {
	Version string   `json:"version"`
	PID     int      `json:"pid"`
	Tools   []string `json:"tools"`
}

// RegisterPing adds the `ping` tool. It is not offered to the model; it only
// proves the service is up before the agent flips AGENT_TOOLBOX=go.
func RegisterPing(srv *server.Server, version string) {
	srv.Register("ping", func(ctx context.Context, req proto.Request, emit server.Emit) (any, error) {
		return PingResult{Version: version, PID: os.Getpid(), Tools: srv.Names()}, nil
	})
}
