// feishu-gw is the transport half of the bridge: it holds the Feishu long connection so the Node
// side can stop depending on the Node SDK for it.
//
// P1 is receive-only. Every message event is written to the log with the same chat/sender facts the
// Node bridge logs, and nothing is sent back — so it can run next to the Node client without
// changing what alex sees. What P1 answers: does this connection see the same events, once, while
// the Node connection is still up? (P2 adds the send/patch/reply operations and the HTTP contract.)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"
)

type counters struct {
	messages  atomic.Int64
	duplicate atomic.Int64
	lastEvent atomic.Int64 // unix seconds of the most recent event
	started   time.Time
}

func main() {
	envPath := flag.String("env", "../.env", "dotenv file to read FEISHU_APP_ID / FEISHU_APP_SECRET from")
	addr := flag.String("addr", "127.0.0.1:8788", "address for /healthz")
	send := flag.Bool("send", false, "enable outbound messages (P2); off means receive-only")
	flag.Parse()

	if err := loadEnv(*envPath); err != nil {
		log.Fatalf("env: %v", err)
	}
	appID, appSecret := os.Getenv("FEISHU_APP_ID"), os.Getenv("FEISHU_APP_SECRET")
	if appID == "" || appSecret == "" {
		log.Fatalf("env: FEISHU_APP_ID / FEISHU_APP_SECRET missing from %s", *envPath)
	}

	stats := &counters{started: time.Now()}
	out := newLogger()

	// A long connection does not verify a webhook token, so both are empty; the app secret is the
	// only credential and it never leaves this process.
	events := dispatcher.NewEventDispatcher("", "")
	events.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		record(out, stats, event)
		return nil
	})

	client := ws.NewClient(appID, appSecret,
		ws.WithEventHandler(events),
		ws.WithLogLevel(larkcore.LogLevelInfo),
		ws.WithAutoReconnect(true),
		ws.WithOnReady(func() {
			out.emit("ready", map[string]any{"mode": modeName(*send)})
		}),
		ws.WithOnReconnecting(func() { out.emit("reconnecting", nil) }),
		ws.WithOnReconnected(func() { out.emit("reconnected", nil) }),
		ws.WithOnDisconnected(func() { out.emit("disconnected", nil) }),
		ws.WithOnError(func(err error) { out.emit("ws error", map[string]any{"err": err.Error()}) }),
	)

	serveHealthz(out, stats, *addr, *send)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	out.emit("starting", map[string]any{"mode": modeName(*send), "appId": appID[:min(8, len(appID))] + "…"})
	if err := client.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("long connection: %v", err)
	}
	out.emit("stopped", map[string]any{"messages": stats.messages.Load()})
}

// record writes one line per event. The fields are the ones the Node bridge logs for the same
// event, so the two logs can be diffed line by line during P1.
func record(out *logging, stats *counters, event *larkim.P2MessageReceiveV1) {
	stats.messages.Add(1)
	stats.lastEvent.Store(time.Now().Unix())
	fields := map[string]any{}
	if event != nil && event.Event != nil && event.Event.Message != nil {
		message := event.Event.Message
		fields["messageId"] = value(message.MessageId)
		fields["chat"] = value(message.ChatId)
		fields["type"] = value(message.MessageType)
		if message.Content != nil {
			fields["chars"] = len(*message.Content)
		}
	}
	if event != nil && event.Event != nil && event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		fields["sender"] = value(event.Event.Sender.SenderId.OpenId)
		fields["senderType"] = value(event.Event.Sender.SenderType)
	}
	if event != nil && event.EventV2Base != nil {
		if event.EventV2Base.Header != nil {
			fields["eventId"] = event.EventV2Base.Header.EventID
		}
	}
	out.emit("message", fields)
}

type logging struct{ logger *log.Logger }

func newLogger() *logging {
	return &logging{logger: log.New(os.Stdout, "", log.LstdFlags|log.Lmicroseconds)}
}

// emit prints one JSON object per line: the bridge log is read by eye and by grep, and a stable
// first field ("t") is what makes `grep '"t":"message"'` work.
func (l *logging) emit(kind string, fields map[string]any) {
	line := map[string]any{"t": kind}
	for key, value := range fields {
		line[key] = value
	}
	body, err := json.Marshal(line)
	if err != nil {
		l.logger.Printf(`{"t":%q,"err":%q}`, kind, err.Error())
		return
	}
	l.logger.Print(string(body))
}

func serveHealthz(out *logging, stats *counters, addr string, send bool) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		body := map[string]any{
			"ok":       true,
			"mode":     modeName(send),
			"uptime":   int64(time.Since(stats.started).Seconds()),
			"messages": stats.messages.Load(),
			"lastEvent": func() int64 {
				if at := stats.lastEvent.Load(); at != 0 {
					return at
				}
				return 0
			}(),
		}
		encoded, _ := json.Marshal(body)
		writer.Header().Set("content-type", "application/json")
		_, _ = writer.Write(encoded)
	})
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("healthz: %v", err)
	}
	out.emit("healthz listening", map[string]any{"addr": listener.Addr().String()})
	go func() {
		if err := http.Serve(listener, mux); err != nil && !errors.Is(err, http.ErrServerClosed) {
			out.emit("healthz failed", map[string]any{"err": err.Error()})
		}
	}()
}

// loadEnv is the smallest dotenv reader that works on the bridge's file: KEY=VALUE lines, optional
// quotes, `#` comments, no expansion. Values already in the environment win, so the launcher can
// override without editing the file.
func loadEnv(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range splitLines(string(body)) {
		key, value, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, value)
		}
	}
	return nil
}

func parseEnvLine(line string) (key, value string, ok bool) {
	line = trimSpace(line)
	if line == "" || line[0] == '#' {
		return "", "", false
	}
	at := -1
	for index := 0; index < len(line); index++ {
		if line[index] == '=' {
			at = index
			break
		}
	}
	if at <= 0 {
		return "", "", false
	}
	key = trimSpace(line[:at])
	value = trimSpace(line[at+1:])
	if len(value) >= 2 {
		if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
			value = value[1 : len(value)-1]
		}
	}
	return key, value, key != ""
}

func splitLines(body string) []string {
	lines := []string{}
	start := 0
	for index := 0; index < len(body); index++ {
		if body[index] == '\n' {
			lines = append(lines, body[start:index])
			start = index + 1
		}
	}
	return append(lines, body[start:])
}

func trimSpace(text string) string {
	start, end := 0, len(text)
	for start < end && (text[start] == ' ' || text[start] == '\t' || text[start] == '\r') {
		start++
	}
	for end > start && (text[end-1] == ' ' || text[end-1] == '\t' || text[end-1] == '\r') {
		end--
	}
	return text[start:end]
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}
	return *pointer
}

func modeName(send bool) string {
	if send {
		return "p2"
	}
	return "p1-receive-only"
}
