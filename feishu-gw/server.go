// server.go: the localhost API the bridge talks to. Every endpoint is one Feishu operation, plus
// the health view — no business rules live here, so a rule can never drift between the two
// processes.
package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
)

type server struct {
	conf   config
	send   *sender
	state  *stats
	out    *logging
	mux    *http.ServeMux
	client *lark.Client
}

func newServer(conf config, client *lark.Client, state *stats, out *logging) *server {
	s := &server{conf: conf, send: &sender{client: client}, state: state, out: out, mux: http.NewServeMux(), client: client}
	s.mux.HandleFunc("GET /healthz", s.healthz)
	s.mux.HandleFunc("GET /v1/bot", s.guarded(s.bot))
	s.mux.HandleFunc("POST /v1/message", s.guarded(s.create))
	s.mux.HandleFunc("POST /v1/reply", s.guarded(s.reply))
	s.mux.HandleFunc("PATCH /v1/message/{id}", s.guarded(s.patch))
	s.mux.HandleFunc("GET /v1/message/{id}/sender", s.guarded(s.senderOf))
	s.mux.HandleFunc("GET /v1/message/{id}/resource", s.guarded(s.resource))
	return s
}

func (s *server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.mux.ServeHTTP(writer, request)
}

// guarded is the only authentication, and it exists because this API can post as the bot: loopback
// is not an authorisation boundary when any local process can reach it.
func (s *server) guarded(next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if s.conf.token != "" {
			given := request.Header.Get("x-pilot-token")
			if given == "" {
				given = request.Header.Get("authorization")
				if len(given) > 7 && given[:7] == "Bearer " {
					given = given[7:]
				}
			}
			if given != s.conf.token {
				writeError(writer, http.StatusUnauthorized, "unauthorized", "x-pilot-token does not match FEISHU_GW_TOKEN", nil)
				return
			}
		}
		next(writer, request)
	}
}

func (s *server) healthz(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"ok":        true,
		"connected": s.state.connected.Load(),
		// "connected" alone is not health: a connection can be open while Feishu routes no events to
		// it. ready additionally requires having seen something.
		"ready":     s.state.connected.Load() && s.state.lastEvent.Load() != 0,
		"uptime":    int64(time.Since(s.state.started).Seconds()),
		"events":    s.state.events.Load(),
		"cards":     s.state.cards.Load(),
		"delivered": s.state.delivered.Load(),
		"dropped":   s.state.dropped.Load(),
		"lastEvent": s.state.lastEvent.Load(),
		"bridge":    s.conf.bridgeURL,
	})
}

func (s *server) bot(writer http.ResponseWriter, request *http.Request) {
	openID, name, err := botIdentity(request.Context(), s.client)
	if err != nil {
		s.fail(writer, "could not read bot identity", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"openId": openID, "name": name})
}

func (s *server) create(writer http.ResponseWriter, request *http.Request) {
	body := struct {
		ChatID   string `json:"chatId"`
		MsgType  string `json:"msgType"`
		Content  string `json:"content"`
		ThreadID string `json:"threadId"`
	}{}
	if !decode(writer, request, &body) {
		return
	}
	if body.ChatID == "" || body.Content == "" {
		writeError(writer, http.StatusBadRequest, "bad_request", "chatId and content are required", nil)
		return
	}
	msgType := firstNonEmpty(body.MsgType, "text")
	messageID, err := s.send.create(request.Context(), body.ChatID, msgType, body.Content)
	if err != nil {
		s.fail(writer, "send failed", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"messageId": messageID})
}

func (s *server) reply(writer http.ResponseWriter, request *http.Request) {
	body := struct {
		MessageID string `json:"messageId"`
		MsgType   string `json:"msgType"`
		Content   string `json:"content"`
		InThread  bool   `json:"inThread"`
	}{}
	if !decode(writer, request, &body) {
		return
	}
	if body.MessageID == "" || body.Content == "" {
		writeError(writer, http.StatusBadRequest, "bad_request", "messageId and content are required", nil)
		return
	}
	messageID, err := s.send.reply(request.Context(), body.MessageID, firstNonEmpty(body.MsgType, "interactive"), body.Content, body.InThread)
	if err != nil {
		s.fail(writer, "reply failed", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"messageId": messageID})
}

func (s *server) patch(writer http.ResponseWriter, request *http.Request) {
	body := struct {
		Content string `json:"content"`
	}{}
	if !decode(writer, request, &body) {
		return
	}
	if body.Content == "" {
		writeError(writer, http.StatusBadRequest, "bad_request", "content is required", nil)
		return
	}
	if err := s.send.patch(request.Context(), request.PathValue("id"), body.Content); err != nil {
		s.fail(writer, "patch failed", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"ok": true})
}

// senderOf also carries `senderType` and the app id, so the bridge can tell "an app wrote this"
// from "a person wrote this" without reading the message body — which for a 2.0 card comes back as
// a "please upgrade" placeholder.
func (s *server) senderOf(writer http.ResponseWriter, request *http.Request) {
	openID, appID, senderType, err := s.send.senderOf(request.Context(), request.PathValue("id"))
	if err != nil {
		s.fail(writer, "sender lookup failed", err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"openId": openID, "appId": appID, "senderType": senderType})
}

func (s *server) resource(writer http.ResponseWriter, request *http.Request) {
	query := request.URL.Query()
	fileKey := query.Get("fileKey")
	if fileKey == "" {
		writeError(writer, http.StatusBadRequest, "bad_request", "fileKey is required", nil)
		return
	}
	reader, name, err := s.send.resource(request.Context(), request.PathValue("id"), fileKey, query.Get("type"))
	if err != nil {
		s.fail(writer, "download failed", err)
		return
	}
	if name != "" {
		writer.Header().Set("content-disposition", `attachment; filename="`+name+`"`)
	}
	writer.Header().Set("content-type", "application/octet-stream")
	writer.WriteHeader(http.StatusOK)
	if _, err := io.Copy(writer, reader); err != nil {
		s.out.emit("download truncated", map[string]any{"messageId": request.PathValue("id"), "err": err.Error()})
	}
}

// fail keeps Feishu's own words: "400" alone never explained anything, and the bridge logs this
// string verbatim.
func (s *server) fail(writer http.ResponseWriter, what string, err error) {
	detail := map[string]any{}
	var feishu *feishuError
	if errors.As(err, &feishu) {
		detail["feishuCode"] = feishu.code
		detail["feishuMsg"] = feishu.msg
	}
	s.out.emit("op failed", map[string]any{"what": what, "err": err.Error()})
	writeError(writer, http.StatusBadGateway, "feishu_api", what+": "+err.Error(), detail)
}

func decode(writer http.ResponseWriter, request *http.Request, into any) bool {
	if err := json.NewDecoder(io.LimitReader(request.Body, 8<<20)).Decode(into); err != nil {
		writeError(writer, http.StatusBadRequest, "bad_request", "invalid json: "+err.Error(), nil)
		return false
	}
	return true
}

func writeJSON(writer http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		status = http.StatusInternalServerError
		encoded = []byte(`{"error":{"code":"encode_failed","message":"could not encode response"}}`)
	}
	writer.Header().Set("content-type", "application/json")
	writer.WriteHeader(status)
	_, _ = writer.Write(encoded)
}

func writeError(writer http.ResponseWriter, status int, code, message string, extra map[string]any) {
	body := map[string]any{"code": code, "message": message}
	for key, value := range extra {
		body[key] = value
	}
	writeJSON(writer, status, map[string]any{"error": body})
}
