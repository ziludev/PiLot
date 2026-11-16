package app

import (
	"log"
	"net/url"
	"strconv"
	"strings"
)

// DeepLink is what pitta://chat/123 asks for: a chat, optionally in its own
// window. The React client had no deep links; a native app can register a
// scheme, which is how a terminal command or a Feishu message jumps straight
// to a conversation.
type DeepLink struct {
	ChatID    int
	NewWindow bool
}

// ParseDeepLink reads a pitta:// URL. Anything else is not ours, and a missing
// or non-numeric chat number is refused rather than guessed.
func ParseDeepLink(raw string) (DeepLink, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "pitta") {
		return DeepLink{}, false
	}
	if host := strings.ToLower(u.Host); host != "" && host != "chat" {
		return DeepLink{}, false
	}
	// pitta://chat/123 and pitta:///chat/123 are the same link: some tools
	// drop the host.
	path := strings.Trim(strings.TrimPrefix(strings.Trim(u.Path, "/"), "chat/"), "/")
	if path == "" {
		return DeepLink{}, false
	}
	chatID, err := strconv.Atoi(path)
	if err != nil || chatID <= 0 {
		return DeepLink{}, false
	}
	newWindow := false
	switch strings.ToLower(u.Query().Get("window")) {
	case "new", "1", "true":
		newWindow = true
	}
	return DeepLink{ChatID: chatID, NewWindow: newWindow}, true
}

// OpenDeepLink acts on a URL the system handed the app.
func (a *App) OpenDeepLink(raw string) {
	link, ok := ParseDeepLink(raw)
	if !ok {
		log.Printf("deeplink: ignore %q", raw)
		return
	}
	log.Printf("deeplink: chat %d newWindow=%v", link.ChatID, link.NewWindow)
	if link.NewWindow && windowOpener != nil {
		a.openInNewWindow(link.ChatID)
		return
	}
	a.OpenFromNotification(link.ChatID)
}
