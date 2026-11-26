package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"pilot/internal/protocol"
)

const (
	firstRetryDelay = time.Second
	maxRetryDelay   = 15 * time.Second

	// A frame can carry a whole conversation, which easily passes the
	// library's 32 KiB default.
	maxFrameBytes = 32 << 20
)

// Watch streams one conversation until ctx is canceled, then closes the
// channel. A dropped socket reconnects with a 1s→2s→4s→…→15s backoff, reset
// after a connection succeeds; frames are read one connection at a time, so a
// reconnected socket never interleaves with the old one.
func (c *Client) Watch(ctx context.Context, conversationID int) <-chan protocol.Frame {
	frames := make(chan protocol.Frame)
	go c.watch(ctx, conversationID, frames)
	return frames
}

func (c *Client) watch(ctx context.Context, conversationID int, frames chan<- protocol.Frame) {
	defer close(frames)
	target := c.watchURL(conversationID)
	delay := firstRetryDelay
	for {
		conn, _, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPClient: c.http})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("api: watch %d: dial: %v", conversationID, err)
			if !wait(ctx, delay) {
				return
			}
			delay = nextDelay(delay)
			continue
		}
		conn.SetReadLimit(maxFrameBytes)
		delay = firstRetryDelay
		c.readFrames(ctx, conversationID, conn, frames)
		conn.CloseNow()
		if ctx.Err() != nil {
			return
		}
		if !wait(ctx, delay) {
			return
		}
		delay = nextDelay(delay)
	}
}

func (c *Client) readFrames(ctx context.Context, conversationID int, conn *websocket.Conn, frames chan<- protocol.Frame) {
	// Read honors ctx, but this also unblocks it when the consumer has stopped
	// receiving and the send below is what is waiting.
	stop := context.AfterFunc(ctx, func() { conn.CloseNow() })
	defer stop()
	// The baseline for this connection only. Deliberately reset on every call:
	// a reconnected socket starts its own stream from a full first frame, and an
	// ops frame that arrived before that baseline must not be applied to the old
	// tree (see the order check in decodeFrame).
	var tree any
	for {
		kind, data, err := conn.Read(ctx)
		if err != nil {
			// A close frame is the bridge restarting or going away on purpose:
			// reconnect quietly instead of logging it as a failure.
			if ctx.Err() == nil && websocket.CloseStatus(err) == -1 {
				log.Printf("api: watch %d: read: %v", conversationID, err)
			}
			return
		}
		if kind != websocket.MessageText {
			continue
		}
		frame, next, err := decodeFrame(data, tree)
		if err != nil {
			// A frame we cannot apply leaves the tree suspect. Reconnecting is the
			// only way back to a known-good baseline, and it is exactly what the
			// existing backoff loop already does; emitting from a half-applied tree
			// would corrupt the view silently.
			log.Printf("api: watch %d: bad frame: %v", conversationID, err)
			conn.CloseNow()
			return
		}
		tree = next
		if frame.Value == nil {
			continue
		}
		select {
		case frames <- frame:
		case <-ctx.Done():
			return
		}
	}
}

// decodeFrame decodes one socket message and returns the tree that the next
// frame must apply to.
//
// Two shapes share the socket:
//
//	{"type":"value","value":<view>}    a full view; (re)builds the baseline
//	{"type":"ops","ops":[<op>...]}     a delta against the current baseline
//
// Decoding through an `any` tree instead of straight into protocol.Frame keeps
// the delta honest: ops address the view itself, and projecting afterwards is
// one marshal. The docs map keeps every document so paths stay valid; project
// emits only "pi.live" (the bridge sends pi.agent/pi.inbox/pi.provider/pi.usage
// whose shapes do not fit LiveDoc).
func decodeFrame(data []byte, tree any) (protocol.Frame, any, error) {
	var wire struct {
		Type  string            `json:"type"`
		Value json.RawMessage   `json:"value"`
		Ops   []json.RawMessage `json:"ops"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return protocol.Frame{}, nil, err
	}

	if wire.Type == "ops" {
		if tree == nil {
			// A delta before any full frame: the stream started mid-conversation
			// or the bridge changed shape. There is nothing to apply it to.
			return protocol.Frame{}, nil, fmt.Errorf("ops frame before a value frame")
		}
		root, err := applyOps(tree, wire.Ops)
		if err != nil {
			return protocol.Frame{}, nil, err
		}
		value, err := project(root)
		if err != nil {
			return protocol.Frame{}, nil, err
		}
		return protocol.Frame{Value: &value}, root, nil
	}

	if len(wire.Value) == 0 {
		// Unknown frame kind with no view: nothing the client can draw.
		return protocol.Frame{}, tree, nil
	}
	root, err := decodeTree(wire.Value)
	if err != nil {
		return protocol.Frame{}, nil, err
	}
	value, err := project(root)
	if err != nil {
		return protocol.Frame{}, nil, err
	}
	return protocol.Frame{Value: &value}, root, nil
}

func (c *Client) watchURL(conversationID int) string {
	base := c.baseURL
	if rest, ok := strings.CutPrefix(base, "https://"); ok {
		base = "wss://" + rest
	} else if rest, ok := strings.CutPrefix(base, "http://"); ok {
		base = "ws://" + rest
	}
	query := url.Values{"conversationId": {strconv.Itoa(conversationID)}}
	// Ask for deltas: the first frame is still the full view, later frames are
	// {"type":"ops"}. The bridge keeps the old shape for clients without this.
	query.Set("ops", "1")
	if c.token != "" {
		query.Set("token", c.token)
	}
	return base + "/ws?" + query.Encode()
}

func wait(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func nextDelay(delay time.Duration) time.Duration {
	delay *= 2
	if delay > maxRetryDelay {
		return maxRetryDelay
	}
	return delay
}
