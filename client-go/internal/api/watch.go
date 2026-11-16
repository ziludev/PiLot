package api

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"

	"pitta/internal/protocol"
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
		frame, err := decodeFrame(data)
		if err != nil {
			log.Printf("api: watch %d: bad frame: %v", conversationID, err)
			continue
		}
		select {
		case frames <- frame:
		case <-ctx.Done():
			return
		}
	}
}

// decodeFrame decodes a frame from the wire instead of going straight into
// protocol.Frame: the bridge sends several documents (pi.agent, pi.inbox,
// pi.provider, pi.usage) whose shapes do not fit LiveDoc, so the docs map is
// decoded only for "pi.live" and the rest are dropped.
func decodeFrame(data []byte) (protocol.Frame, error) {
	var wire struct {
		Value *struct {
			Entries []protocol.Entry           `json:"entries"`
			Docs    map[string]json.RawMessage `json:"docs"`
		} `json:"value"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return protocol.Frame{}, err
	}
	if wire.Value == nil {
		return protocol.Frame{}, nil
	}
	frame := protocol.Frame{Value: &protocol.FrameValue{
		Entries: wire.Value.Entries,
		Docs:    map[string]protocol.LiveDoc{},
	}}
	if raw, ok := wire.Value.Docs["pi.live"]; ok {
		var live protocol.LiveDoc
		if err := json.Unmarshal(raw, &live); err != nil {
			return protocol.Frame{}, err
		}
		frame.Value.Docs["pi.live"] = live
	}
	return frame, nil
}

func (c *Client) watchURL(conversationID int) string {
	base := c.baseURL
	if rest, ok := strings.CutPrefix(base, "https://"); ok {
		base = "wss://" + rest
	} else if rest, ok := strings.CutPrefix(base, "http://"); ok {
		base = "ws://" + rest
	}
	query := url.Values{"conversationId": {strconv.Itoa(conversationID)}}
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
