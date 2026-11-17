package app

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"pitta/internal/bridge"
	"pitta/internal/i18n"
	"pitta/internal/theme"

	"github.com/egoist/mygo/ui"
)

// logTailBytes is how much of the log the viewer holds: a few thousand lines of
// recent history, small enough to re-read on every tick.
const logTailBytes = 96 * 1024

// logPoll is how often an open viewer looks for new lines.
const logPoll = time.Second

// logLineHeight is one drawn line, and the room the dialog gives them: a few
// lines still read as a panel, and a long log stops at a dialog's height.
const (
	logLineHeight = 14.0
	logMinHeight  = 120.0
	logMaxHeight  = 460.0
)

// logsPath is the file the agent appends to, beside its own storage.
func logsPath() string {
	return filepath.Join(bridge.Home(), "logs", "agent.log")
}

// logViewer is the agent's log as the window draws it. The tail is kept instead
// of the whole file: a log from a long week is not something a dialog scrolls,
// and the newest lines are the ones being looked for.
type logViewer struct {
	open   bool
	path   string
	lines  []string
	size   int64
	mod    time.Time
	scroll ui.ScrollState
	// follow keeps the newest line on screen. Scrolling up clears it, so a
	// ticking refresh cannot undo what the reader is looking at.
	follow bool
	// stop ends the poller; nil while the viewer is closed.
	stop chan struct{}
}

// toggleLogs opens the viewer, or closes it when it is already open. It runs on
// the main thread: the click handler wraps it in apply.
func (a *App) toggleLogs() {
	if a.logs.open {
		a.closeLogs()
		return
	}
	a.openLogs()
}

func (a *App) openLogs() {
	path := logsPath()
	lines, size, mod := readLogTail(path)
	stop := make(chan struct{})
	a.logs = logViewer{open: true, path: path, lines: lines, size: size, mod: mod, stop: stop, follow: true}
	go a.pollLogs(path, size, mod, stop)
}

func (a *App) closeLogs() {
	if a.logs.stop != nil {
		close(a.logs.stop)
	}
	a.logs = logViewer{}
}

// pollLogs reads the file again whenever it grew or was replaced, and one more
// time when the agent wrote while the previous read was in flight. It keeps its
// own stamp so the goroutine reads no field the window is writing.
func (a *App) pollLogs(path string, size int64, mod time.Time, stop chan struct{}) {
	tick := time.NewTicker(logPoll)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			lines, nextSize, nextMod, changed := logGrew(path, size, mod)
			if !changed {
				continue
			}
			size, mod = nextSize, nextMod
			a.post(func() {
				if a.logs.stop != stop {
					return
				}
				a.logs.lines = lines
				a.logs.size = nextSize
				a.logs.mod = nextMod
				a.invalidate()
			})
		}
	}
}

// logGrew reports whether the file moved since the last look, and returns the
// new tail when it did. Split out of the poller so the check can be driven by a
// test instead of a one-second clock.
func logGrew(path string, size int64, mod time.Time) ([]string, int64, time.Time, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, size, mod, false
	}
	if info.Size() == size && info.ModTime().Equal(mod) {
		return nil, size, mod, false
	}
	lines, nextSize, nextMod := readLogTail(path)
	return lines, nextSize, nextMod, true
}

// readLogTail returns the last logTailBytes of the file as lines, with the
// partial line the cut landed in dropped: half a line reads as a corrupted one.
// A missing file is not an error — the agent may not have written yet.
func readLogTail(path string) ([]string, int64, time.Time) {
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, time.Time{}
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, 0, time.Time{}
	}
	size := info.Size()
	start := int64(0)
	if size > logTailBytes {
		start = size - logTailBytes
	}
	if start > 0 {
		if _, err := file.Seek(start, io.SeekStart); err != nil {
			return nil, size, info.ModTime()
		}
	}
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, size, info.ModTime()
	}
	text := string(data)
	if start > 0 {
		if cut := strings.IndexByte(text, '\n'); cut >= 0 {
			text = text[cut+1:]
		} else {
			text = ""
		}
	}
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil, size, info.ModTime()
	}
	return strings.Split(text, "\n"), size, info.ModTime()
}

// logsDialog draws the viewer. Escape and the backdrop close it, the way every
// other dialog in the window closes.
func (a *App) logsDialog(c *ui.Context, t theme.Tokens) {
	open := a.logs.open
	ui.Modal(c, &open, func() {
		ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("logs")).FontSize(13).FontWeight(600).TextColor(t.Foreground)
			ui.Text(c, shortPath(a.logs.path)).FontSize(10).TextColor(t.MutedFg).
				Grow(1).MaxLines(1).TruncateMiddle()
			ui.Button(c, i18n.T("close")).FontSize(11).OnClick(func() {
				a.apply(func() { a.closeLogs(); a.invalidate() })
			})
		})
		if len(a.logs.lines) == 0 {
			ui.Text(c, i18n.T("logsEmpty")).FontSize(11).TextColor(t.MutedFg)
			return
		}
		// The reader took over when they scrolled away from the end; scrolling
		// back to it hands the tail back to the poller.
		if a.logs.scroll.MaxY > 0 {
			a.logs.follow = a.logs.scroll.Y >= a.logs.scroll.MaxY-8
		}
		if a.logs.follow {
			a.logs.scroll.Y = math.MaxFloat32
		}
		ui.ScrollBoth(c).FillWidth().Height(logHeight(len(a.logs.lines))).
			TrackScroll(&a.logs.scroll).Children(func() {
			for _, line := range a.logs.lines {
				ui.Text(c, line).FontSize(10.5).Font("monospace").MaxLines(1).
					TextColor(logLineColor(t, line))
			}
		})
	})
	if a.logs.open != open {
		a.apply(func() {
			if open {
				a.openLogs()
			} else {
				a.closeLogs()
			}
			a.invalidate()
		})
	}
}

// logHeight is the dialog's room for n lines: enough that a short log is not
// drawn inside a screenful of white, and capped so a long one scrolls.
func logHeight(n int) float32 {
	height := float32(n)*logLineHeight + 8
	if height < logMinHeight {
		return logMinHeight
	}
	if height > logMaxHeight {
		return logMaxHeight
	}
	return height
}

// logLineColor marks a line the way the log marks itself: a failure reads at a
// glance, everything else stays background so the failures stand out.
func logLineColor(t theme.Tokens, line string) ui.Color {
	lower := strings.ToLower(line)
	if strings.Contains(lower, "[error]") || strings.Contains(lower, " error ") {
		return t.Destructive
	}
	if strings.Contains(lower, "[warn]") {
		return t.Warning
	}
	return t.MutedFg
}
