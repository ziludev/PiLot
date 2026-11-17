// Package server serves tool calls over the toolbox unix socket.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"pitta/toolbox/internal/proto"
)

// Emit writes one event of a call. A handler uses it for streamed output and
// diagnostics; the terminal event is written by the server from the returned
// value, so a handler can never end a call twice.
type Emit func(proto.Event)

// Handler runs one call. The context is cancelled when the agent closes the
// connection or the server stops.
type Handler func(ctx context.Context, req proto.Request, emit Emit) (any, error)

// Server dispatches tool names to handlers and owns the listener's lifetime.
type Server struct {
	handlers map[string]Handler
	log      *log.Logger
	base     context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

// New returns a server with no tools. base is the lifetime of the whole process:
// stopping it cancels every call in flight.
func New(base context.Context, logger *log.Logger) *Server {
	ctx, cancel := context.WithCancel(base)
	return &Server{handlers: map[string]Handler{}, log: logger, base: ctx, cancel: cancel}
}

// Register adds one tool. Registering a name twice panics: a silent override
// would make the toolbox answer a tool the agent never meant to move here.
func (s *Server) Register(tool string, handler Handler) {
	if _, ok := s.handlers[tool]; ok {
		panic("toolbox: handler registered twice: " + tool)
	}
	s.handlers[tool] = handler
}

// Names lists the registered tools, for the startup log and the ping reply.
func (s *Server) Names() []string {
	names := make([]string, 0, len(s.handlers))
	for name := range s.handlers {
		names = append(names, name)
	}
	return names
}

// ErrUnknownTool is what a call for an unregistered tool fails with.
var ErrUnknownTool = errors.New("unknown tool")

// Serve accepts connections until the listener is closed or base is cancelled.
func (s *Server) Serve(listener net.Listener) error {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if s.base.Err() != nil || errors.Is(err, net.ErrClosed) {
				s.wg.Wait()
				return nil
			}
			return err
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(conn)
		}()
	}
}

// Close cancels every call in flight and waits for them to return.
func (s *Server) Close() {
	s.cancel()
	s.wg.Wait()
}

// handleConn reads one Request and answers it. A decoder is used instead of a
// line reader because `write` carries whole files: a Scanner would cap the
// request at its buffer size.
func (s *Server) handleConn(conn net.Conn) {
	defer conn.Close()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	var writeMu sync.Mutex
	write := func(event proto.Event) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.Encode(event)
	}

	var req proto.Request
	if err := decoder.Decode(&req); err != nil {
		if !errors.Is(err, io.EOF) {
			s.log.Printf("toolbox: bad request: %v", err)
			_ = write(proto.Event{Type: proto.TypeError, Message: "bad request: " + err.Error()})
		}
		return
	}

	handler, ok := s.handlers[req.Tool]
	if !ok {
		_ = write(proto.Event{Type: proto.TypeError, Message: fmt.Sprintf("%s: %s", ErrUnknownTool, req.Tool)})
		return
	}

	// The call dies with the connection: the agent closing the socket is the
	// only abort signal it has.
	ctx, cancel := context.WithCancel(s.base)
	defer cancel()
	go func() {
		_, err := decoder.Token()
		if err != nil {
			cancel()
		}
	}()

	result, err := handler(ctx, req, func(event proto.Event) {
		if writeErr := write(event); writeErr != nil {
			cancel()
		}
	})
	if ctx.Err() != nil && err != nil {
		// The agent went away; nothing is listening for the outcome.
		return
	}
	if err != nil {
		_ = write(proto.Event{Type: proto.TypeError, Message: err.Error()})
		return
	}
	payload, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		_ = write(proto.Event{Type: proto.TypeError, Message: "result: " + marshalErr.Error()})
		return
	}
	_ = write(proto.Event{Type: proto.TypeResult, Result: payload})
}
