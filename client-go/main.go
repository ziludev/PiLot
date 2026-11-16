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
	"pitta/internal/notify"
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
			Title: "Pitta",
			// The client draws its own header instead of a title bar, so the
			// traffic lights sit over the rail's top row, as in the native apps
			// this replaces (see App.View's c.TitleBar() padding).
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &mygo.Point{X: 14, Y: 14},
			Width:                1180,
			Height:               820,
			MinWidth:             720,
			MinHeight:            480,
			BackgroundColor:      "light-dark(#ffffff, #0a0a0a)",
			StateKey:             "main",
			Content:              ui.View(a.View),
		})
		app.SetBadgeHook(func(n int) {
			mygo.App.SetBadgeCount(n)
		})
		app.SetPicker(mygo.Dialog.Open)
		notify.Sender = func(title, body string) error {
			return mygo.NewNotification(mygo.NotificationOptions{
				ID: "answer", Title: title, Body: body, Group: "answers",
			}).Show()
		}
		mygo.App.OnNotificationClick(func(string) {
			win.Show()
			win.Focus()
		})
		installMenu(a)
		registerHotkey(win)
		a.Attach(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
