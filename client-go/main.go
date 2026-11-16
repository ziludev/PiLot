// Pitta: a native desktop client for the local pi-durable agent.
//
// The window is drawn by mygo's ui package from Go; there is no web page. The
// Go side also owns what the web page could not do itself in the React
// client: reading the bridge port and token from the agent's .env, running
// scripts/agentctl, notifications and revealing the log directory.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"pitta/internal/app"
	"pitta/internal/bridge"
	"pitta/internal/theme"
)

func main() {
	cfg := app.Config{
		BaseURL: bridge.BaseURL(),
		Token:   bridge.Token(),
		Home:    bridge.Home(),
	}

	mygo.App.WhenReady(func() {
		if err := theme.RegisterFonts(); err != nil {
			log.Printf("theme: register font: %v", err)
		}
		a := app.New(cfg)
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:           "Pitta",
			Width:           1180,
			Height:          820,
			MinWidth:        720,
			MinHeight:       480,
			BackgroundColor: "light-dark(#ffffff, #0a0a0a)",
			StateKey:        "main",
			Content:         ui.View(a.View),
		})
		a.Attach(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
