// feishu-gw owns the two things that only a long-lived Feishu process should own: the long
// connection and the credentials. The bridge talks to it over localhost HTTP; nothing else changes
// shape.
//
// Receiving (P1) and sending (P2) live here because they share one failure mode: an app token that
// went stale, or a connection that reports "connected" while Feishu is not routing events to it.
// Kept in one process, "we are up" means one thing.
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

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher/callback"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	"github.com/larksuite/oapi-sdk-go/v3/ws"
)

type config struct {
	appID     string
	appSecret string
	addr      string
	bridgeURL string
	// The bridge authenticates its own HTTP API with AGENT_HTTP_TOKEN; forwarded events have to
	// arrive with it or the bridge refuses them like any other stranger.
	bridgeToken string
	token       string
	debug       bool
}

type stats struct {
	events    atomic.Int64 // message events seen on the long connection
	cards     atomic.Int64 // card actions seen
	delivered atomic.Int64 // events the bridge accepted
	dropped   atomic.Int64 // events no retry could deliver
	lastEvent atomic.Int64 // unix seconds, 0 = never
	connected atomic.Bool
	started   time.Time
}

func main() {
	envPath := flag.String("env", "../.env", "dotenv file to read credentials from")
	addr := flag.String("addr", "", "address for the HTTP API (default FEISHU_GW_ADDR or 127.0.0.1:8788)")
	debug := flag.Bool("debug", false, "log every frame the connection receives")
	flag.Parse()

	if err := loadEnv(*envPath); err != nil {
		log.Fatalf("env: %v", err)
	}
	conf := config{
		appID:       os.Getenv("FEISHU_APP_ID"),
		appSecret:   os.Getenv("FEISHU_APP_SECRET"),
		addr:        firstNonEmpty(*addr, os.Getenv("FEISHU_GW_ADDR"), "127.0.0.1:8788"),
		bridgeURL:   firstNonEmpty(os.Getenv("BRIDGE_URL"), "http://127.0.0.1:8787"),
		bridgeToken: os.Getenv("AGENT_HTTP_TOKEN"),
		token:       os.Getenv("FEISHU_GW_TOKEN"),
		debug:       *debug,
	}
	if conf.appID == "" || conf.appSecret == "" {
		log.Fatalf("env: FEISHU_APP_ID / FEISHU_APP_SECRET missing from %s", *envPath)
	}
	if conf.token == "" {
		// The API can send messages as the bot. Reachable only from loopback, but a token is what
		// keeps a stray local process from posting as us.
		log.Print("warning: FEISHU_GW_TOKEN is empty, the local API is unauthenticated")
	}

	out := newLogger()
	state := &stats{started: time.Now()}
	level := larkcore.LogLevelInfo
	if conf.debug {
		level = larkcore.LogLevelDebug
	}
	client := lark.NewClient(conf.appID, conf.appSecret, lark.WithLogLevel(level))

	forwarder := &forwarder{base: conf.bridgeURL, token: conf.bridgeToken, out: out, state: state}
	events := dispatcher.NewEventDispatcher("", "")

	// A long connection does not verify a webhook token, and nothing here is encrypted, so both
	// dispatcher strings are empty; the app secret is the only credential and it stays in-process.
	events.OnP2MessageReceiveV1(func(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
		state.events.Add(1)
		state.lastEvent.Store(time.Now().Unix())
		// Returning now is the ack. The bridge runs a turn for minutes; a handler that waited for it
		// is why Feishu used to redeliver nearly every message.
		body := map[string]any{"type": "message", "event": innerEvent(event)}
		go forwarder.deliver(body)
		return nil
	})
	events.OnP2CardActionTrigger(func(ctx context.Context, event *callback.CardActionTriggerEvent) (*callback.CardActionTriggerResponse, error) {
		state.cards.Add(1)
		state.lastEvent.Store(time.Now().Unix())
		// Feishu gives a card action roughly three seconds before it shows its own error, so this
		// path is synchronous with a budget under that. A timeout is a toast, not a lost click.
		return forwarder.card(ctx, map[string]any{"type": "card", "event": innerCard(event)}), nil
	})

	conn := ws.NewClient(conf.appID, conf.appSecret,
		ws.WithEventHandler(events),
		ws.WithLogLevel(level),
		ws.WithAutoReconnect(true),
		ws.WithOnReady(func() {
			state.connected.Store(true)
			out.emit("ready", nil)
		}),
		ws.WithOnReconnecting(func() {
			state.connected.Store(false)
			out.emit("reconnecting", nil)
		}),
		ws.WithOnReconnected(func() {
			state.connected.Store(true)
			out.emit("reconnected", nil)
		}),
		ws.WithOnDisconnected(func() {
			state.connected.Store(false)
			out.emit("disconnected", nil)
		}),
		ws.WithOnError(func(err error) { out.emit("ws error", map[string]any{"err": err.Error()}) }),
	)

	server := newServer(conf, client, state, out)
	listener, err := net.Listen("tcp", conf.addr)
	if err != nil {
		log.Fatalf("listen %s: %v", conf.addr, err)
	}
	out.emit("listening", map[string]any{"addr": listener.Addr().String(), "bridge": conf.bridgeURL})
	go func() {
		if err := http.Serve(listener, server); err != nil && !errors.Is(err, http.ErrServerClosed) {
			out.emit("http failed", map[string]any{"err": err.Error()})
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	out.emit("starting", map[string]any{"appId": conf.appID[:min(8, len(conf.appID))] + "…"})
	if err := conn.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("long connection: %v", err)
	}
	out.emit("stopped", map[string]any{"events": state.events.Load(), "dropped": state.dropped.Load()})
}

// innerEvent unwraps the 2.0 envelope: the bridge's handler takes {sender, message}, which is what
// the Node SDK handed it before, so the forwarding keeps that shape rather than a new one.
func innerEvent(event *larkim.P2MessageReceiveV1) any {
	if event == nil || event.Event == nil {
		return nil
	}
	return event.Event
}

func innerCard(event *callback.CardActionTriggerEvent) any {
	if event == nil || event.Event == nil {
		return nil
	}
	return event.Event
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
