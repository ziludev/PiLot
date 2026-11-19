// send.go: the five Feishu operations the bridge actually uses, and nothing else.
//
// The bridge used to reach for the SDK for each of them; what it needs from Feishu is create,
// reply, patch, "who sent this" and "give me that attachment". Keeping the list closed is the point
// — anything else it might want later is a new decision, not a fallthrough.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

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

type sender struct{ client *lark.Client }

func (s *sender) create(ctx context.Context, chatID, msgType, content string) (string, error) {
	response, err := s.client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.CreateMessageV1ReceiveIDTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType(msgType).
			Content(content).
			Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !response.Success() {
		return "", &feishuError{code: response.Code, msg: response.Msg}
	}
	if response.Data == nil || response.Data.MessageId == nil {
		return "", fmt.Errorf("feishu returned no message id")
	}
	return *response.Data.MessageId, nil
}

func (s *sender) reply(ctx context.Context, messageID, msgType, content string, inThread bool) (string, error) {
	body := larkim.NewReplyMessageReqBodyBuilder().
		MsgType(msgType).
		Content(content)
	if inThread {
		body = body.ReplyInThread(true)
	}
	response, err := s.client.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(body.Build()).
		Build())
	if err != nil {
		return "", err
	}
	if !response.Success() {
		return "", &feishuError{code: response.Code, msg: response.Msg}
	}
	if response.Data == nil || response.Data.MessageId == nil {
		return "", fmt.Errorf("feishu returned no message id")
	}
	return *response.Data.MessageId, nil
}

func (s *sender) patch(ctx context.Context, messageID, content string) error {
	response, err := s.client.Im.Message.Patch(ctx, larkim.NewPatchMessageReqBuilder().
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
}

// senderOf answers "did we write this?" — the one fact the bridge reads back from Feishu, used to
// tell a person quoting our answer from a person quoting themselves.
func (s *sender) senderOf(ctx context.Context, messageID string) (openID, appID, senderType string, err error) {
	response, err := s.client.Im.Message.Get(ctx, larkim.NewGetMessageReqBuilder().
		MessageId(messageID).
		Build())
	if err != nil {
		return "", "", "", err
	}
	if !response.Success() {
		return "", "", "", &feishuError{code: response.Code, msg: response.Msg}
	}
	if response.Data == nil || len(response.Data.Items) == 0 || response.Data.Items[0] == nil {
		return "", "", "", fmt.Errorf("message %s not found", messageID)
	}
	message := response.Data.Items[0]
	if message.Sender == nil {
		return "", "", "", nil
	}
	// For an app-sent message Feishu fills `id` with the app id and puts the bot's open id in
	// open_bot_id; for a person `id` is the open id itself. The bridge needs both to answer "did we
	// write this?" without reading the body.
	return pointerValue(message.Sender.OpenBotId), pointerValue(message.Sender.Id), pointerValue(message.Sender.SenderType), nil
}

func (s *sender) resource(ctx context.Context, messageID, fileKey, kind string) (io.Reader, string, error) {
	if kind == "" {
		kind = "image"
	}
	response, err := s.client.Im.MessageResource.Get(ctx, larkim.NewGetMessageResourceReqBuilder().
		MessageId(messageID).
		FileKey(fileKey).
		Type(kind).
		Build())
	if err != nil {
		return nil, "", err
	}
	if !response.Success() {
		return nil, "", &feishuError{code: response.Code, msg: response.Msg}
	}
	return response.File, response.FileName, nil
}

// bot is who we are: the bridge drops an @ of this open id from the text, and compares it with a
// quoted message's sender. bot/v3/info is not generated in the SDK, so it goes through the raw API.
func botIdentity(ctx context.Context, client *lark.Client) (openID, name string, err error) {
	response, err := client.Get(ctx, "/open-apis/bot/v3/info", nil, larkcore.AccessTokenTypeTenant)
	if err != nil {
		return "", "", err
	}
	var body struct {
		Code int `json:"code"`
		Bot  struct {
			OpenID  string `json:"open_id"`
			AppName string `json:"app_name"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(response.RawBody, &body); err != nil {
		return "", "", err
	}
	if body.Code != 0 {
		return "", "", &feishuError{code: body.Code, msg: "bot info failed"}
	}
	return body.Bot.OpenID, body.Bot.AppName, nil
}

func pointerValue(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}
