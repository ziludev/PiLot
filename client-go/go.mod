module pitta

go 1.27.3

require (
	github.com/coder/websocket v1.8.15
	// The goal asked for v0.6.4; v0.9.1 is deliberate and explained in
	// docs/mygo-version.md: 0.6.4 commits the in-progress IME composition
	// before it handles Enter, so a half-typed pinyin leaked into the sent
	// message. Nothing else in the app needs a post-0.6.4 API.
	github.com/egoist/mygo v0.9.1
)

require (
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/cloudflare/circl v1.6.2 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	go.digitalxero.dev/rpm v0.3.4 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

tool github.com/egoist/mygo/cmd/mygo
