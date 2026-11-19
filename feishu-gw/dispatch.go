// dispatch.go: the two directions of the event path.
//
// They have opposite timing needs, which is why they are not one function. A message event is a
// turn that takes minutes: the connection must be released at once, so the forward happens in the
// background and a failure is retried. A card action is a click: Feishu shows its own error after
// about three seconds, so the answer has to come back inside that budget.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
)

// Feishu shows "操作失败" if a card action is not answered in roughly three seconds.
const cardBudget = 2500 * time.Millisecond

// 0, 1s, 2s, 4s: a bridge restart lasts seconds, and anything longer than that is a turn already
// lost by another route. A variable only so tests need not wait out the real backoff.
var retryBackoff = []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second}

type forwarder struct {
	base     string
	token    string
	out      *logging
	state    *stats
	http     *http.Client
	cardHTTP *http.Client
}

func (f *forwarder) client() *http.Client {
	if f.http == nil {
		f.http = &http.Client{Timeout: 10 * time.Second}
	}
	return f.http
}

func (f *forwarder) cardClient() *http.Client {
	if f.cardHTTP == nil {
		f.cardHTTP = &http.Client{Timeout: cardBudget}
	}
	return f.cardHTTP
}

// deliver posts one event and retries a few times. The bridge dedupes by message id, so a retry
// after a timeout it actually processed is harmless.
func (f *forwarder) deliver(body map[string]any) {
	payload, err := json.Marshal(body)
	if err != nil {
		f.state.dropped.Add(1)
		f.out.emit("event encode failed", map[string]any{"err": err.Error()})
		return
	}
	var last error
	for _, delay := range retryBackoff {
		if delay > 0 {
			time.Sleep(delay)
		}
		last = f.post(payload)
		if last == nil {
			f.state.delivered.Add(1)
			return
		}
	}
	f.state.dropped.Add(1)
	f.out.emit("event dropped", map[string]any{"err": last.Error(), "bytes": len(payload)})
}

func (f *forwarder) post(payload []byte) error {
	request, err := http.NewRequest(http.MethodPost, f.base+"/feishu/events", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("content-type", "application/json")
	if f.token != "" {
		request.Header.Set("authorization", "Bearer "+f.token)
	}
	response, err := f.client().Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode >= 300 {
		return fmt.Errorf("bridge answered %d", response.StatusCode)
	}
	return nil
}

// card hands the click to the bridge and passes its answer straight back to Feishu. The bridge
// returns the very shape Feishu expects ({toast} or {card}), so this only has to be a bridge.
func (f *forwarder) card(ctx context.Context, body map[string]any) *callback.CardActionTriggerResponse {
	payload, err := json.Marshal(body)
	if err != nil {
		return toast("error", "点击没送到（编码失败）")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.base+"/feishu/events", bytes.NewReader(payload))
	if err != nil {
		return toast("error", "点击没送到（请求失败）")
	}
	request.Header.Set("content-type", "application/json")
	if f.token != "" {
		request.Header.Set("authorization", "Bearer "+f.token)
	}
	response, err := f.cardClient().Do(request)
	if err != nil {
		f.out.emit("card action not answered", map[string]any{"err": err.Error()})
		return toast("info", "正在处理，请看卡片")
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || response.StatusCode >= 300 {
		f.out.emit("card action failed", map[string]any{"status": response.StatusCode})
		return toast("info", "正在处理，请看卡片")
	}
	answer := &callback.CardActionTriggerResponse{}
	if err := json.Unmarshal(raw, answer); err != nil || (answer.Toast == nil && answer.Card == nil) {
		return &callback.CardActionTriggerResponse{}
	}
	return answer
}

func toast(kind, content string) *callback.CardActionTriggerResponse {
	return &callback.CardActionTriggerResponse{Toast: &callback.Toast{Type: kind, Content: content}}
}
