// PiLot: a native desktop client for the local pi-durable agent.
//
// The window is drawn by mygo's ui package from Go; there is no web page, no
// tray, and no second window. One app, one window, one conversation.
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
	a := app.New(app.Config{
		BaseURL: bridge.BaseURL(),
		Token:   bridge.Token(),
	})

	mygo.App.WhenReady(func() {
		if err := theme.RegisterFonts(); err != nil {
			log.Printf("theme: register font: %v", err)
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "PiLot",
			// The window draws its own header, so the traffic lights sit over
			// the rail's top strip, where the eye expects them.
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
		installMenu(a)
		a.Attach(win)
		// A click on a banner lands here, after the window exists: the ID it
		// carries is the only thing that survives a restart.
		mygo.App.OnNotificationClick(a.NotificationClicked)
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
