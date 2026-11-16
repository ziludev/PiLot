// Pitta: a native desktop client for the local pi-durable agent.
//
// The window is drawn by mygo's ui package from Go; there is no web page. The
// Go side also owns what the web page could not do itself in the earlier client
// client: reading the bridge port and token from the agent's .env, running
// scripts/agentctl, notifications and revealing the log directory.
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"pitta/internal/api"
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
		tray := installTray(a, win)
		app.SetBadgeHook(func(n int) {
			mygo.App.SetBadgeCount(n)
			if tray != nil {
				// The menu bar is where a hidden window's work shows up.
				title := ""
				if n > 0 {
					title = fmt.Sprintf(" %d", n)
				}
				tray.SetTitle(title)
			}
		})
		app.SetSaver(func(name string, data []byte) error {
			path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
				Title:       "Export chat",
				DefaultPath: name,
			})
			if err != nil || path == "" {
				return err
			}
			return os.WriteFile(path, data, 0o644)
		})
		app.SetPicker(mygo.Dialog.Open)
		app.SetQuit(mygo.App.Quit)
		// An approval is the one thing that must reach the user while the
		// window is behind others: the system alert plus a Dock bounce, which
		// stops as soon as the wait is over.
		var bounce int
		app.SetAttention(func(on bool) {
			if on {
				mygo.Shell.Beep()
				bounce = mygo.App.Dock.Bounce(false)
				return
			}
			if bounce != 0 {
				mygo.App.Dock.CancelBounce(bounce)
				bounce = 0
			}
		})
		notify.Sender = func(id, title, body string) error {
			return mygo.NewNotification(mygo.NotificationOptions{
				ID: id, Title: title, Body: body, Group: "answers",
			}).Show()
		}
		// A click lands on the chat the answer came from, not just on the app:
		// the notification says which one in its id.
		mygo.App.OnNotificationClick(func(id string) {
			win.Show()
			win.Focus()
			if chatID, ok := notify.ChatFromID(id); ok {
				a.OpenFromNotification(chatID)
			}
		})
		// pitta://chat/123 from a terminal, a script or a chat message: the
		// scheme is declared in mygo.config.ts.
		mygo.App.OnOpenURL(func(raw string) {
			win.Show()
			win.Focus()
			a.OpenDeepLink(raw)
		})
		installMenu(a)
		registerHotkey(win)
		// ⌘⇧N and the rail's menu open one chat in its own window: it shares
		// the client, while approvals, the badge and the preference file stay
		// with the first window.
		app.SetWindowOpener(func(cfg app.Config, client *api.Client, conversationID int) {
			second := app.NewSecondary(cfg, client, conversationID)
			win := mygo.NewWindow(mygo.WindowOptions{
				Title:                "Pitta",
				Width:                1180,
				Height:               820,
				MinWidth:             720,
				MinHeight:            480,
				BackgroundColor:      "light-dark(#ffffff, #0a0a0a)",
				TitleBarStyle:        mygo.TitleBarHidden,
				TrafficLightPosition: &mygo.Point{X: 14, Y: 14},
				Content:              ui.View(second.View),
			})
			second.Attach(win)
		})
		a.Attach(win)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
