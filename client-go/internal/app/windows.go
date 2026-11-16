package app

import "pitta/internal/api"

// newWindowLabel names the rail's "open this chat in a window" item. React has
// no such command, so it has no i18n key.
const newWindowLabel = "Open in new window"

// windowOpener creates a second window on a chat. main.go sets it, because only
// main can create a mygo window.
var windowOpener func(cfg Config, client *api.Client, conversationID int)

// SetWindowOpener lets main.go hand the app its window factory.
func SetWindowOpener(fn func(cfg Config, client *api.Client, conversationID int)) {
	windowOpener = fn
}
