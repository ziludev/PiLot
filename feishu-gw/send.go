// send.go: the five Feishu operations the bridge actually uses, and nothing else.
//
// The bridge used to reach for the SDK for each of them; what it needs from Feishu is create,
// reply, patch, "who sent this" and "give me that attachment". Keeping the list closed is the point
// — anything else it might want later is a new decision, not a fallthrough.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// feishuError carries Feishu's own code so the bridge can log what Feishu said instead of "400".
type feishuError struct {
	code int
	msg  string
}

func (e *feishuError) Error() string { return fmt.Sprintf("feishu %d: %s", e.code, e.msg) }

type sender struct {
	clients *clientSet
	out     *logging
}

// clientSet owns the SDK client so a dead tenant token can be replaced at the first rejection.
//
// The SDK keeps the tenant token in memory until its own expiry and never re-fetches it when Feishu
// answers "invalid token". One failed refresh — a proxy blip is enough — leaves the process sending
// a token Feishu has already dropped, and every call fails until someone restarts it. A fresh client
// starts with an empty cache, so rebuilding it is the whole repair.
type clientSet struct {
	mu    sync.Mutex
	build func() *lark.Client
	live  *lark.Client
}

func newClientSet(build func() *lark.Client) *clientSet {
	set := &clientSet{build: build}
	if build != nil {
		set.live = build()
	}
	return set
}

func (c *clientSet) get() *lark.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.live
}

// reset replaces the client only if it is still the one that failed: a burst of calls that all hit
// the stale token must rebuild once, not once per call.
func (c *clientSet) reset(stale *lark.Client) {
	if c.build == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live != stale {
		return
	}
	c.live = c.build()
}

// staleTokenCodes: 99991663 "invalid access token", 99991668 "tenant token invalid or expired".
// Both mean the token is unusable and the SDK cannot recover from it on its own.
var staleTokenCodes = map[int]bool{99991663: true, 99991668: true}

func isStaleToken(err error) bool {
	var feishu *feishuError
	if !errors.As(err, &feishu) {
		return false
	}
	return staleTokenCodes[feishu.code]
}

// guard runs one operation and retries it once after replacing the client when Feishu says the token
// is dead. Only that error is retried: a timeout means the call may already have landed, so retrying
// it could post twice.
func (s *sender) guard(what string, op func(*lark.Client) error) error {
	client := s.clients.get()
	err := op(client)
	if !isStaleToken(err) {
		return err
	}
	s.out.emit("stale token", map[string]any{"what": what, "err": err.Error()})
	s.clients.reset(client)
	return op(s.clients.get())
}

func (s *sender) create(ctx context.Context, chatID, msgType, content string) (string, error) {
	var messageID string
	err := s.guard("create", func(client *lark.Client) error {
		response, err := client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(larkim.CreateMessageV1ReceiveIDTypeChatId).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(chatID).
				MsgType(msgType).
				Content(content).
				Build()).
			Build())
		if err != nil {
			return err
		}
		if !response.Success() {
			return &feishuError{code: response.Code, msg: response.Msg}
		}
		if response.Data == nil || response.Data.MessageId == nil {
			return fmt.Errorf("feishu returned no message id")
		}
		messageID = *response.Data.MessageId
		return nil
	})
	return messageID, err
}

func (s *sender) reply(ctx context.Context, messageID, msgType, content string, inThread bool) (string, error) {
	body := larkim.NewReplyMessageReqBodyBuilder().
		MsgType(msgType).
		Content(content)
	if inThread {
		body = body.ReplyInThread(true)
	}
	var replied string
	err := s.guard("reply", func(client *lark.Client) error {
		response, err := client.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().
			MessageId(messageID).
			Body(body.Build()).
			Build())
		if err != nil {
			return err
		}
		if !response.Success() {
			return &feishuError{code: response.Code, msg: response.Msg}
		}
		if response.Data == nil || response.Data.MessageId == nil {
			return fmt.Errorf("feishu returned no message id")
		}
		replied = *response.Data.MessageId
		return nil
	})
	return replied, err
}

func (s *sender) patch(ctx context.Context, messageID, content string) error {
	return s.guard("patch", func(client *lark.Client) error {
		response, err := client.Im.Message.Patch(ctx, larkim.NewPatchMessageReqBuilder().
			MessageId(messageID).
			Body(larkim.NewPatchMessageReqBodyBuilder().Content(content).Build()).
			Build())
		if err != nil {
			return err
		}
		if !response.Success() {
			return &feishuError{code: response.Code, msg: response.Msg}
		}
		return nil
	})
}

// senderOf answers "did we write this?" — the one fact the bridge reads back from Feishu, used to
// tell a person quoting our answer from a person quoting themselves.
func (s *sender) senderOf(ctx context.Context, messageID string) (openID, appID, senderType string, err error) {
	err = s.guard("sender lookup", func(client *lark.Client) error {
		openID, appID, senderType = "", "", ""
		response, err := client.Im.Message.Get(ctx, larkim.NewGetMessageReqBuilder().
			MessageId(messageID).
			Build())
		if err != nil {
			return err
		}
		if !response.Success() {
			return &feishuError{code: response.Code, msg: response.Msg}
		}
		if response.Data == nil || len(response.Data.Items) == 0 || response.Data.Items[0] == nil {
			return fmt.Errorf("message %s not found", messageID)
		}
		message := response.Data.Items[0]
		if message.Sender == nil {
			return nil
		}
		// For an app-sent message Feishu fills `id` with the app id and puts the bot's open id in
		// open_bot_id; for a person `id` is the open id itself. The bridge needs both to answer "did we
		// write this?" without reading the body.
		openID, appID, senderType = pointerValue(message.Sender.OpenBotId), pointerValue(message.Sender.Id), pointerValue(message.Sender.SenderType)
		return nil
	})
	return openID, appID, senderType, err
}

func (s *sender) resource(ctx context.Context, messageID, fileKey, kind string) (io.Reader, string, error) {
	if kind == "" {
		kind = "image"
	}
	var reader io.Reader
	var name string
	err := s.guard("download", func(client *lark.Client) error {
		response, err := client.Im.MessageResource.Get(ctx, larkim.NewGetMessageResourceReqBuilder().
			MessageId(messageID).
			FileKey(fileKey).
			Type(kind).
			Build())
		if err != nil {
			return err
		}
		if !response.Success() {
			return &feishuError{code: response.Code, msg: response.Msg}
		}
		// Only the first attempt's body is handed back: a retried download is a fresh stream.
		reader, name = response.File, response.FileName
		return nil
	})
	return reader, name, err
}

// bot is who we are: the bridge drops an @ of this open id from the text, and compares it with a
// quoted message's sender. bot/v3/info is not generated in the SDK, so it goes through the raw API.
func (s *sender) botIdentity(ctx context.Context) (openID, name string, err error) {
	err = s.guard("bot identity", func(client *lark.Client) error {
		openID, name = "", ""
		response, err := client.Get(ctx, "/open-apis/bot/v3/info", nil, larkcore.AccessTokenTypeTenant)
		if err != nil {
			return err
		}
		var body struct {
			Code int `json:"code"`
			Bot  struct {
				OpenID  string `json:"open_id"`
				AppName string `json:"app_name"`
			} `json:"bot"`
		}
		if err := json.Unmarshal(response.RawBody, &body); err != nil {
			return err
		}
		if body.Code != 0 {
			return &feishuError{code: body.Code, msg: "bot info failed"}
		}
		openID, name = body.Bot.OpenID, body.Bot.AppName
		return nil
	})
	return openID, name, err
}

func pointerValue(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}
