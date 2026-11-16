package app

import (
	"github.com/egoist/mygo/ui"

	"pitta/internal/i18n"
	"pitta/internal/theme"
)

// Words for the quit question. They are constants rather than i18n keys
// because the dictionary must stay identical to client/src/i18n.ts.
const (
	quitTitle = "A turn is still running"
	quitBody  = "Quit anyway? The agent will stop where it is."
	quitLabel = "Quit"
)

// Quit asks the platform to end the process. main.go points it at mygo's own
// Quit; the tests replace it so nothing exits mid-run.
var Quit func()

// SetQuit lets main.go end the process for ⌘Q.
func SetQuit(fn func()) { Quit = fn }

// askQuit closes the window, but not while a turn is running: a half-finished
// answer is work the owner may not want to throw away, so it asks first.
func (a *App) askQuit() {
	if !a.turnRunning() {
		a.quitNow()
		return
	}
	a.apply(func() {
		a.quitAsk = true
		a.invalidate()
	})
}

// turnRunning says whether anything is in flight: the open chat's live doc, a
// busy flag, or a queued task for this or another chat.
func (a *App) turnRunning() bool {
	running := false
	a.apply(func() {
		running = a.busy || (a.live != nil && len(a.live.Run) > 0)
		if running {
			return
		}
		for _, task := range a.tasks {
			switch task.State {
			case "running", "queued", "pending", "waiting":
				running = true
				return
			}
		}
	})
	return running
}

// quitNow saves what the next launch needs and quits.
func (a *App) quitNow() {
	a.apply(func() { a.savePrefsNow() })
	if Quit != nil {
		Quit()
	}
}

// quitDialog is the confirmation the running-turn case shows.
func (a *App) quitDialog(c *ui.Context, t theme.Tokens) {
	if !a.quitAsk {
		return
	}
	open := true
	ui.Modal(c, &open, func() {
		ui.Text(c, quitTitle).FontSize(13).FontWeight(600).TextColor(t.Foreground)
		ui.Text(c, quitBody).FontSize(11).TextColor(t.MutedFg)
		ui.Row(c).FillWidth().Gap(6).Children(func() {
			ui.Row(c).Grow(1)
			ui.Button(c, i18n.T("cancel")).FontSize(11).OnClick(func() {
				a.apply(func() { a.quitAsk = false; a.invalidate() })
			})
			ui.PrimaryButton(c, quitLabel).FontSize(11).OnClick(func() {
				a.apply(func() { a.quitAsk = false })
				a.quitNow()
			})
		})
	})
	if !open && a.quitAsk {
		a.apply(func() { a.quitAsk = false; a.invalidate() })
	}
}
